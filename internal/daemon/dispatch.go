package daemon

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

// Snapshot is safe to expose to a scheduler or logger. It contains no tokens,
// outcome payloads, provider errors, or Artifact contents.
type Snapshot struct {
	Candidate      Candidate
	State          DispatchState
	RunState       RunState
	ClaimID        string
	RunRef         RunRef
	Attempts       int
	StopReason     StopReason
	ClaimEnded     bool
	EndReason      string
	OutcomeApplied bool
	Outcome        OutcomeKind
}

func (s Snapshot) Terminal() bool { return s.State == Finished || s.State == Lost }

// Dispatch retains all recovery state in memory. Step and Heartbeat may run
// concurrently; Step calls are serialized. Retain a nonterminal Dispatch when
// Run is interrupted and resume the same instance, not a new Claim.
type Dispatch struct {
	core              Core
	adapter           Adapter
	options           Options
	candidate         Candidate
	executorToken     Secret
	claimOperation    string
	outcomeOperation  string
	stepMu            sync.Mutex
	heartbeatMu       sync.Mutex
	runMu             sync.Mutex
	mu                sync.Mutex
	snapshot          Snapshot
	claim             Claim
	uncertainClaim    bool
	confirmed         bool
	safeUntil         time.Time
	nextHeartbeat     time.Time
	stopAt            time.Time
	stopSent          bool
	observeFailureAt  time.Time
	intent            *HarnessOutcome
	applyAcknowledged bool
	operationCancel   context.CancelFunc
	heartbeatWake     chan struct{}
	stopWake          chan struct{}
}

func NewDispatch(core Core, adapter Adapter, candidate Candidate, options Options) (*Dispatch, error) {
	if core == nil || adapter == nil {
		return nil, errors.New("Core and Adapter are required")
	}
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	if err := options.validate(); err != nil {
		return nil, err
	}
	claimOperation, err := randomID()
	if err != nil {
		return nil, err
	}
	outcomeOperation, err := randomID()
	if err != nil {
		return nil, err
	}
	token, err := randomID()
	if err != nil {
		return nil, err
	}
	return &Dispatch{core: core, adapter: adapter, candidate: candidate, options: options,
		executorToken: NewSecret(identity.ExecutorTokenPrefix + token), claimOperation: claimOperation, outcomeOperation: outcomeOperation,
		heartbeatWake: make(chan struct{}, 1), stopWake: make(chan struct{}),
		snapshot: Snapshot{Candidate: candidate, State: Prepared}}, nil
}

func (d *Dispatch) Snapshot() Snapshot { d.mu.Lock(); defer d.mu.Unlock(); return d.snapshot }

func (d *Dispatch) RequestStop(reason StopReason) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopLocked(reason)
}

func (d *Dispatch) stopLocked(reason StopReason) {
	if d.snapshot.Terminal() || d.snapshot.StopReason != "" {
		return
	}
	if reason == "" {
		reason = StopRequested
	}
	d.snapshot.StopReason = reason
	d.snapshot.State = Stopping
	d.stopAt = d.options.Clock.Now().Add(d.options.StopTimeout)
	close(d.stopWake)
	d.wakeHeartbeat()
	if d.operationCancel != nil {
		d.operationCancel()
	}
}

func (d *Dispatch) wakeHeartbeat() {
	select {
	case d.heartbeatWake <- struct{}{}:
	default:
	}
}

// Heartbeat is independent of slow Adapter and finalization calls. Deadlines
// use local elapsed time from request start, never the server wall clock.
func (d *Dispatch) Heartbeat(ctx context.Context) error {
	d.heartbeatMu.Lock()
	defer d.heartbeatMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	if d.snapshot.Terminal() || d.snapshot.StopReason != "" || d.claim.ID == "" || !d.claim.Active {
		d.mu.Unlock()
		return nil
	}
	start := d.options.Clock.Now()
	if !start.Before(d.safeUntil) {
		d.stopLocked(StopLeaseLost)
		d.mu.Unlock()
		return nil
	}
	if start.Before(d.nextHeartbeat) {
		d.mu.Unlock()
		return nil
	}
	claim := d.claim
	timeout := min(d.options.RequestTimeout, d.safeUntil.Sub(start))
	d.mu.Unlock()
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	renewed, err := d.core.Heartbeat(callCtx, d.candidate, claim.ID, int64(d.options.Lease/time.Second))
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.snapshot.Terminal() || d.snapshot.StopReason != "" {
		return err
	}
	if err != nil {
		if coreRejected(err) {
			d.stopLocked(StopAuthorityLost)
		}
		if !d.options.Clock.Now().Before(d.safeUntil) {
			d.stopLocked(StopLeaseLost)
		}
		d.nextHeartbeat = d.options.Clock.Now().Add(controlRetryInterval(time.Duration(claim.LeaseSeconds) * time.Second))
		d.wakeHeartbeat()
		return err
	}
	if renewed.ID != claim.ID || renewed.Executor != claim.Executor || !renewed.Active || renewed.LeaseSeconds < 15 || renewed.LeaseSeconds > 1800 {
		d.stopLocked(StopAuthorityLost)
		return errors.New("Core heartbeat did not confirm bound execution authority")
	}
	lease := time.Duration(renewed.LeaseSeconds) * time.Second
	d.claim.LeaseSeconds = renewed.LeaseSeconds
	d.safeUntil = start.Add(lease - leaseSafetyMargin(lease))
	if !d.options.Clock.Now().Before(d.safeUntil) {
		d.stopLocked(StopLeaseLost)
		return nil
	}
	d.confirmed = true
	d.nextHeartbeat = start.Add(heartbeatInterval(lease))
	d.wakeHeartbeat()
	return nil
}

// Step advances at most one phase. Errors leave the Snapshot authoritative:
// transport errors are retryable, not proof that a Dispatch is terminal.
func (d *Dispatch) Step(ctx context.Context) error {
	d.stepMu.Lock()
	defer d.stepMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	if d.snapshot.Terminal() {
		d.mu.Unlock()
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, d.options.RequestTimeout)
	d.operationCancel = cancel
	d.mu.Unlock()
	defer func() { cancel(); d.mu.Lock(); d.operationCancel = nil; d.mu.Unlock() }()
	snapshot := d.Snapshot()
	if snapshot.ClaimID == "" {
		return d.acquire(callCtx)
	}
	if snapshot.StopReason != "" {
		return d.stop(callCtx)
	}
	switch snapshot.State {
	case Claimed, Starting:
		return d.start(callCtx)
	case Running:
		return d.observe(callCtx)
	case Finalizing:
		return d.finalize(callCtx)
	default:
		return errors.New("invalid Dispatch state")
	}
}

func (d *Dispatch) acquire(ctx context.Context) error {
	d.mu.Lock()
	if d.snapshot.StopReason != "" && !d.uncertainClaim {
		d.snapshot.State = Finished
		d.mu.Unlock()
		return nil
	}
	started := d.options.Clock.Now()
	d.mu.Unlock()
	claim, err := d.core.Claim(ctx, d.candidate, d.claimOperation, d.executorToken, int64(d.options.Lease/time.Second))
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		var attempt *ClaimAttemptError
		known := errors.As(err, &attempt)
		if known && attempt.State == ClaimRejected {
			d.uncertainClaim = false
		}
		if (known && attempt.State == ClaimRejected) || (coreRejected(err) && !d.uncertainClaim) {
			d.snapshot.State = Finished
			if d.snapshot.StopReason == "" {
				d.snapshot.StopReason = StopCoreRejected
			}
		} else if !known || attempt.State != ClaimNotSent {
			d.uncertainClaim = true
		}
		return err
	}
	if claim.ID == "" || claim.Executor.Kind != domain.ActorAgent || claim.Executor.Validate() != nil || (claim.Active && (claim.LeaseSeconds < 15 || claim.LeaseSeconds > 1800)) {
		d.uncertainClaim = true
		return errors.New("invalid Core Claim response")
	}
	d.claim = claim
	d.uncertainClaim = false
	d.snapshot.ClaimID = claim.ID
	if !claim.Active {
		d.snapshot.ClaimEnded, d.snapshot.EndReason, d.snapshot.State = true, claim.EndReason, Finished
		return nil
	}
	lease := time.Duration(claim.LeaseSeconds) * time.Second
	d.safeUntil = started.Add(lease - leaseSafetyMargin(lease))
	d.wakeHeartbeat()
	if d.snapshot.StopReason == "" {
		d.snapshot.State = Claimed
	}
	return nil
}

func (d *Dispatch) start(ctx context.Context) error {
	if err := d.Heartbeat(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	if d.snapshot.StopReason != "" || !d.confirmed {
		d.mu.Unlock()
		return nil
	}
	d.snapshot.State, d.snapshot.RunState = Starting, RunStarting
	d.snapshot.Attempts++
	attempt := d.snapshot.Attempts
	claimID := d.claim.ID
	d.mu.Unlock()
	ref, err := d.adapter.Start(ctx, StartRequest{Candidate: d.candidate, ClaimID: claimID, Attempt: attempt,
		MCPURL: d.options.MCPURL, ExecutorToken: d.executorToken})
	d.mu.Lock()
	defer d.mu.Unlock()
	if (err != nil && ref.ID != "") || (err == nil && ref.ID == "") {
		// A contract-violating Adapter must not cause a second live run.
		d.snapshot.RunRef, d.snapshot.RunState = ref, RunLost
		d.stopLocked(StopProtocolError)
		return errors.New("Adapter violated atomic Start contract")
	}
	if err != nil {
		d.runtimeFailedLocked()
		return errors.New("Harness Start failed")
	}
	d.snapshot.RunRef, d.snapshot.RunState = ref, RunRunning
	if d.snapshot.StopReason == "" {
		d.snapshot.State = Running
	}
	return nil
}

func (d *Dispatch) runtimeFailedLocked() {
	d.snapshot.RunState = RuntimeFailed
	if d.snapshot.StopReason != "" {
		return
	}
	if d.snapshot.Attempts >= d.options.MaxAttempts {
		d.stopLocked(StopRuntimeFailure)
		return
	}
	d.snapshot.State = Starting
}

func (d *Dispatch) observe(ctx context.Context) error {
	d.mu.Lock()
	ref := d.snapshot.RunRef
	var deadline time.Time
	if !d.observeFailureAt.IsZero() {
		deadline = d.observeFailureAt.Add(d.options.StopTimeout)
		remaining := deadline.Sub(d.options.Clock.Now())
		if remaining <= 0 {
			d.stopLocked(StopRuntimeFailure)
			d.mu.Unlock()
			return errors.New("Harness observation deadline exceeded")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, remaining)
		defer cancel()
	}
	d.mu.Unlock()
	observation, err := d.adapter.Observe(ctx, ref)
	d.mu.Lock()
	defer d.mu.Unlock()
	// A reply at or after the recovery deadline cannot reopen the failure
	// window. Stopping will separately confirm whether the run has ended.
	if !deadline.IsZero() && !d.options.Clock.Now().Before(deadline) {
		d.stopLocked(StopRuntimeFailure)
		return errors.New("Harness observation deadline exceeded")
	}
	if err != nil {
		if d.observeFailureAt.IsZero() {
			d.observeFailureAt = d.options.Clock.Now()
		}
		return errors.New("Harness observation unavailable")
	}
	d.observeFailureAt = time.Time{}
	switch observation.State {
	case RunStarting, RunRunning:
		d.snapshot.RunState = observation.State
	case OutcomeReady:
		d.snapshot.RunState = OutcomeReady
		if d.snapshot.StopReason != "" {
			return nil
		}
		if observation.Outcome == nil || observation.Outcome.Validate(d.candidate) != nil {
			d.runtimeFailedLocked()
			return errors.New("invalid Harness outcome")
		}
		frozen, err := freezeOutcome(*observation.Outcome)
		if err != nil {
			d.runtimeFailedLocked()
			return err
		}
		d.intent, d.snapshot.State = &frozen, Finalizing
		d.snapshot.Outcome = frozen.Kind()
	case RuntimeFailed:
		d.runtimeFailedLocked()
	case RunStopped:
		d.snapshot.RunState = RunStopped
		d.stopLocked(StopRequested)
	case RunLost:
		d.snapshot.RunState = RunLost
		d.stopLocked(StopRuntimeFailure)
	default:
		// An invalid observation does not prove the process exited.
		d.stopLocked(StopProtocolError)
		return errors.New("invalid Harness run state")
	}
	return nil
}

func (d *Dispatch) inspect(ctx context.Context) (ClaimStatus, error) {
	s := d.Snapshot()
	status, err := d.core.Inspect(ctx, d.candidate, s.ClaimID)
	if err != nil {
		return status, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if status.Claim.ID != d.claim.ID || status.Claim.Executor != d.claim.Executor {
		return status, errors.New("Core Claim history identity mismatch")
	}
	if !status.Claim.Active {
		d.claim.Active = false
		d.snapshot.ClaimEnded, d.snapshot.EndReason = true, status.Claim.EndReason
	}
	return status, nil
}

func (d *Dispatch) finalize(ctx context.Context) error {
	status, err := d.inspect(ctx)
	if err != nil {
		return err
	}
	if !status.Claim.Active {
		d.finish(status)
		return nil
	}
	d.mu.Lock()
	if d.snapshot.StopReason != "" {
		d.mu.Unlock()
		return nil
	}
	intent := *d.intent
	d.mu.Unlock()
	err = d.core.Apply(ctx, d.candidate, status.Claim.ID, d.outcomeOperation, intent)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err == nil {
		d.applyAcknowledged = true
		return nil
	}
	if coreRejected(err) {
		d.stopLocked(StopCoreRejected)
	}
	return err
}

func runEnded(state RunState) bool {
	return state == OutcomeReady || state == RuntimeFailed || state == RunStopped
}

func (d *Dispatch) stop(ctx context.Context) error {
	s := d.Snapshot()
	if s.RunRef.ID != "" && !runEnded(s.RunState) {
		d.mu.Lock()
		expired, sent := !d.options.Clock.Now().Before(d.stopAt), d.stopSent
		d.mu.Unlock()
		// Even if the confirmation window elapsed before this Step, issue the
		// best-effort termination request before releasing responsibility.
		if !sent {
			err := d.adapter.Stop(ctx, s.RunRef, s.StopReason)
			if err == nil || expired || s.RunState == RunLost {
				d.mu.Lock()
				d.stopSent = true
				d.mu.Unlock()
			}
		}
		d.mu.Lock()
		expired = !d.options.Clock.Now().Before(d.stopAt)
		d.mu.Unlock()
		if expired || s.RunState == RunLost {
			d.mu.Lock()
			d.snapshot.RunState = RunLost
			d.mu.Unlock()
		} else {
			observation, err := d.adapter.Observe(ctx, s.RunRef)
			if err == nil && (runEnded(observation.State) || observation.State == RunLost) {
				d.mu.Lock()
				d.snapshot.RunState = observation.State
				d.mu.Unlock()
			}
		}
	}
	status, err := d.inspect(ctx)
	if err != nil {
		return err
	}
	s = d.Snapshot()
	ended := s.RunRef.ID == "" || runEnded(s.RunState) || s.RunState == RunLost
	if !ended {
		return nil
	}
	if !status.Claim.Active {
		d.finish(status)
		return nil
	}
	// Release has its own Claim-kind route. A lost response is checked against
	// Core history on the next Step; it is never treated as confirmed completion.
	return d.core.Release(ctx, d.candidate, s.ClaimID, string(s.StopReason))
}

func (d *Dispatch) finish(status ClaimStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.snapshot.State = Finished
	if d.snapshot.RunState == RunLost {
		d.snapshot.State = Lost
	}
	if d.intent != nil {
		d.snapshot.OutcomeApplied = outcomeMatches(d.candidate, status, *d.intent, d.applyAcknowledged)
	}
}

// Run drives Step with an independent heartbeat guard. Cancellation requests
// Stop and performs one bounded cleanup pass. An unresolved Snapshot stays
// nonterminal and must retain its scheduler slot until this instance is resumed.
func (d *Dispatch) Run(ctx context.Context) (Snapshot, error) {
	d.runMu.Lock()
	defer d.runMu.Unlock()
	guardCtx, cancelGuard := context.WithCancel(ctx)
	guardDone := make(chan struct{})
	go func() {
		defer close(guardDone)
		for {
			_ = d.Heartbeat(guardCtx)
			timer := d.heartbeatTimer()
			select {
			case <-guardCtx.Done():
				return
			case <-d.heartbeatWake:
			case <-timer:
			}
		}
	}()
	defer func() { cancelGuard(); <-guardDone }()
	for {
		if ctx.Err() != nil {
			d.RequestStop(StopRequested)
			cleanup, cancel := context.WithTimeout(context.Background(), d.options.RequestTimeout)
			_ = d.Step(cleanup)
			cancel()
			return d.Snapshot(), ctx.Err()
		}
		before := d.Snapshot()
		err := d.Step(ctx)
		if s := d.Snapshot(); s.Terminal() {
			return s, err
		}
		d.mu.Lock()
		state := d.snapshot.State
		var stopWake <-chan struct{}
		if d.snapshot.StopReason == "" {
			stopWake = d.stopWake
		}
		delay := controlRetryInterval(d.options.Lease)
		if state == Running {
			delay = d.options.PollInterval
			if !d.observeFailureAt.IsZero() {
				remaining := d.observeFailureAt.Add(d.options.StopTimeout).Sub(d.options.Clock.Now())
				delay = min(delay, controlRetryInterval(d.options.Lease), max(0, remaining))
			}
		}
		if state == Stopping {
			remaining := d.stopAt.Sub(d.options.Clock.Now())
			if remaining > 0 {
				delay = min(delay, remaining)
			}
		}
		d.mu.Unlock()
		// Lifecycle transitions progress immediately; only observation and
		// retries wait. A stop signal interrupts even a long Observe poll.
		if before.State != state {
			continue
		}
		select {
		case <-ctx.Done():
		case <-stopWake:
		case <-d.options.Clock.After(delay):
		}
	}
}

func (d *Dispatch) heartbeatTimer() <-chan time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.snapshot.Terminal() || d.snapshot.StopReason != "" || d.claim.ID == "" || !d.claim.Active {
		return nil
	}
	deadline := d.nextHeartbeat
	if deadline.IsZero() || d.safeUntil.Before(deadline) {
		deadline = d.safeUntil
	}
	return d.options.Clock.After(max(0, deadline.Sub(d.options.Clock.Now())))
}
