package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestExecutorTokenAndCapabilityValidation(t *testing.T) {
	token := ExecutorTokenPrefix + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	hash, err := ExecutorTokenHash(token)
	if err != nil || len(hash) != 64 {
		t.Fatalf("executor token hash = %q, err %v", hash, err)
	}
	for _, invalid := range []string{"identity-token", ExecutorTokenPrefix, token + "="} {
		if _, err := ExecutorTokenHash(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("token %q error = %v, want invalid", invalid, err)
		}
	}

	principal := Principal{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "executor"},
		Executor: &ExecutorScope{
			Profile: TaskExecutor, ClaimID: "claim-1", TaskID: "task-1", WorkItemID: "work-item-1", TokenHash: hash,
		},
	}
	if err := principal.ValidateCapability(ScopedRead); err != nil {
		t.Fatalf("task executor scoped read: %v", err)
	}
	if err := principal.ValidateCapability(TaskArtifactWrite); err != nil {
		t.Fatalf("task executor artifact write: %v", err)
	}
	principal.Executor.Profile = CoordinationExecutor
	principal.Executor.TaskID = ""
	principal.Executor.CandidateKind = domain.CoordinationClaimEmptyBlackboard
	if err := principal.ValidateCapability(TaskArtifactWrite); !errors.Is(err, ErrForbidden) {
		t.Fatalf("coordination executor artifact error = %v, want forbidden", err)
	}
}

type staticAuthenticator struct {
	identity Identity
	calls    int
	err      error
}

func (a *staticAuthenticator) Authenticate(context.Context, string) (Identity, error) {
	a.calls++
	return a.identity, a.err
}

func TestAuthenticatedResolverRoutesExecutorFormat(t *testing.T) {
	regular := &staticAuthenticator{identity: Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "regular"}, Role: "backend"}}
	executor := &staticAuthenticator{identity: Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "executor"}}}
	resolver := AuthenticatedResolver{Authenticator: regular, ExecutorAuthenticator: executor}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	token := ExecutorTokenPrefix + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	request.Header.Set("Authorization", "Bearer "+token)
	got, err := resolver.Resolve(request)
	if err != nil || got.Actor.ID != "executor" || regular.calls != 0 || executor.calls != 1 {
		t.Fatalf("executor resolution = %+v, err %v, calls regular=%d executor=%d", got, err, regular.calls, executor.calls)
	}
	for _, legacy := range []string{"identity-token", ExecutorTokenPrefix + strings.Repeat("A", 33), token[:len(token)-1] + "B"} {
		request.Header.Set("Authorization", "Bearer "+legacy)
		got, err := resolver.Resolve(request)
		if err != nil || got.Actor.ID != "regular" || executor.calls != 1 {
			t.Fatalf("legacy resolution = %+v, err %v, executor calls=%d", got, err, executor.calls)
		}
	}
	executor.err = ErrUnauthenticated
	request.Header.Set("Authorization", "Bearer "+token)
	if _, err := resolver.Resolve(request); !errors.Is(err, ErrUnauthenticated) || regular.calls != 3 {
		t.Fatalf("Claim lookup must not fall back: err=%v identity calls=%d", err, regular.calls)
	}
	resolver.ExecutorAuthenticator = nil
	if _, err := resolver.Resolve(request); !errors.Is(err, ErrUnauthenticated) || regular.calls != 3 {
		t.Fatalf("missing Claim authenticator must not fall back: err=%v identity calls=%d", err, regular.calls)
	}
}
