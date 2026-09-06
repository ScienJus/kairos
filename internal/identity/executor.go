package identity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

const ExecutorTokenPrefix = "krs_claim_"

type ExecutorProfile string

const (
	TaskExecutor         ExecutorProfile = "task_executor"
	CoordinationExecutor ExecutorProfile = "coordination_executor"
)

// ExecutorScope is resolved exclusively from a stored Claim, never request fields.
type ExecutorScope struct {
	Profile       ExecutorProfile
	ClaimID       string
	TaskID        domain.TaskID
	WorkItemID    domain.WorkItemID
	CandidateKind domain.CoordinationClaimKind
	TokenHash     string
}

type Capability string

const (
	ScopedRead              Capability = "scoped_read"
	TaskArtifactWrite       Capability = "task_artifact_write"
	BlackboardPlanningWrite Capability = "blackboard_planning_write"
)

// ValidateCapability keeps executor credentials out of ordinary identity operations.
func (p Principal) ValidateCapability(capability Capability) error {
	if p.Executor == nil {
		return p.Validate()
	}
	e := p.Executor
	if p.Actor.Validate() != nil || p.Actor.Kind != domain.ActorAgent || p.Role != "" ||
		e.ClaimID == "" || e.WorkItemID == "" || len(e.TokenHash) != 64 {
		return fmt.Errorf("%w: invalid executor principal", ErrUnauthenticated)
	}
	switch e.Profile {
	case TaskExecutor:
		if e.TaskID == "" || e.CandidateKind != "" {
			return ErrUnauthenticated
		}
	case CoordinationExecutor:
		if e.TaskID != "" || !e.CandidateKind.Valid() {
			return ErrUnauthenticated
		}
	default:
		return ErrUnauthenticated
	}
	if capability == ScopedRead {
		return nil
	}
	if e.Profile == TaskExecutor && (capability == TaskArtifactWrite || capability == BlackboardPlanningWrite) {
		return nil
	}
	return ErrForbidden
}

// ExecutorTokenHash validates the canonical prefixed, unpadded 256-bit token.
func ExecutorTokenHash(token string) (string, error) {
	if !strings.HasPrefix(token, ExecutorTokenPrefix) {
		return "", fmt.Errorf("%w: invalid executor token format", ErrInvalid)
	}
	encoded := strings.TrimPrefix(token, ExecutorTokenPrefix)
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return "", fmt.Errorf("%w: executor token must contain 256 random bits encoded as unpadded base64url", ErrInvalid)
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:]), nil
}

// WithExecutorAuthenticator extends authenticated transports without changing Trusted Mode.
func WithExecutorAuthenticator(resolver Resolver, executor Authenticator) Resolver {
	switch r := resolver.(type) {
	case AuthenticatedResolver:
		r.ExecutorAuthenticator = executor
		return r
	case *AuthenticatedResolver:
		copy := *r
		copy.ExecutorAuthenticator = executor
		return &copy
	default:
		return resolver
	}
}
