package identity

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

type identityTestClock struct{ now time.Time }

func (clock identityTestClock) Now() time.Time { return clock.now }

func TestServiceClockUsesUTCMicroseconds(t *testing.T) {
	raw := time.Date(2026, time.August, 24, 20, 0, 0, 123456789, time.FixedZone("test", 8*60*60))
	got := (microsecondClock{Clock: identityTestClock{now: raw}}).Now()
	want := raw.UTC().Truncate(time.Microsecond)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("normalized identity time = %s, want %s in UTC", got, want)
	}
}

func TestTrustedResolverResolvesAgentRole(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set(HeaderActorID, "codex-backend")
	request.Header.Set(HeaderActorRole, "backend")

	got, err := (TrustedResolver{}).Resolve(request)
	if err != nil {
		t.Fatalf("resolve trusted identity: %v", err)
	}
	if got.Actor.Kind != domain.ActorAgent || got.Actor.ID != "codex-backend" {
		t.Fatalf("unexpected identity: %+v", got)
	}
	if got.Role != "backend" || !got.HasAnyRole([]string{"frontend", "backend"}) {
		t.Fatalf("role = %q, expected backend match", got.Role)
	}
}

func TestTrustedResolverRequiresActorID(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	_, err := (TrustedResolver{}).Resolve(request)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want unauthenticated", err)
	}
}

func TestHumanIdentityRejectsRoles(t *testing.T) {
	identity := Identity{
		Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "human-1"},
		Role:  "backend",
	}
	if err := identity.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want invalid identity", err)
	}
}
