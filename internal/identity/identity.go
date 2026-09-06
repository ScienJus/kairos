// Package identity defines trusted actor identities presented to Kairos.
package identity

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

var (
	// ErrUnauthenticated indicates that a request did not provide a usable identity.
	ErrUnauthenticated = errors.New("identity is unauthenticated")

	// ErrInvalid indicates that credential or identity attributes are malformed.
	ErrInvalid = errors.New("invalid identity")

	// ErrNotFound indicates that a managed identity does not exist.
	ErrNotFound = errors.New("identity not found")

	// ErrConflict indicates that an identity mutation conflicts with stored state.
	ErrConflict = errors.New("identity conflict")

	// ErrForbidden indicates that a credential cannot perform an operation.
	ErrForbidden = errors.New("credential operation forbidden")
)

// Principal is the authenticated actor information consumed by application operations.
// Authentication mechanisms resolve credentials into this type before invoking
// the application layer.
type Principal struct {
	Actor    domain.ActorRef
	Role     string
	Executor *ExecutorScope `json:"-"`
}

// Identity remains an alias for existing callers presenting ordinary identities.
type Identity = Principal

// Validate checks the trusted identity fields.
func (i Identity) Validate() error {
	if i.Executor != nil {
		return ErrForbidden
	}
	if err := i.Actor.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if i.Role != strings.TrimSpace(i.Role) {
		return fmt.Errorf("%w: role must be trimmed", ErrInvalid)
	}
	if i.Actor.Kind == domain.ActorHuman && i.Role != "" {
		return fmt.Errorf("%w: human identity must not declare an agent role", ErrInvalid)
	}
	if i.Actor.Kind == domain.ActorAgent && i.Role == "" {
		return fmt.Errorf("%w: agent identity requires a role", ErrInvalid)
	}
	return nil
}

// HasAnyRole reports whether the identity owns at least one allowed role.
// An empty allowed set does not impose a role restriction.
func (i Identity) HasAnyRole(allowed []string) bool {
	return len(allowed) == 0 || slices.Contains(allowed, i.Role)
}
