package application

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
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

// Identity contains the trusted actor identity used by application operations.
type Identity struct {
	Actor domain.ActorRef
	Role  string
}

// Validate checks the trusted identity fields.
func (i Identity) Validate() error {
	if err := i.Actor.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCommand, err)
	}
	if i.Actor.Kind == domain.ActorHuman && strings.TrimSpace(i.Role) != "" {
		return fmt.Errorf("%w: human identity must not declare an agent role", ErrInvalidCommand)
	}
	return nil
}

// Service coordinates application operations over one Repository.
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
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
	return &Service{repository: repository, clock: clock, ids: ids}, nil
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

	if identity.Actor.Kind != domain.ActorAgent || len(task.AllowedRoles) == 0 {
		return nil
	}
	for _, role := range task.AllowedRoles {
		if role == identity.Role {
			return nil
		}
	}
	return forbidden("agent role %q cannot execute task %q", identity.Role, task.ID)
}

func sameActor(left, right domain.ActorRef) bool {
	return left.Kind == right.Kind && left.ID == right.ID
}
