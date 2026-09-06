package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

// StoredIdentity is the persistence representation of one managed identity.
// TokenHash is never returned through the management API.
type StoredIdentity struct {
	Identity  Identity
	TokenHash string
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Record is the public management view of one identity.
type Record struct {
	Identity    Identity
	TokenActive bool
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IssuedToken contains plaintext token material that is returned only when a
// token is created or rotated.
type IssuedToken struct {
	Identity Identity
	Token    string
}

// Repository persists managed identities and resolves token hashes.
type Repository interface {
	CreateIdentity(context.Context, StoredIdentity) error
	SaveIdentity(context.Context, StoredIdentity) error
	GetIdentity(context.Context, domain.ActorRef) (StoredIdentity, error)
	GetIdentityByTokenHash(context.Context, string) (StoredIdentity, error)
	ListIdentities(context.Context) ([]StoredIdentity, error)
}

// Clock supplies deterministic identity timestamps.
type Clock interface {
	Now() time.Time
}

// TokenGenerator creates high-entropy bearer tokens.
type TokenGenerator interface {
	NewToken() (string, error)
}

// SecureTokenGenerator creates 256-bit URL-safe bearer tokens.
type SecureTokenGenerator struct{}

func (SecureTokenGenerator) NewToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// Service manages identity records and authenticates bearer tokens.
type Service struct {
	repository Repository
	clock      Clock
	tokens     TokenGenerator
}

type microsecondClock struct {
	Clock
}

func (clock microsecondClock) Now() time.Time {
	return clock.Clock.Now().UTC().Truncate(time.Microsecond)
}

// NewService creates an authenticated identity service.
func NewService(repository Repository, clock Clock, tokens TokenGenerator) (*Service, error) {
	if repository == nil || clock == nil || tokens == nil {
		return nil, fmt.Errorf("%w: repository, clock and token generator are required", ErrInvalid)
	}
	return &Service{repository: repository, clock: microsecondClock{Clock: clock}, tokens: tokens}, nil
}

// CreateIdentity creates one identity and its initial token.
func (s *Service) CreateIdentity(ctx context.Context, actor domain.ActorRef, role string) (IssuedToken, error) {
	identity := Identity{Actor: actor, Role: strings.TrimSpace(role)}
	if err := identity.Validate(); err != nil {
		return IssuedToken{}, err
	}
	token, tokenHash, err := s.newToken()
	if err != nil {
		return IssuedToken{}, err
	}
	now := s.clock.Now()
	stored := StoredIdentity{
		Identity: identity, TokenHash: tokenHash, CreatedAt: now, UpdatedAt: now,
	}
	if err := validateStoredIdentity(stored); err != nil {
		return IssuedToken{}, err
	}
	if err := s.repository.CreateIdentity(ctx, stored); err != nil {
		return IssuedToken{}, fmt.Errorf("create identity %q: %w", actor.ID, err)
	}
	return IssuedToken{Identity: identity, Token: token}, nil
}

// Authenticate resolves one bearer token into its server-managed identity.
func (s *Service) Authenticate(ctx context.Context, token string) (Identity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Identity{}, fmt.Errorf("%w: bearer token is required", ErrUnauthenticated)
	}
	stored, err := s.repository.GetIdentityByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Identity{}, fmt.Errorf("%w: bearer token is invalid or revoked", ErrUnauthenticated)
		}
		return Identity{}, fmt.Errorf("authenticate bearer token: %w", err)
	}
	if stored.TokenHash == "" {
		return Identity{}, fmt.Errorf("%w: bearer token is revoked", ErrUnauthenticated)
	}
	if err := validateStoredIdentity(stored); err != nil {
		return Identity{}, err
	}
	return stored.Identity, nil
}

// GetIdentity returns one public managed identity record.
func (s *Service) GetIdentity(ctx context.Context, actor domain.ActorRef) (Record, error) {
	if err := actor.Validate(); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	stored, err := s.repository.GetIdentity(ctx, actor)
	if err != nil {
		return Record{}, err
	}
	return publicRecord(stored), nil
}

// ListIdentities returns all managed identities without credential hashes.
func (s *Service) ListIdentities(ctx context.Context) ([]Record, error) {
	stored, err := s.repository.ListIdentities(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(stored))
	for _, value := range stored {
		result = append(result, publicRecord(value))
	}
	return result, nil
}

// RotateToken atomically replaces the current token while preserving identity.
func (s *Service) RotateToken(ctx context.Context, actor domain.ActorRef) (IssuedToken, error) {
	stored, err := s.repository.GetIdentity(ctx, actor)
	if err != nil {
		return IssuedToken{}, err
	}
	token, tokenHash, err := s.newToken()
	if err != nil {
		return IssuedToken{}, err
	}
	stored.TokenHash = tokenHash
	stored.Version++
	stored.UpdatedAt = s.clock.Now()
	if err := s.repository.SaveIdentity(ctx, stored); err != nil {
		return IssuedToken{}, fmt.Errorf("rotate token for %q: %w", actor.ID, err)
	}
	return IssuedToken{Identity: stored.Identity, Token: token}, nil
}

// RevokeToken removes the current credential without deleting identity history.
func (s *Service) RevokeToken(ctx context.Context, actor domain.ActorRef) error {
	stored, err := s.repository.GetIdentity(ctx, actor)
	if err != nil {
		return err
	}
	if stored.TokenHash == "" {
		return nil
	}
	stored.TokenHash = ""
	stored.Version++
	stored.UpdatedAt = s.clock.Now()
	if err := s.repository.SaveIdentity(ctx, stored); err != nil {
		return fmt.Errorf("revoke token for %q: %w", actor.ID, err)
	}
	return nil
}

func (s *Service) newToken() (string, string, error) {
	token, err := s.tokens.NewToken()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(token) == "" || token != strings.TrimSpace(token) || strings.HasPrefix(token, ExecutorTokenPrefix) {
		return "", "", fmt.Errorf("%w: generated token is empty, untrimmed, or uses a reserved prefix", ErrInvalid)
	}
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func publicRecord(stored StoredIdentity) Record {
	return Record{
		Identity: stored.Identity, TokenActive: stored.TokenHash != "", Version: stored.Version,
		CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
	}
}

func validateStoredIdentity(stored StoredIdentity) error {
	if err := stored.Identity.Validate(); err != nil {
		return err
	}
	if stored.Version < 0 {
		return fmt.Errorf("%w: version must not be negative", ErrInvalid)
	}
	if stored.CreatedAt.IsZero() || stored.UpdatedAt.IsZero() || stored.UpdatedAt.Before(stored.CreatedAt) {
		return fmt.Errorf("%w: identity timestamps are invalid", ErrInvalid)
	}
	if stored.TokenHash != "" {
		decoded, err := hex.DecodeString(stored.TokenHash)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("%w: token hash is invalid", ErrInvalid)
		}
	}
	return nil
}
