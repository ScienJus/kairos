package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SchedulerOptions struct {
	Dispatch                                                    Options
	Slots, DiscoveryLimit, MaxDispatches                        int
	DiscoveryInterval, ProbeInterval, Cooldown, ShutdownTimeout time.Duration
	WorkspaceRoot                                               string
	Tags                                                        []string
	Logger                                                      *slog.Logger
}

func DefaultSchedulerOptions() SchedulerOptions {
	return SchedulerOptions{Dispatch: DefaultOptions(), Slots: 1, DiscoveryLimit: 50,
		MaxDispatches: 3, DiscoveryInterval: 5 * time.Second, ProbeInterval: 30 * time.Second,
		Cooldown: time.Minute, ShutdownTimeout: 30 * time.Second, WorkspaceRoot: ".kairos-daemon"}
}

type SchedulerStats struct {
	Active                                                                                            int
	Paused                                                                                            bool
	Claims, Heartbeats, HeartbeatFailures, Probes, ProbeFailures, Retries, Suppressed, Finished, Lost uint64
}

type suppression struct {
	generation  string
	failures    int
	until       time.Time
	quarantined bool
}

type scheduledRun struct {
	dispatch *Dispatch
	work     DiscoveredCandidate
	started  time.Time
}

// Scheduler owns process-local admissions for one Run. Unresolved Dispatches
// remain in Active after shutdown; they are never declared ended locally.
type Scheduler struct {
	core         DiscoveryCore
	adapter      Adapter
	options      SchedulerOptions
	runStarted   bool
	probeGate    chan struct{}
	mu           sync.Mutex
	stats        SchedulerStats
	active       map[Candidate]*scheduledRun
	records      map[Candidate]suppression
	cursor       int
	nextProbe    time.Time
	probeBackoff time.Duration
	healthSerial uint64
}

func NewScheduler(core DiscoveryCore, adapter Adapter, options SchedulerOptions) (*Scheduler, error) {
	if core == nil || adapter == nil {
		return nil, errors.New("Core and Adapter are required")
	}
	if err := options.Dispatch.validate(); err != nil {
		return nil, err
	}
	if options.Slots < 1 || options.DiscoveryLimit < 1 || options.DiscoveryLimit > 50 || options.MaxDispatches < 1 ||
		options.DiscoveryInterval <= 0 || options.ProbeInterval <= 0 || options.Cooldown <= 0 || options.ShutdownTimeout <= 0 || options.WorkspaceRoot == "" {
		return nil, errors.New("invalid scheduler limits, timing, or workspace root")
	}
	if err := validateTaskSpecSet("tags", options.Tags); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(options.WorkspaceRoot)
	if err != nil {
		return nil, errors.New("invalid workspace root")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, errors.New("cannot create workspace root")
	}
	options.WorkspaceRoot = root
	options.Tags = append([]string{}, options.Tags...)
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &Scheduler{core: core, adapter: adapter, options: options, probeGate: make(chan struct{}, 1), active: make(map[Candidate]*scheduledRun), records: make(map[Candidate]suppression)}, nil
}

func (s *Scheduler) Stats() SchedulerStats { s.mu.Lock(); defer s.mu.Unlock(); return s.stats }

var candidateOrder = [...]CandidateKind{WorkItemAcceptance, BlackboardCompletion, TaskCandidate, EmptyBlackboard}

func (s *Scheduler) signalSystemFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthSerial++
	if !s.stats.Paused {
		s.stats.Paused = true
		s.nextProbe = s.options.Dispatch.Clock.Now().Add(s.options.DiscoveryInterval)
		s.options.Logger.Warn("harness_paused")
	}
}

func (s *Scheduler) probe(ctx context.Context, force bool) bool {
	// Waiting for another Probe must not delay cancellation of Run or Dispatch.
	select {
	case s.probeGate <- struct{}{}:
		defer func() { <-s.probeGate }()
	case <-ctx.Done():
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	now := s.options.Dispatch.Clock.Now()
	if now.Before(s.nextProbe) && (s.stats.Paused || !force) {
		ok := !s.stats.Paused
		s.mu.Unlock()
		return ok
	}
	s.stats.Probes++
	serial := s.healthSerial
	s.mu.Unlock()
	call, cancel := context.WithTimeout(ctx, s.options.Dispatch.RequestTimeout)
	err := s.adapter.Probe(call)
	if err == nil {
		err = call.Err()
	}
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	// A caller aborts an observation, not Harness health. Check the parent,
	// not call: our own request timeout remains a failed health observation.
	if ctx.Err() != nil {
		return false
	}
	now = s.options.Dispatch.Clock.Now()
	if err != nil {
		s.stats.ProbeFailures++
		s.stats.Paused = true
		s.probeBackoff = min(max(s.options.DiscoveryInterval, s.probeBackoff*2), time.Minute)
		// Jitter only delays retries; it never bypasses the minimum backoff.
		s.nextProbe = now.Add(s.probeBackoff + time.Duration(rand.Int64N(max(1, int64(s.probeBackoff/4)))))
		s.options.Logger.Warn("probe_failed", "retry_after", s.nextProbe)
		return false
	}
	if serial != s.healthSerial {
		return false
	}
	s.stats.Paused = false
	s.probeBackoff = 0
	s.nextProbe = now.Add(s.options.ProbeInterval)
	s.options.Logger.Info("probe_healthy")
	return true
}

func (s *Scheduler) selectCandidate(rows []DiscoveredCandidate) (DiscoveredCandidate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stats.Paused || len(s.active) >= s.options.Slots {
		return DiscoveredCandidate{}, false
	}
	for offset := 0; offset < len(candidateOrder); offset++ {
		kind := candidateOrder[(s.cursor+offset)%len(candidateOrder)]
		for _, row := range rows {
			if row.Candidate.Kind != kind || row.Generation == "" || row.Candidate.Validate() != nil {
				continue
			}
			if _, ok := s.active[row.Candidate]; ok {
				continue
			}
			record, ok := s.records[row.Candidate]
			if ok && record.generation != row.Generation {
				delete(s.records, row.Candidate)
				ok = false
			}
			if ok && (record.quarantined || s.options.Dispatch.Clock.Now().Before(record.until)) {
				s.stats.Suppressed++
				continue
			}
			return row, true
		}
	}
	return DiscoveredCandidate{}, false
}

func (s *Scheduler) finish(run *scheduledRun) {
	result := run.dispatch.Snapshot()
	if !result.Terminal() {
		return
	}
	if result.ClaimID == "" && result.Attempts == 0 && result.RunRef.ID == "" {
		// Remove only an empty workspace after acquisition is definitively over.
		// Claimed or unresolved work retains its directory for inspection.
		if err := os.Remove(run.dispatch.options.Workspace); err != nil && !os.IsNotExist(err) {
			s.options.Logger.Warn("workspace_cleanup_failed")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, run.work.Candidate)
	s.stats.Active = len(s.active)
	s.stats.Finished++
	if result.State == Lost {
		s.stats.Lost++
	}
	if result.Attempts > 1 {
		s.stats.Retries += uint64(result.Attempts - 1)
	}
	if result.ClaimID != "" {
		record := s.records[run.work.Candidate]
		if record.generation != run.work.Generation {
			record = suppression{generation: run.work.Generation}
		}
		switch {
		case result.Outcome == Abandoned:
			record.quarantined = true
			s.records[run.work.Candidate] = record
		case result.OutcomeApplied:
			delete(s.records, run.work.Candidate)
		default:
			record.failures++
			record.until = s.options.Dispatch.Clock.Now().Add(s.options.Cooldown)
			record.quarantined = record.failures >= s.options.MaxDispatches
			s.records[run.work.Candidate] = record
		}
	}
	record := s.records[run.work.Candidate]
	s.options.Logger.Info("dispatch_ended", "kind", result.Candidate.Kind, "work_item_id", result.Candidate.WorkItemID,
		"task_id", result.Candidate.TaskID, "claim_id", result.ClaimID, "state", result.State, "reason", result.StopReason,
		"outcome", result.Outcome, "applied", result.OutcomeApplied, "attempts", result.Attempts,
		"generation", run.work.Generation, "failures", record.failures, "quarantined", record.quarantined, "cooldown_until", record.until,
		"duration", s.options.Dispatch.Clock.Now().Sub(run.started), "active", s.stats.Active)
}

var ErrSchedulerAlreadyRun = errors.New("Scheduler.Run may only be called once")

// Run is single-use, including concurrent calls and calls after cancellation.
// It stops admissions on cancellation and requests termination immediately.
// After ShutdownTimeout, unresolved work is retained in memory, not declared ended.
func (s *Scheduler) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.runStarted {
		s.mu.Unlock()
		return ErrSchedulerAlreadyRun
	}
	s.runStarted = true
	s.mu.Unlock()
	workers, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	done := make(chan *scheduledRun, s.options.Slots)
	launch := func(run *scheduledRun) {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = run.dispatch.Run(workers); done <- run }()
	}
	defer func() { cancel(); wg.Wait() }()
	poll := s.options.Dispatch.Clock.After(0)
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for _, run := range s.active {
				run.dispatch.RequestStop(StopRequested)
			}
			s.mu.Unlock()
			deadline := s.options.Dispatch.Clock.After(s.options.ShutdownTimeout)
			for s.Stats().Active > 0 {
				select {
				case run := <-done:
					s.finish(run)
				case <-deadline:
					cancel()
					wg.Wait()
					for len(done) > 0 {
						s.finish(<-done)
					}
					if s.Stats().Active > 0 {
						return errors.New("shutdown left unresolved Dispatches; slots retained")
					}
					return nil
				}
			}
			return nil
		case run := <-done:
			s.finish(run)
		case <-poll:
			if s.Stats().Active < s.options.Slots && s.probe(ctx, false) {
				s.admit(ctx, launch)
			}
			s.options.Logger.Info("scheduler_status", "stats", s.Stats())
			poll = s.options.Dispatch.Clock.After(s.options.DiscoveryInterval)
		}
	}
}

func (s *Scheduler) admit(ctx context.Context, launch func(*scheduledRun)) {
	rows, err := s.core.Discover(ctx, s.options.Tags, s.options.DiscoveryLimit, s.options.Dispatch.RequestTimeout)
	if err != nil {
		if statusIs(err, 401, 403) {
			s.mu.Lock()
			for _, run := range s.active {
				run.dispatch.RequestStop(StopAuthorityLost)
			}
			s.mu.Unlock()
			return
		}
		s.options.Logger.Warn("discovery_failed")
		// A later context request must not discard already resolved candidates.
		// Cancellation still prevents admissions below.
	}
	for ctx.Err() == nil {
		row, ok := s.selectCandidate(rows)
		if !ok {
			return
		}
		workspace, err := os.MkdirTemp(s.options.WorkspaceRoot, "dispatch-")
		if err != nil {
			s.options.Logger.Warn("workspace_failed")
			return
		}
		options := s.options.Dispatch
		options.Workspace = workspace
		core := &schedulerCore{Core: s.core, scheduler: s}
		dispatch, err := NewDispatch(core, &schedulerAdapter{Adapter: s.adapter, scheduler: s}, row.Candidate, options)
		if err != nil {
			_ = os.Remove(workspace) // Newly created and empty: no Harness has run.
			s.options.Logger.Warn("dispatch_prepare_failed")
			return
		}
		run := &scheduledRun{dispatch: dispatch, work: row, started: options.Clock.Now()}
		dispatch.claimAdmission = func(ctx context.Context) bool { return s.probe(ctx, true) }
		s.mu.Lock()
		s.active[row.Candidate] = run
		s.stats.Active = len(s.active)
		s.mu.Unlock()
		// Serialize the first acquisition so successful Claims advance rotation
		// before the next admission. Unknown responses keep this reserved slot.
		_ = dispatch.claimOnce(ctx)
		if dispatch.Snapshot().Terminal() {
			s.finish(run)
			return
		}
		launch(run)
		if dispatch.Snapshot().ClaimID == "" {
			return
		}
	}
}

type schedulerCore struct {
	Core
	scheduler *Scheduler
	once      sync.Once
}

func (c *schedulerCore) Claim(ctx context.Context, candidate Candidate, op string, token Secret, lease int64) (Claim, error) {
	claim, err := c.Core.Claim(ctx, candidate, op, token, lease)
	if err == nil && claim.ID != "" {
		c.once.Do(func() {
			s := c.scheduler
			s.mu.Lock()
			defer s.mu.Unlock()
			s.stats.Claims++
			for i, kind := range candidateOrder {
				if candidate.Kind == kind {
					s.cursor = (i + 1) % len(candidateOrder)
					break
				}
			}
			s.options.Logger.Info("claim_acquired", "kind", candidate.Kind, "work_item_id", candidate.WorkItemID, "task_id", candidate.TaskID, "claim_id", claim.ID)
		})
	}
	return claim, err
}

func (c *schedulerCore) Heartbeat(ctx context.Context, candidate Candidate, id string, lease int64) (Claim, error) {
	claim, err := c.Core.Heartbeat(ctx, candidate, id, lease)
	s := c.scheduler
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.Heartbeats++
	if err != nil {
		s.stats.HeartbeatFailures++
	}
	return claim, err
}

type schedulerAdapter struct {
	Adapter
	scheduler *Scheduler
}

func (a *schedulerAdapter) Start(ctx context.Context, r StartRequest) (RunRef, error) {
	ref, err := a.Adapter.Start(ctx, r)
	var system *SystemError
	if errors.As(err, &system) {
		a.scheduler.signalSystemFailure()
	}
	return ref, err
}

func (a *schedulerAdapter) Observe(ctx context.Context, r RunRef) (RunObservation, error) {
	observation, err := a.Adapter.Observe(ctx, r)
	var system *SystemError
	if errors.As(err, &system) || observation.SystemFailure {
		a.scheduler.signalSystemFailure()
	}
	return observation, err
}

func (a *schedulerAdapter) Stop(ctx context.Context, r RunRef, reason StopReason) error {
	err := a.Adapter.Stop(ctx, r, reason)
	var system *SystemError
	if errors.As(err, &system) {
		a.scheduler.signalSystemFailure()
	}
	return err
}
