package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

// AdminRepository atomically creates or loads the database's dedicated Human.
// The deployment credential hash is checked for collisions but never persisted.
type AdminRepository interface {
	EnsureAdminIdentity(context.Context, StoredIdentity, string) (StoredIdentity, error)
}

// ConfigureAdmin must run once at startup, before serving requests. Rotation is
// a deployment configuration change: no Admin credential is an Identity token.
func (s *Service) ConfigureAdmin(ctx context.Context, token string) error {
	if err := ValidateAdminToken(token); err != nil {
		return err
	}
	if s.adminHash != "" {
		return fmt.Errorf("%w: Admin authentication is already configured", ErrInvalid)
	}
	repo, ok := s.repository.(AdminRepository)
	if !ok {
		return fmt.Errorf("%w: repository does not support Admin identities", ErrInvalid)
	}
	for attempt := 0; attempt < 8; attempt++ {
		suffix, err := (SecureTokenGenerator{}).NewToken()
		if err != nil {
			return err
		}
		now := s.clock.Now()
		candidate := StoredIdentity{Identity: Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: domain.ActorID("admin-" + suffix)}}, CredentialSource: "admin", CreatedAt: now, UpdatedAt: now}
		stored, err := repo.EnsureAdminIdentity(ctx, candidate, hashToken(token))
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if err := validateStoredIdentity(stored); err != nil {
			return err
		}
		if stored.CredentialSource != "admin" || stored.Identity.Actor.Kind != domain.ActorHuman || stored.Identity.Role != "" || stored.TokenHash != "" {
			return fmt.Errorf("%w: corrupt Admin identity binding", ErrInvalid)
		}
		s.adminHash, s.adminIdentity = hashToken(token), stored.Identity
		return nil
	}
	return fmt.Errorf("%w: could not initialize Admin identity after concurrent or ID conflicts", ErrConflict)
}

// ValidateAdminToken keeps deployment credentials out of the Claim namespace.
func ValidateAdminToken(token string) error {
	if len(token) < 32 {
		return fmt.Errorf("%w: Admin Token must contain at least 32 visible ASCII characters", ErrInvalid)
	}
	if strings.ContainsFunc(token, func(r rune) bool { return r < 0x21 || r > 0x7e }) {
		return fmt.Errorf("%w: Admin Token must contain only visible ASCII characters (0x21-0x7E)", ErrInvalid)
	}
	if _, err := ExecutorTokenHash(token); err == nil {
		return fmt.Errorf("%w: Admin Token must not use the canonical Executor format", ErrInvalid)
	}
	return nil
}
