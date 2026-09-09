package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/ScienJus/kairos/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestValidateAdminToken(t *testing.T) {
	for _, token := range []string{"", strings.Repeat("x", 31), strings.Repeat("界", 10) + "x", strings.Repeat("x", 16) + "\u00a0" + strings.Repeat("x", 16), " " + strings.Repeat("x", 32), strings.Repeat("x", 32) + "\n", strings.Repeat("x", 16) + " " + strings.Repeat("x", 16), ExecutorTokenPrefix + base64.RawURLEncoding.EncodeToString(make([]byte, 32))} {
		if err := ValidateAdminToken(token); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid Admin credential accepted")
		}
	}
	for _, token := range []string{strings.Repeat("x", 32), strings.Repeat("界", 10) + "xx", strings.Repeat("界", 11), ExecutorTokenPrefix + strings.Repeat("x", 32)} {
		if err := ValidateAdminToken(token); err != nil {
			t.Fatal(err)
		}
	}
}

type adminRepositoryStub struct {
	Repository
	ensure func(StoredIdentity) (StoredIdentity, error)
}

func (r adminRepositoryStub) EnsureAdminIdentity(_ context.Context, candidate StoredIdentity, _ string) (StoredIdentity, error) {
	return r.ensure(candidate)
}

type adminTestClock struct{}

func (adminTestClock) Now() time.Time { return time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC) }

func TestConfigureAdminRetriesCollisionAndRejectsCorruptBinding(t *testing.T) {
	token, err := (SecureTokenGenerator{}).NewToken()
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	repo := adminRepositoryStub{ensure: func(candidate StoredIdentity) (StoredIdentity, error) {
		attempts++
		if attempts == 1 {
			return StoredIdentity{}, ErrConflict
		}
		return candidate, nil
	}}
	s, _ := NewService(repo, adminTestClock{}, SecureTokenGenerator{})
	if err := s.ConfigureAdmin(context.Background(), token); err != nil || attempts != 2 {
		t.Fatal("ID collision was not retried")
	}
	if err := s.ConfigureAdmin(context.Background(), token); !errors.Is(err, ErrInvalid) {
		t.Fatal("runtime reconfiguration accepted")
	}
	for _, corrupt := range []func(*StoredIdentity){
		func(v *StoredIdentity) { v.CredentialSource = "identity" },
		func(v *StoredIdentity) { v.Identity.Actor.Kind = domain.ActorAgent; v.Identity.Role = "worker" },
		func(v *StoredIdentity) { v.Identity.Role = "admin" },
		func(v *StoredIdentity) { v.TokenHash = strings.Repeat("a", 64) },
		func(v *StoredIdentity) { v.Identity.Actor.ID = "" },
	} {
		repo.ensure = func(candidate StoredIdentity) (StoredIdentity, error) { corrupt(&candidate); return candidate, nil }
		fresh, _ := NewService(repo, adminTestClock{}, SecureTokenGenerator{})
		if err := fresh.ConfigureAdmin(context.Background(), token); !errors.Is(err, ErrInvalid) {
			t.Fatal("corrupt Admin binding accepted")
		}
		if fresh.adminHash != "" {
			t.Fatal("failed configuration activated credential")
		}
	}
}
