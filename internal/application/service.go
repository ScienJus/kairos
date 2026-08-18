package application

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
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
	ErrForbidden = errors.New("application operation forbidden")

	// ErrInvalidCommand indicates malformed application input.
	ErrInvalidCommand = errors.New("invalid application command")
)

// Identity is kept as an application-level alias for command compatibility.
// The identity model itself lives independently from application orchestration.
type Identity = identity.Identity

// Service coordinates application operations over one Repository.
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
	claimLease time.Duration
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
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if err := s.ReapExpiredClaims(ctx); err != nil {
					log.Printf("kairos: lease reaper: %v", err)
				}
			}
		}
	}()
	return func() { close(stop); <-done }
}

// ReapExpiredClaims scans open WorkItems and expires stale active Claims.
func (s *Service) ReapExpiredClaims(ctx context.Context) error {
	return s.repository.Update(ctx, func(store WriteStore) error {
		items, err := store.ListWorkItems()
		if err != nil {
			return err
		}
		now := s.clock.Now()
		for _, item := range items {
			if item.Status != domain.WorkItemStatusOpen {
				continue
			}
			tasks, err := store.ListTasks(item.ID)
			if err != nil {
				return err
			}
			for i := range tasks {
				if tasks[i].ActiveClaimID == nil {
					continue
				}
				claims, err := store.ListClaims(tasks[i].ID)
				if err != nil {
					return err
				}
				if _, err := s.expireActiveClaim(store, &tasks[i], claims, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
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
	return &Service{repository: repository, clock: clock, ids: ids, claimLease: DefaultClaimLease}, nil
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

func forbidden(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrForbidden, fmt.Sprintf(format, args...))
}

func identityCanExecute(identity Identity, task domain.Task) error {
	if err := identity.Validate(); err != nil {
		return invalidCommand("invalid identity: %v", err)
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
