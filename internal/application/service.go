package application

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

var (
	// ErrNotFound indicates that an application target does not exist.
	ErrNotFound = errors.New("application target not found")

	// ErrConflict indicates that current state no longer permits the operation.
	ErrConflict = errors.New("application state conflict")

	// ErrForbidden indicates that the actor may not perform the operation.
	ErrForbidden = identity.ErrForbidden

	// ErrInvalidCommand indicates malformed application input.
	ErrInvalidCommand = errors.New("invalid application command")

	// ErrWorkItemCancelled indicates that a terminal cancellation rejected a mutation.
	ErrWorkItemCancelled = errors.New("work item cancelled")
)

// Identity is kept as an application-level alias for command compatibility.
// The identity model itself lives independently from application orchestration.
type Identity = identity.Identity

// Service coordinates application operations over one Repository.
type Service struct {
	repository          Repository
	clock               Clock
	ids                 IDGenerator
	claimLease          time.Duration
	artifactStore       ArtifactContentStore
	artifactStoreScheme string
	artifactStoreMu     sync.RWMutex
}

type microsecondClock struct {
	Clock
}

func (clock microsecondClock) Now() time.Time {
	return clock.Clock.Now().UTC().Truncate(time.Microsecond)
}

// StartLeaseReaper periodically returns abandoned tasks to the pending queue.
// The returned stop function waits for the worker to exit.
func (s *Service) StartLeaseReaper(ctx context.Context, interval time.Duration) func() {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		reap := func() {
			if err := s.ReapExpiredClaims(ctx); err != nil {
				log.Printf("kairos: lease reaper: %v", err)
			}
		}
		reap()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				reap()
			}
		}
	}()
	return func() { close(stop); <-done }
}

// ReapExpiredClaims ends expired Task and WorkItem coordination Claims.
func (s *Service) ReapExpiredClaims(ctx context.Context) error {
	now := s.clock.Now()
	var taskIDs []domain.TaskID
	var workItemIDs []domain.WorkItemID
	if err := s.repository.View(ctx, func(store ReadStore) error {
		var err error
		taskIDs, err = store.ListReapableAgentClaimTasks(now)
		if err != nil {
			return fmt.Errorf("list reapable agent claims: %w", err)
		}
		workItemIDs, err = store.ListReapableCoordinationClaimWorkItems(now)
		if err != nil {
			return fmt.Errorf("list reapable coordination claims: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	var reapErrors []error
	for _, taskID := range taskIDs {
		if err := s.repository.Update(ctx, func(store WriteStore) error {
			task, err := store.GetTask(taskID)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					return nil
				}
				return fmt.Errorf("get reapable task %q: %w", taskID, err)
			}
			claims, err := store.ListClaims(task.ID)
			if err != nil {
				return fmt.Errorf("list claims for reapable task %q: %w", task.ID, err)
			}
			if _, err := s.reapActiveClaim(store, &task, claims, now); err != nil {
				return fmt.Errorf("reap claim for task %q: %w", task.ID, err)
			}
			return nil
		}); err != nil {
			reapErrors = append(reapErrors, err)
		}
	}
	for _, workItemID := range workItemIDs {
		if err := s.repository.Update(ctx, func(store WriteStore) error {
			workItem, err := store.GetWorkItem(workItemID)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					return nil
				}
				return fmt.Errorf("get reapable work item %q: %w", workItemID, err)
			}
			claims, err := store.ListCoordinationClaims(workItem.ID)
			if err != nil {
				return fmt.Errorf("list coordination claims for %q: %w", workItem.ID, err)
			}
			claim := activeCoordinationClaim(claims)
			if claim == nil || now.Before(claim.LeaseUntil) {
				return nil
			}
			return endCoordinationClaim(store, claim, domain.CoordinationClaimEndExpired, now)
		}); err != nil {
			reapErrors = append(reapErrors, err)
		}
	}
	return errors.Join(reapErrors...)
}

// NewService creates an application Service.
func NewService(repository Repository, clock Clock, ids IDGenerator) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: repository is required", ErrInvalidCommand)
	}
	if clock == nil {
		return nil, fmt.Errorf("%w: clock is required", ErrInvalidCommand)
	}
	if ids == nil {
		return nil, fmt.Errorf("%w: id generator is required", ErrInvalidCommand)
	}
	return &Service{repository: repository, clock: microsecondClock{Clock: clock}, ids: ids, claimLease: DefaultClaimLease}, nil
}

// ConfigureArtifactStore selects the managed content store for this service.
func (s *Service) ConfigureArtifactStore(store ArtifactContentStore) error {
	if store == nil || strings.TrimSpace(store.Scheme()) == "" {
		return invalidCommand("artifact store scheme is required")
	}
	s.artifactStoreMu.Lock()
	defer s.artifactStoreMu.Unlock()
	s.artifactStore = store
	s.artifactStoreScheme = strings.ToLower(strings.TrimSpace(store.Scheme()))
	return nil
}

func (s *Service) SetClaimLeaseDuration(duration time.Duration) error {
	if duration <= 0 {
		return invalidCommand("claim lease duration must be positive")
	}
	s.claimLease = duration
	return nil
}

func (s *Service) newID(field string) (string, error) {
	id := strings.TrimSpace(s.ids.NewID())
	if id == "" {
		return "", fmt.Errorf("%w: generated %s is empty", ErrInvalidCommand, field)
	}
	return id, nil
}

func invalidCommand(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCommand, fmt.Sprintf(format, args...))
}

func conflict(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrConflict, fmt.Sprintf(format, args...))
}

func rejectCancelledWorkItem(workItem domain.WorkItem) error {
	if workItem.Status == domain.WorkItemStatusCancelled {
		return fmt.Errorf("%w: work item %q", ErrWorkItemCancelled, workItem.ID)
	}
	return nil
}

func forbidden(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrForbidden, fmt.Sprintf(format, args...))
}

func identityCanExecute(identity Identity, task domain.Task) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	switch task.Executor {
	case domain.ExecutorAgent:
		if identity.Actor.Kind != domain.ActorAgent {
			return forbidden("task %q requires an agent", task.ID)
		}
	case domain.ExecutorHuman:
		if identity.Actor.Kind != domain.ActorHuman {
			return forbidden("task %q requires a human", task.ID)
		}
	case domain.ExecutorEither:
	default:
		return invalidCommand("task %q has invalid executor %q", task.ID, task.Executor)
	}

	if identity.Actor.Kind != domain.ActorAgent || identity.HasAnyRole(task.AllowedRoles) {
		return nil
	}
	return forbidden("agent role %q cannot execute task %q", identity.Role, task.ID)
}

func sameActor(left, right domain.ActorRef) bool {
	return left.Kind == right.Kind && left.ID == right.ID
}
