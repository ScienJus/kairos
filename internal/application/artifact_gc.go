package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

const DefaultArtifactGCRetention = 24 * time.Hour
const DefaultArtifactGCInterval = 15 * time.Minute

// ArtifactGCResult summarizes one Artifact garbage collection pass.
type ArtifactGCResult struct {
	ArtifactsDeleted           int
	BlobsDeleted               int
	PendingDeleted             int
	CompletedOperationsDeleted int
}

// GarbageCollectArtifacts removes old staged Artifacts whose Claims have
// ended, then removes managed Blobs that no Artifact references.
func (s *Service) GarbageCollectArtifacts(ctx context.Context, retention time.Duration) (ArtifactGCResult, error) {
	if retention <= 0 {
		return ArtifactGCResult{}, invalidCommand("artifact GC retention must be positive")
	}

	s.artifactStoreMu.Lock()
	defer s.artifactStoreMu.Unlock()

	cutoff := s.clock.Now().Add(-retention)
	result := ArtifactGCResult{}
	var pending []IdempotencyRecord
	if err := s.repository.Update(ctx, func(store WriteStore) error {
		artifacts, err := store.ListArtifactGarbage(cutoff)
		if err != nil {
			return fmt.Errorf("list Artifact garbage: %w", err)
		}
		for _, artifact := range artifacts {
			if err := store.DeleteArtifact(artifact.ID); err != nil {
				return fmt.Errorf("delete staged Artifact %q: %w", artifact.ID, err)
			}
			result.ArtifactsDeleted++
		}
		pending, err = store.ListPendingIdempotencyRecords(cutoff)
		if err != nil {
			return fmt.Errorf("list pending Artifact uploads: %w", err)
		}
		result.CompletedOperationsDeleted, err = store.DeleteCompletedArtifactOperationRecords(cutoff)
		if err != nil {
			return fmt.Errorf("delete completed Artifact operation records: %w", err)
		}
		return nil
	}); err != nil {
		return ArtifactGCResult{}, err
	}

	var blobs []domain.ArtifactBlob
	if err := s.repository.View(ctx, func(store ReadStore) error {
		var err error
		blobs, err = store.ListUnreferencedArtifactBlobs(cutoff)
		return err
	}); err != nil {
		return result, fmt.Errorf("list unreferenced Artifact Blobs: %w", err)
	}

	var collectionErrors []error
	for _, record := range pending {
		if record.Operation != ArtifactUploadOperation {
			continue
		}
		if err := s.collectPendingArtifactUpload(ctx, record); err != nil {
			collectionErrors = append(collectionErrors, err)
			continue
		}
		result.PendingDeleted++
	}
	for _, blob := range blobs {
		parsed, err := url.Parse(blob.URI)
		if err != nil {
			collectionErrors = append(collectionErrors, fmt.Errorf("parse Artifact Blob URI %q: %w", blob.URI, err))
			continue
		}
		if s.artifactStore == nil || strings.ToLower(parsed.Scheme) != s.artifactStoreScheme {
			continue
		}
		if err := s.artifactStore.Delete(ctx, blob.URI); err != nil {
			collectionErrors = append(collectionErrors, fmt.Errorf("delete Artifact Blob %q: %w", blob.URI, err))
			continue
		}
		if err := s.repository.Update(ctx, func(store WriteStore) error {
			referenced, err := store.ArtifactBlobReferenced(blob.URI)
			if err != nil {
				return err
			}
			if referenced {
				return nil
			}
			return store.DeleteArtifactBlob(blob.URI)
		}); err != nil {
			collectionErrors = append(collectionErrors, fmt.Errorf("delete Artifact Blob metadata %q: %w", blob.URI, err))
			continue
		}
		result.BlobsDeleted++
	}
	return result, errors.Join(collectionErrors...)
}

func (s *Service) collectPendingArtifactUpload(ctx context.Context, record IdempotencyRecord) error {
	var state artifactUploadState
	response := strings.TrimSpace(record.Response)
	if response == "" {
		response = "{}"
	}
	if err := json.Unmarshal([]byte(response), &state); err != nil {
		return fmt.Errorf("decode pending Artifact upload %q: %w", record.OperationID, err)
	}
	deleteURI := func(rawURI string) error {
		if strings.TrimSpace(rawURI) == "" {
			return nil
		}
		parsed, err := url.Parse(rawURI)
		if err != nil {
			return err
		}
		if s.artifactStore == nil || strings.ToLower(parsed.Scheme) != s.artifactStoreScheme {
			return fmt.Errorf("no Artifact Store configured for URI scheme %q", parsed.Scheme)
		}
		if err := s.artifactStore.Delete(ctx, rawURI); err != nil {
			return err
		}
		return nil
	}
	if err := deleteURI(state.BlobURI); err != nil {
		return fmt.Errorf("delete pending Artifact Blob %q: %w", record.OperationID, err)
	}
	return s.repository.Update(ctx, func(store WriteStore) error {
		if err := store.DeleteIdempotencyRecord(record.Actor, record.OperationID); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return nil
	})
}

// StartArtifactGarbageCollector periodically collects abandoned Artifacts.
// The returned stop function waits for the worker to exit.
func (s *Service) StartArtifactGarbageCollector(ctx context.Context, retention, interval time.Duration) (func(), error) {
	if retention <= 0 {
		return nil, invalidCommand("artifact GC retention must be positive")
	}
	if interval <= 0 {
		return nil, invalidCommand("artifact GC interval must be positive")
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		collect := func() {
			result, err := s.GarbageCollectArtifacts(ctx, retention)
			if err != nil {
				log.Printf("kairos: Artifact GC: %v", err)
				return
			}
			if result.ArtifactsDeleted > 0 || result.BlobsDeleted > 0 || result.PendingDeleted > 0 || result.CompletedOperationsDeleted > 0 {
				log.Printf("kairos: Artifact GC deleted %d Artifacts, %d Blobs, %d pending uploads, and %d completed Artifact operation records", result.ArtifactsDeleted, result.BlobsDeleted, result.PendingDeleted, result.CompletedOperationsDeleted)
			}
		}
		collect()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				collect()
			}
		}
	}()
	return func() { close(stop); <-done }, nil
}
