package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

const (
	// CreateArtifactOperation identifies replay records for external Artifacts.
	CreateArtifactOperation = "create_artifact"
	// ArtifactUploadOperation identifies the specialized pending/completed
	// recovery records used by managed Artifact uploads.
	ArtifactUploadOperation = "upload_artifact"
)

// CreateArtifactCommand registers one external deliverable under an active Claim.
type CreateArtifactCommand struct {
	TaskID      domain.TaskID
	ClaimID     domain.ClaimID
	Identity    Identity
	OperationID string
	Name        string
	URI         string
}

// UploadArtifactCommand uploads one deliverable to the server's managed Store.
type UploadArtifactCommand struct {
	TaskID      domain.TaskID
	ClaimID     domain.ClaimID
	Identity    Identity
	OperationID string
	Name        string
}

// CreateArtifact registers an external URI. Managed schemes must use UploadArtifact.
func (s *Service) CreateArtifact(ctx context.Context, command CreateArtifactCommand) (domain.Artifact, error) {
	parsed, err := url.Parse(strings.TrimSpace(command.URI))
	if err != nil || parsed.Scheme == "" {
		return domain.Artifact{}, invalidCommand("artifact uri must be absolute")
	}
	s.artifactStoreMu.RLock()
	managed := strings.ToLower(parsed.Scheme) == s.artifactStoreScheme
	s.artifactStoreMu.RUnlock()
	if managed {
		return domain.Artifact{}, invalidCommand("managed artifact scheme %q must be created by upload", parsed.Scheme)
	}
	return s.createArtifact(ctx, command)
}

// UploadArtifact reserves durable upload state, writes content to the managed
// Store, then atomically creates its Blob and staged Artifact records.
func (s *Service) UploadArtifact(ctx context.Context, command UploadArtifactCommand, source io.Reader) (domain.Artifact, error) {
	s.artifactStoreMu.Lock()
	defer s.artifactStoreMu.Unlock()

	if s.artifactStore == nil {
		return domain.Artifact{}, invalidCommand("artifact store is not configured")
	}
	if source == nil {
		return domain.Artifact{}, invalidCommand("artifact content is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Artifact{}, err
	}
	command.Name = strings.TrimSpace(command.Name)
	if command.Name == "" {
		return domain.Artifact{}, invalidCommand("artifact name is required")
	}
	if command.OperationID == "" {
		return domain.Artifact{}, invalidCommand("operation id is required for managed artifact upload")
	}
	if command.OperationID != strings.TrimSpace(command.OperationID) {
		return domain.Artifact{}, invalidCommand("operation id must not have surrounding whitespace")
	}
	uploadKey, err := artifactUploadStorageKey(command.Identity.Actor, command.OperationID)
	if err != nil {
		return domain.Artifact{}, err
	}
	uploadURI, err := s.artifactStore.UploadURI(uploadKey)
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("prepare artifact upload URI: %w", err)
	}
	completed, state, err := s.reserveArtifactUpload(ctx, command, uploadURI)
	if err != nil {
		return domain.Artifact{}, err
	}
	if completed != nil {
		var persistedBlob domain.ArtifactBlob
		if err := s.repository.View(ctx, func(store ReadStore) error {
			var err error
			persistedBlob, err = store.GetArtifactBlob(completed.URI)
			return err
		}); err != nil {
			return domain.Artifact{}, fmt.Errorf("get completed Artifact Blob: %w", err)
		}
		if err := verifyUploadDigest(source, persistedBlob.Digest); err != nil {
			return domain.Artifact{}, err
		}
		return *completed, nil
	}
	blob, err := s.artifactStore.Put(ctx, state.BlobURI, source)
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("store artifact content: %w", err)
	}
	if blob.URI != state.BlobURI {
		return domain.Artifact{}, fmt.Errorf("artifact Store returned a different URI than the registered upload")
	}
	if state.Digest != "" && (blob.Digest != state.Digest || blob.Size != state.Size) {
		return domain.Artifact{}, conflict("operation id was reused with different artifact content")
	}
	if state.Digest == "" {
		state.Digest, state.Size = blob.Digest, blob.Size
		if err := s.saveArtifactUploadState(ctx, command, state); err != nil {
			return domain.Artifact{}, fmt.Errorf("record artifact content metadata: %w", err)
		}
	}
	blob.CreatedAt = s.clock.Now()
	return s.finalizeArtifactUpload(ctx, command, blob)
}

func artifactUploadStorageKey(actor domain.ActorRef, operationID string) (string, error) {
	return idempotencyRequestHash(struct {
		Actor       domain.ActorRef `json:"actor"`
		OperationID string          `json:"operation_id"`
	}{Actor: actor, OperationID: operationID})
}

func verifyUploadDigest(source io.Reader, expected string) error {
	hash := sha256.New()
	if _, err := io.Copy(hash, source); err != nil {
		return fmt.Errorf("read artifact content for idempotency check: %w", err)
	}
	actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return conflict("operation id was reused with different artifact content")
	}
	return nil
}

func (s *Service) createArtifact(ctx context.Context, command CreateArtifactCommand) (domain.Artifact, error) {
	if strings.TrimSpace(string(command.TaskID)) == "" || strings.TrimSpace(string(command.ClaimID)) == "" {
		return domain.Artifact{}, invalidCommand("task id and claim id are required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Artifact{}, err
	}
	command.Name = strings.TrimSpace(command.Name)
	command.URI = strings.TrimSpace(command.URI)
	if command.Name == "" || command.URI == "" {
		return domain.Artifact{}, invalidCommand("artifact name and uri are required")
	}

	var created domain.Artifact
	var terminalErr error
	err := s.replayableCreate(ctx, command.Identity, command.OperationID, CreateArtifactOperation, command, &created, func(store WriteStore) error {
		task, claim, err := activeOwnedClaim(store, command.TaskID, command.ClaimID, command.Identity)
		if err != nil {
			return err
		}
		id, err := s.newID("artifact id")
		if err != nil {
			return err
		}
		reached, err := taskArtifactCapacity(store, task)
		if err != nil {
			return err
		}
		if reached {
			reason := historyLimitReason(task.ID, "artifacts", MaxArtifactsPerTask)
			workItem, err := store.GetWorkItem(task.WorkItemID)
			if err != nil {
				return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
			}
			if err := s.failWorkItem(store, &workItem, nil, systemFailureActor(), reason, s.clock.Now()); err != nil {
				return err
			}
			terminalErr = conflict("work item %q failed: %s", task.WorkItemID, reason)
			return commitAndReturn(terminalErr)
		}
		created = domain.Artifact{
			ID: domain.ArtifactID(id), WorkItemID: task.WorkItemID, TaskID: task.ID,
			ClaimID: claim.ID, Name: command.Name, URI: command.URI, CreatedAt: s.clock.Now(),
		}
		if err := store.CreateArtifact(created); err != nil {
			return fmt.Errorf("create artifact: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Artifact{}, err
	}
	if terminalErr != nil {
		return domain.Artifact{}, terminalErr
	}
	return created, nil
}

type artifactUploadReservation struct {
	TaskID  domain.TaskID
	ClaimID domain.ClaimID
	Name    string
}

type artifactUploadState struct {
	BlobURI string `json:"blob_uri"`
	Digest  string `json:"digest,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

func (s *Service) reserveArtifactUpload(ctx context.Context, command UploadArtifactCommand, uploadURI string) (*domain.Artifact, artifactUploadState, error) {
	reservation := artifactUploadReservation{TaskID: command.TaskID, ClaimID: command.ClaimID, Name: command.Name}
	requestHash, err := idempotencyRequestHash(reservation)
	if err != nil {
		return nil, artifactUploadState{}, err
	}
	state := artifactUploadState{BlobURI: uploadURI}
	var completed *domain.Artifact
	var terminalErr error
	err = s.repository.Update(ctx, func(store WriteStore) error {
		if err := store.LockIdempotencyKey(command.Identity.Actor, command.OperationID); err != nil {
			return fmt.Errorf("lock operation %q: %w", command.OperationID, err)
		}
		record, err := store.GetIdempotencyRecord(command.Identity.Actor, command.OperationID)
		switch {
		case errors.Is(err, ErrNotFound):
			task, _, err := activeOwnedClaim(store, command.TaskID, command.ClaimID, command.Identity)
			if err != nil {
				return err
			}
			reached, err := taskArtifactCapacity(store, task)
			if err != nil {
				return err
			}
			if reached {
				reason := historyLimitReason(task.ID, "artifacts", MaxArtifactsPerTask)
				workItem, err := store.GetWorkItem(task.WorkItemID)
				if err != nil {
					return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
				}
				if err := s.failWorkItem(store, &workItem, nil, systemFailureActor(), reason, s.clock.Now()); err != nil {
					return err
				}
				terminalErr = conflict("work item %q failed: %s", workItem.ID, reason)
				return nil
			}
			return store.CreateIdempotencyRecord(IdempotencyRecord{
				Actor: command.Identity.Actor, OperationID: command.OperationID,
				Operation: ArtifactUploadOperation, Status: IdempotencyPending,
				RequestHash: requestHash, Response: mustArtifactUploadState(state), CreatedAt: s.clock.Now(),
			})
		case err != nil:
			return fmt.Errorf("get operation %q: %w", command.OperationID, err)
		case record.Operation == ArtifactUploadOperation && record.Status == IdempotencyPending:
			if record.RequestHash != requestHash {
				return conflict("operation id %q was already used for another request", command.OperationID)
			}
			if err := json.Unmarshal([]byte(record.Response), &state); err != nil || state.BlobURI == "" {
				return fmt.Errorf("decode pending Artifact upload %q: %w", command.OperationID, err)
			}
			task, _, err := activeOwnedClaim(store, command.TaskID, command.ClaimID, command.Identity)
			if err != nil {
				return err
			}
			reached, err := taskArtifactCapacity(store, task)
			if err != nil {
				return err
			}
			if reached {
				reason := historyLimitReason(task.ID, "artifacts", MaxArtifactsPerTask)
				workItem, err := store.GetWorkItem(task.WorkItemID)
				if err != nil {
					return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
				}
				if err := s.failWorkItem(store, &workItem, nil, systemFailureActor(), reason, s.clock.Now()); err != nil {
					return err
				}
				terminalErr = conflict("work item %q failed: %s", workItem.ID, reason)
			}
			return nil
		case record.Status == IdempotencyCompleted && record.Operation == ArtifactUploadOperation:
			if record.RequestHash != requestHash {
				return conflict("operation id %q was already used for another request", command.OperationID)
			}
			artifact, err := retainedIdempotentArtifact(store, record)
			if err != nil {
				return err
			}
			if artifact.TaskID != command.TaskID || artifact.ClaimID != command.ClaimID || artifact.Name != command.Name {
				return conflict("operation id %q was already used for another request", command.OperationID)
			}
			completed = &artifact
			return nil
		default:
			return conflict("operation id %q was already used for another request", command.OperationID)
		}
	})
	if err != nil {
		return nil, artifactUploadState{}, err
	}
	if terminalErr != nil {
		return nil, artifactUploadState{}, terminalErr
	}
	return completed, state, nil
}

func taskArtifactCapacity(store ReadStore, task domain.Task) (bool, error) {
	artifacts, err := store.ListArtifacts(ArtifactFilter{WorkItemID: task.WorkItemID, TaskID: task.ID})
	if err != nil {
		return false, fmt.Errorf("count artifacts for task %q: %w", task.ID, err)
	}
	return len(artifacts) >= MaxArtifactsPerTask, nil
}

func mustArtifactUploadState(state artifactUploadState) string {
	encoded, _ := json.Marshal(state)
	return string(encoded)
}

func (s *Service) saveArtifactUploadState(ctx context.Context, command UploadArtifactCommand, state artifactUploadState) error {
	return s.repository.Update(ctx, func(store WriteStore) error {
		if err := store.LockIdempotencyKey(command.Identity.Actor, command.OperationID); err != nil {
			return err
		}
		record, err := store.GetIdempotencyRecord(command.Identity.Actor, command.OperationID)
		if err != nil {
			return err
		}
		if record.Operation != ArtifactUploadOperation || record.Status != IdempotencyPending {
			return conflict("operation id %q was already completed", command.OperationID)
		}
		record.Response = mustArtifactUploadState(state)
		return store.SaveIdempotencyRecord(record, s.clock.Now())
	})
}

func (s *Service) finalizeArtifactUpload(ctx context.Context, command UploadArtifactCommand, blob domain.ArtifactBlob) (domain.Artifact, error) {
	reservation := artifactUploadReservation{TaskID: command.TaskID, ClaimID: command.ClaimID, Name: command.Name}
	reservationHash, err := idempotencyRequestHash(reservation)
	if err != nil {
		return domain.Artifact{}, err
	}

	var result domain.Artifact
	var terminalErr error
	err = s.repository.Update(ctx, func(store WriteStore) error {
		if err := store.LockIdempotencyKey(command.Identity.Actor, command.OperationID); err != nil {
			return fmt.Errorf("lock operation %q: %w", command.OperationID, err)
		}
		record, err := store.GetIdempotencyRecord(command.Identity.Actor, command.OperationID)
		if err != nil {
			return fmt.Errorf("get operation %q: %w", command.OperationID, err)
		}
		if record.Status == IdempotencyCompleted && record.Operation == ArtifactUploadOperation {
			if record.RequestHash != reservationHash {
				return conflict("operation id %q was already used for another request", command.OperationID)
			}
			artifact, err := retainedIdempotentArtifact(store, record)
			if err != nil {
				return err
			}
			result = artifact
			return nil
		}
		if record.Operation != ArtifactUploadOperation || record.Status != IdempotencyPending || record.RequestHash != reservationHash {
			return conflict("operation id %q was already used for another request", command.OperationID)
		}
		task, claim, err := activeOwnedClaim(store, command.TaskID, command.ClaimID, command.Identity)
		if err != nil {
			return err
		}
		// Recheck while the final transaction holds the WorkItem lock. The
		// reservation transaction has already committed, so another instance
		// may have added an Artifact in the meantime.
		reached, err := taskArtifactCapacity(store, task)
		if err != nil {
			return err
		}
		if reached {
			reason := historyLimitReason(task.ID, "artifacts", MaxArtifactsPerTask)
			workItem, err := store.GetWorkItem(task.WorkItemID)
			if err != nil {
				return fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
			}
			if err := s.failWorkItem(store, &workItem, nil, systemFailureActor(), reason, s.clock.Now()); err != nil {
				return err
			}
			terminalErr = conflict("work item %q failed: %s", task.WorkItemID, reason)
			return nil
		}
		if err := store.CreateArtifactBlob(blob); err != nil {
			return fmt.Errorf("create artifact blob: %w", err)
		}
		id, err := s.newID("artifact id")
		if err != nil {
			return err
		}
		result = domain.Artifact{
			ID: domain.ArtifactID(id), WorkItemID: task.WorkItemID, TaskID: task.ID,
			ClaimID: claim.ID, Name: command.Name, URI: blob.URI, CreatedAt: s.clock.Now(),
		}
		if err := store.CreateArtifact(result); err != nil {
			return fmt.Errorf("create artifact: %w", err)
		}
		response, err := idempotencyResponse(result)
		if err != nil {
			return err
		}
		record.Status = IdempotencyCompleted
		record.Response = response
		if err := store.SaveIdempotencyRecord(record, s.clock.Now()); err != nil {
			return fmt.Errorf("complete Artifact upload operation %q: %w", command.OperationID, err)
		}
		return nil
	})
	if err != nil {
		return domain.Artifact{}, err
	}
	if terminalErr != nil {
		return domain.Artifact{}, terminalErr
	}
	return result, nil
}

func retainedIdempotentArtifact(store ReadStore, record IdempotencyRecord) (domain.Artifact, error) {
	var artifact domain.Artifact
	if err := json.Unmarshal([]byte(record.Response), &artifact); err != nil {
		return domain.Artifact{}, fmt.Errorf("decode idempotent Artifact response: %w", err)
	}
	persisted, err := store.GetArtifact(artifact.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.Artifact{}, conflict("idempotent Artifact %q is no longer retained", artifact.ID)
		}
		return domain.Artifact{}, fmt.Errorf("get idempotent Artifact %q: %w", artifact.ID, err)
	}
	return persisted, nil
}

func activeOwnedClaim(store ReadStore, taskID domain.TaskID, claimID domain.ClaimID, identity Identity) (domain.Task, domain.Claim, error) {
	task, err := store.GetTask(taskID)
	if err != nil {
		return domain.Task{}, domain.Claim{}, fmt.Errorf("get task %q: %w", taskID, err)
	}
	workItem, err := store.GetWorkItem(task.WorkItemID)
	if err != nil {
		return domain.Task{}, domain.Claim{}, fmt.Errorf("get work item %q: %w", task.WorkItemID, err)
	}
	if err := rejectCancelledWorkItem(workItem); err != nil {
		return domain.Task{}, domain.Claim{}, err
	}
	if workItem.Status != domain.WorkItemStatusOpen {
		return domain.Task{}, domain.Claim{}, conflict("work item %q is %s", workItem.ID, workItem.Status)
	}
	claims, err := store.ListClaims(task.ID)
	if err != nil {
		return domain.Task{}, domain.Claim{}, fmt.Errorf("list claims for task %q: %w", task.ID, err)
	}
	index := findClaim(claims, claimID)
	if index < 0 {
		return domain.Task{}, domain.Claim{}, fmt.Errorf("%w: claim %q", ErrNotFound, claimID)
	}
	claim := claims[index]
	if task.Status != domain.TaskStatusWorking || task.ActiveClaimID == nil || *task.ActiveClaimID != claim.ID || !claim.Active() {
		return domain.Task{}, domain.Claim{}, conflict("claim %q is not active for task %q", claim.ID, task.ID)
	}
	if !sameActor(claim.Executor, identity.Actor) {
		return domain.Task{}, domain.Claim{}, forbidden("actor does not own claim %q", claim.ID)
	}
	return task, claim, nil
}

// ListArtifacts returns a page of committed Artifacts in a WorkItem.
func (s *Service) ListArtifacts(ctx context.Context, workItemID domain.WorkItemID, identity Identity, page PageRequest[ArtifactCursor]) (Page[domain.Artifact], error) {
	if strings.TrimSpace(string(workItemID)) == "" {
		return Page[domain.Artifact]{}, invalidCommand("work item id is required")
	}
	if err := identity.Validate(); err != nil {
		return Page[domain.Artifact]{}, err
	}
	if err := validatePageRequest(page.Limit); err != nil {
		return Page[domain.Artifact]{}, err
	}
	result := make([]domain.Artifact, 0)
	err := s.repository.View(ctx, func(store ReadStore) error {
		if _, err := store.GetWorkItem(workItemID); err != nil {
			return fmt.Errorf("get work item %q: %w", workItemID, err)
		}
		artifacts, err := store.ListArtifacts(ArtifactFilter{WorkItemID: workItemID, SubmittedOnly: true, Page: page})
		if err != nil {
			return fmt.Errorf("list artifacts for work item %q: %w", workItemID, err)
		}
		result = append(result, artifacts...)
		return nil
	})
	if err != nil {
		return Page[domain.Artifact]{}, err
	}
	return boundedPage(result, page.Limit), nil
}

// OpenArtifact opens managed content after checking staged Artifact ownership.
func (s *Service) OpenArtifact(ctx context.Context, artifactID domain.ArtifactID, identity Identity) (domain.Artifact, io.ReadCloser, error) {
	if err := identity.Validate(); err != nil {
		return domain.Artifact{}, nil, err
	}
	var artifact domain.Artifact
	err := s.repository.View(ctx, func(store ReadStore) error {
		var err error
		artifact, err = store.GetArtifact(artifactID)
		if err != nil {
			return err
		}
		if artifact.SubmissionID != nil {
			return nil
		}
		claims, err := store.ListClaims(artifact.TaskID)
		if err != nil {
			return err
		}
		index := findClaim(claims, artifact.ClaimID)
		if index < 0 || !claims[index].Active() || !sameActor(claims[index].Executor, identity.Actor) {
			return forbidden("artifact %q has not been submitted", artifact.ID)
		}
		return nil
	})
	if err != nil {
		return domain.Artifact{}, nil, err
	}
	s.artifactStoreMu.RLock()
	artifactStore := s.artifactStore
	artifactStoreScheme := s.artifactStoreScheme
	s.artifactStoreMu.RUnlock()
	parsed, _ := url.Parse(artifact.URI)
	if strings.ToLower(parsed.Scheme) != artifactStoreScheme || artifactStore == nil {
		return artifact, nil, fmt.Errorf("%w: artifact URI scheme %q is not managed", ErrNotFound, parsed.Scheme)
	}
	content, err := artifactStore.Open(ctx, artifact.URI)
	if err != nil {
		return artifact, nil, err
	}
	return artifact, content, nil
}
