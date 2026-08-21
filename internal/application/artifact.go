package application

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
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

// UploadArtifactCommand uploads one deliverable to the server-configured default Store.
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
	if _, managed := s.artifactStores[strings.ToLower(parsed.Scheme)]; managed {
		return domain.Artifact{}, invalidCommand("managed artifact scheme %q must be created by upload", parsed.Scheme)
	}
	return s.createArtifact(ctx, command, nil)
}

// UploadArtifact writes content to the configured default Store before creating
// its staged Artifact record. Content addressing makes retries inexpensive.
func (s *Service) UploadArtifact(ctx context.Context, command UploadArtifactCommand, source io.Reader) (domain.Artifact, error) {
	if s.defaultArtifactStore == nil {
		return domain.Artifact{}, invalidCommand("default artifact store is not configured")
	}
	if source == nil {
		return domain.Artifact{}, invalidCommand("artifact content is required")
	}
	if err := command.Identity.Validate(); err != nil {
		return domain.Artifact{}, err
	}
	if strings.TrimSpace(command.Name) == "" {
		return domain.Artifact{}, invalidCommand("artifact name is required")
	}
	if err := s.repository.View(ctx, func(store ReadStore) error {
		_, _, err := activeOwnedClaim(store, command.TaskID, command.ClaimID, command.Identity)
		return err
	}); err != nil {
		return domain.Artifact{}, err
	}
	blob, err := s.defaultArtifactStore.Put(ctx, s.defaultArtifactStoreURI, source)
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("store artifact content: %w", err)
	}
	blob.CreatedAt = s.clock.Now()
	return s.createArtifact(ctx, CreateArtifactCommand{
		TaskID: command.TaskID, ClaimID: command.ClaimID, Identity: command.Identity,
		OperationID: command.OperationID, Name: command.Name, URI: blob.URI,
	}, &blob)
}

func (s *Service) createArtifact(ctx context.Context, command CreateArtifactCommand, blob *domain.ArtifactBlob) (domain.Artifact, error) {
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
	err := s.idempotentUpdate(ctx, command.Identity, command.OperationID, "create_artifact", command, &created, func(store WriteStore) error {
		task, claim, err := activeOwnedClaim(store, command.TaskID, command.ClaimID, command.Identity)
		if err != nil {
			return err
		}
		if blob != nil {
			if err := store.CreateArtifactBlob(*blob); err != nil {
				return fmt.Errorf("create artifact blob: %w", err)
			}
		}
		id, err := s.newID("artifact id")
		if err != nil {
			return err
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
	return created, nil
}

func activeOwnedClaim(store ReadStore, taskID domain.TaskID, claimID domain.ClaimID, identity Identity) (domain.Task, domain.Claim, error) {
	task, err := store.GetTask(taskID)
	if err != nil {
		return domain.Task{}, domain.Claim{}, fmt.Errorf("get task %q: %w", taskID, err)
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

// ListArtifacts returns every committed Artifact in a WorkItem.
func (s *Service) ListArtifacts(ctx context.Context, workItemID domain.WorkItemID, identity Identity) ([]domain.Artifact, error) {
	if strings.TrimSpace(string(workItemID)) == "" {
		return nil, invalidCommand("work item id is required")
	}
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	result := make([]domain.Artifact, 0)
	err := s.repository.View(ctx, func(store ReadStore) error {
		if _, err := store.GetWorkItem(workItemID); err != nil {
			return fmt.Errorf("get work item %q: %w", workItemID, err)
		}
		artifacts, err := store.ListArtifacts(workItemID)
		if err != nil {
			return fmt.Errorf("list artifacts for work item %q: %w", workItemID, err)
		}
		for _, artifact := range artifacts {
			if artifact.SubmissionID != nil {
				result = append(result, artifact)
			}
		}
		return nil
	})
	return result, err
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
	parsed, _ := url.Parse(artifact.URI)
	store, exists := s.artifactStores[strings.ToLower(parsed.Scheme)]
	if !exists {
		return artifact, nil, fmt.Errorf("%w: artifact URI scheme %q is not managed", ErrNotFound, parsed.Scheme)
	}
	content, err := store.Open(ctx, artifact.URI)
	if err != nil {
		return artifact, nil, err
	}
	return artifact, content, nil
}
