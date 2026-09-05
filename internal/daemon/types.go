// Package daemon runs one explicitly selected Kairos candidate through a Harness.
// It does not discover work or implement a continuous scheduler.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

type CandidateKind string

const (
	TaskCandidate        CandidateKind = "task"
	EmptyBlackboard      CandidateKind = "empty_blackboard"
	BlackboardCompletion CandidateKind = "blackboard_completion"
	WorkItemAcceptance   CandidateKind = "work_item_acceptance"
)

type Candidate struct {
	Kind       CandidateKind           `json:"kind"`
	WorkItemID domain.WorkItemID       `json:"work_item_id"`
	TaskID     domain.TaskID           `json:"task_id,omitempty"`
	Mode       domain.CoordinationMode `json:"mode"`
}

func (c Candidate) Validate() error {
	if strings.TrimSpace(string(c.WorkItemID)) == "" {
		return errors.New("work item id is required")
	}
	if c.Mode != domain.CoordinationModeWorkflow && c.Mode != domain.CoordinationModeBlackboard {
		return errors.New("invalid coordination mode")
	}
	if c.Kind == TaskCandidate {
		if strings.TrimSpace(string(c.TaskID)) == "" {
			return errors.New("task id is required")
		}
		return nil
	}
	if c.TaskID != "" || c.Mode != domain.CoordinationModeBlackboard || !domain.CoordinationClaimKind(c.Kind).Valid() {
		return errors.New("invalid coordination candidate")
	}
	return nil
}

// Secret must be explicitly revealed to a transport or Adapter, never formatted.
type Secret struct{ value string }

func NewSecret(value string) Secret         { return Secret{value: value} }
func (s Secret) Reveal() string             { return s.value }
func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }

func randomID() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

type DispatchState string

const (
	Prepared   DispatchState = "prepared"
	Claimed    DispatchState = "claimed"
	Starting   DispatchState = "starting"
	Running    DispatchState = "running"
	Finalizing DispatchState = "finalizing"
	Stopping   DispatchState = "stopping"
	Finished   DispatchState = "finished"
	Lost       DispatchState = "lost"
)

type RunState string

const (
	RunStarting   RunState = "starting"
	RunRunning    RunState = "running"
	OutcomeReady  RunState = "outcome_ready"
	RuntimeFailed RunState = "runtime_failed"
	RunStopped    RunState = "stopped"
	RunLost       RunState = "lost"
)

type StopReason string

const (
	StopRequested      StopReason = "requested"
	StopLeaseLost      StopReason = "lease_lost"
	StopAuthorityLost  StopReason = "authority_lost"
	StopRuntimeFailure StopReason = "runtime_failure"
	StopProtocolError  StopReason = "protocol_error"
	StopCoreRejected   StopReason = "core_rejected"
)

// RunRef is an Adapter-owned, secret-free reference valid in this process.
type RunRef struct {
	ID string `json:"id"`
}

type StartRequest struct {
	Candidate     Candidate
	ClaimID       string
	Attempt       int
	MCPURL        string
	ExecutorToken Secret
}

type RunObservation struct {
	State   RunState
	Outcome *HarnessOutcome
}

// Adapter methods must honor cancellation. Start returns an error only after
// proving that no run remains. Observe errors do not prove termination. Stop
// is an idempotent best-effort request; Observe must confirm termination.
type Adapter interface {
	Probe(context.Context) error
	Start(context.Context, StartRequest) (RunRef, error)
	Observe(context.Context, RunRef) (RunObservation, error)
	Stop(context.Context, RunRef, StopReason) error
}

type Claim struct {
	ID           string
	Executor     domain.ActorRef
	LeaseSeconds int64
	Active       bool
	EndReason    string
	EndedAt      *time.Time
}

// ClaimStatus includes immutable business history used to reconcile a lost
// finalization response. A missing Claim is an error, not an ended Claim.
type ClaimStatus struct {
	Claim          Claim
	Task           *domain.Task
	Artifacts      []domain.Artifact
	Tasks          []domain.Task
	WorkItemResult string
}

type Core interface {
	// Claim returns ClaimAttemptError when transport progress is known. Other
	// errors conservatively mean the acquisition may have committed.
	Claim(context.Context, Candidate, string, Secret, int64) (Claim, error)
	Heartbeat(context.Context, Candidate, string, int64) (Claim, error)
	Inspect(context.Context, Candidate, string) (ClaimStatus, error)
	Apply(context.Context, Candidate, string, string, HarnessOutcome) error
	Release(context.Context, Candidate, string, string) error
}

type ClaimAttemptState string

const (
	// ClaimNotSent says this attempt never sent a Claim mutation. It cannot
	// resolve uncertainty left by an earlier attempt.
	ClaimNotSent ClaimAttemptState = "not_sent"
	// ClaimRejected says a stable idempotent retry authoritatively established
	// that this operation has no successful Claim to replay.
	ClaimRejected ClaimAttemptState = "rejected"
)

type ClaimAttemptError struct {
	State ClaimAttemptState
	Err   error
}

func (e *ClaimAttemptError) Error() string { return e.Err.Error() }
func (e *ClaimAttemptError) Unwrap() error { return e.Err }

// APIError deliberately omits server text, which may contain request secrets.
type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string { return fmt.Sprintf("Core HTTP %d (%s)", e.Status, e.Code) }
func statusIs(err error, statuses ...int) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	for _, status := range statuses {
		if api.Status == status {
			return true
		}
	}
	return false
}

func coreRejected(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.Status >= 300 && api.Status < 500 && api.Status != 408 && api.Status != 429
}

// Clock must support concurrent calls and preserve monotonic elapsed time.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type Options struct {
	Lease time.Duration
	// PollInterval controls Harness observation only, never lease safety.
	PollInterval   time.Duration
	RequestTimeout time.Duration
	StopTimeout    time.Duration
	MaxAttempts    int
	MCPURL         string
	Clock          Clock
}

func DefaultOptions() Options {
	return Options{Lease: 5 * time.Minute,
		PollInterval: time.Second, RequestTimeout: 10 * time.Second, StopTimeout: 30 * time.Second, MaxAttempts: 2, Clock: realClock{}}
}

func (o Options) validate() error {
	if o.Clock == nil || o.Lease < 15*time.Second || o.Lease > 30*time.Minute || o.Lease%time.Second != 0 ||
		o.PollInterval <= 0 || o.RequestTimeout <= 0 || o.StopTimeout <= 0 || o.MaxAttempts < 1 ||
		heartbeatInterval(o.Lease)+o.RequestTimeout+leaseSafetyMargin(o.Lease) >= o.Lease {
		return errors.New("invalid Dispatch timing or retry options")
	}
	if strings.TrimSpace(o.MCPURL) == "" {
		return errors.New("MCP URL is required")
	}
	return nil
}

func heartbeatInterval(lease time.Duration) time.Duration    { return lease / 5 }
func leaseSafetyMargin(lease time.Duration) time.Duration    { return lease / 10 }
func controlRetryInterval(lease time.Duration) time.Duration { return min(time.Second, lease/10) }
