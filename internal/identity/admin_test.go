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
	for _, tc := range []struct{ name, token, reason string }{
		{"missing", "", "at least 32 visible ASCII characters"},
		{"short", strings.Repeat("x", 31), "at least 32 visible ASCII characters"},
		{"unicode at byte boundary", strings.Repeat("界", 10) + "xx", "only visible ASCII"},
		{"unicode beyond byte boundary", strings.Repeat("界", 11), "only visible ASCII"},
		{"latin1", strings.Repeat("x", 32) + "é", "only visible ASCII"},
		{"unicode whitespace", strings.Repeat("x", 32) + "\u00a0", "only visible ASCII"},
		{"space below lower boundary", " " + strings.Repeat("x", 32), "only visible ASCII"},
		{"internal space", strings.Repeat("x", 16) + " " + strings.Repeat("x", 16), "only visible ASCII"},
		{"newline", strings.Repeat("x", 32) + "\n", "only visible ASCII"},
		{"tab", strings.Repeat("x", 32) + "\t", "only visible ASCII"},
		{"nul", strings.Repeat("x", 32) + "\x00", "only visible ASCII"},
		{"del beyond upper boundary", strings.Repeat("x", 32) + "\x7f", "only visible ASCII"},
		{"executor namespace", ExecutorTokenPrefix + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), "canonical Executor format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAdminToken(tc.token)
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("expected guard %q, got %v", tc.reason, err)
			}
			if tc.token != "" && strings.Contains(err.Error(), tc.token) {
				t.Fatal("error disclosed credential")
			}
		})
	}
	for _, token := range []string{strings.Repeat("x", 32), strings.Repeat("x", 33), strings.Repeat("!", 32), strings.Repeat("~", 32), ExecutorTokenPrefix + strings.Repeat("x", 32)} {
		if err := ValidateAdminToken(token); err != nil {
			t.Fatal(err)
		}
	}
	// Every byte outside the documented alphabet is rejected, including all
	// HTTP control characters and invalid UTF-8. No earlier length guard fires.
	for value := 0; value <= 255; value++ {
		if value >= 0x21 && value <= 0x7e {
			continue
		}
		err := ValidateAdminToken(strings.Repeat("x", 32) + string([]byte{byte(value)}))
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "only visible ASCII") {
			t.Fatalf("byte %d bypassed ASCII guard", value)
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
