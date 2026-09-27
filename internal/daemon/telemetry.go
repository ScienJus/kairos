package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand"
	"sort"
	"sync"
	"time"

	"github.com/ScienJus/kairos/internal/daemonobs"
)

var errDaemonReportTooLarge = errors.New("daemon report exceeds request limit")

// Telemetry is a bounded, process-local queue. Sending it is never part of a
// Claim or Dispatch state transition.
type Telemetry struct {
	mu       sync.Mutex
	next     int64
	events   []daemonobs.Event
	dropped  uint64
	started  time.Time
	instance string
}

func NewTelemetry() (*Telemetry, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	return &Telemetry{instance: id, started: time.Now().UTC(), events: []daemonobs.Event{}}, nil
}

func newUUID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[:4], data[4:6], data[6:8], data[8:10], data[10:]), nil
}

func (t *Telemetry) ID() string           { return t.instance }
func (t *Telemetry) StartedAt() time.Time { return t.started }

func (t *Telemetry) Emit(event daemonobs.Event) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	event.Sequence = t.next
	event.OccurredAt = time.Now().UTC().Truncate(time.Microsecond)
	event.ReceivedAt = nil
	if len(t.events) == 1000 {
		t.events = t.events[1:]
		t.dropped++
	}
	t.events = append(t.events, event)
}

func (t *Telemetry) Pending() ([]daemonobs.Event, uint64) {
	if t == nil {
		return []daemonobs.Event{}, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	count := min(50, len(t.events))
	result := append([]daemonobs.Event{}, t.events[:count]...)
	return result, t.dropped
}

func (t *Telemetry) AckThrough(sequence int64) {
	if t == nil || sequence < 1 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	acknowledged := 0
	for acknowledged < len(t.events) && t.events[acknowledged].Sequence <= sequence {
		acknowledged++
	}
	t.events = append([]daemonobs.Event{}, t.events[acknowledged:]...)
}

func eventFor(kind string, candidate Candidate, claimID string) daemonobs.Event {
	event := daemonobs.Event{Kind: kind}
	if candidate.Kind != "" {
		value := string(candidate.Kind)
		event.CandidateKind = &value
	}
	if candidate.WorkItemID != "" {
		value := string(candidate.WorkItemID)
		event.WorkItemID = &value
	}
	if candidate.TaskID != "" {
		value := string(candidate.TaskID)
		event.TaskID = &value
	}
	if claimID != "" {
		event.ClaimID = &claimID
	}
	return event
}

// TelemetrySnapshot never exposes credentials, workspaces, or Adapter RunRefs.
func (s *Scheduler) TelemetrySnapshot() daemonobs.Report {
	type runSnapshot struct {
		id      string
		started time.Time
		value   Snapshot
	}
	s.mu.Lock()
	paused := s.stats.Paused
	observed := s.probeObserved
	lifecycle := "running"
	if s.stopping {
		lifecycle = "stopping"
	}
	runs := make([]runSnapshot, 0, len(s.active))
	for _, run := range s.active {
		snapshot := run.dispatch.Snapshot()
		if !snapshot.Terminal() {
			runs = append(runs, runSnapshot{id: run.id, started: run.started, value: snapshot})
		}
	}
	s.mu.Unlock()
	sort.Slice(runs, func(i, j int) bool { return runs[i].started.Before(runs[j].started) })
	health := "unknown"
	if paused {
		health = "paused"
	} else if observed {
		health = "healthy"
	}
	report := daemonobs.Report{Lifecycle: lifecycle, Health: health, ActiveCount: len(runs),
		ActiveDispatches: []daemonobs.Dispatch{}, Events: []daemonobs.Event{}}
	for _, run := range runs {
		if len(report.ActiveDispatches) == 100 {
			report.OmittedActiveCount++
			continue
		}
		snapshot := run.value
		claimStatus := "not_attempted"
		if snapshot.UncertainClaim {
			claimStatus = "uncertain"
		} else if snapshot.ClaimID != "" {
			claimStatus = "active"
			if snapshot.ClaimEnded {
				claimStatus = "ended"
			}
		}
		dispatch := daemonobs.Dispatch{ID: run.id, CandidateKind: string(snapshot.Candidate.Kind),
			WorkItemID: string(snapshot.Candidate.WorkItemID), ClaimStatus: claimStatus,
			State: string(snapshot.State), Attempts: snapshot.Attempts, StartedAt: run.started.UTC()}
		if snapshot.Candidate.TaskID != "" {
			value := string(snapshot.Candidate.TaskID)
			dispatch.TaskID = &value
		}
		if snapshot.ClaimID != "" {
			value := snapshot.ClaimID
			dispatch.ClaimID = &value
		}
		report.ActiveDispatches = append(report.ActiveDispatches, dispatch)
	}
	return report
}

type Reporter struct {
	client     *HTTPClient
	telemetry  *Telemetry
	register   daemonobs.Registration
	mu         sync.Mutex
	registered bool
	revision   int64
}

func NewReporter(client *HTTPClient, telemetry *Telemetry, name, version, adapter string, slots int, tags []string) *Reporter {
	return &Reporter{client: client, telemetry: telemetry, register: daemonobs.Registration{
		ID: telemetry.ID(), Name: name, ProcessStartedAt: telemetry.StartedAt(), DaemonVersion: version,
		Adapter: adapter, Slots: slots, Tags: append([]string{}, tags...),
	}}
}

// Run retries registration and reports until cancelled. Errors are deliberately
// not returned to the scheduler; the next tick attempts recovery.
func (r *Reporter) Run(ctx context.Context, scheduler *Scheduler) {
	next := time.Now()
	failures := 0
	lastCategory := ""
	lastAttempt := time.Time{}
	for {
		timer := time.NewTimer(max(time.Until(next), 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := r.Flush(call, scheduler)
		cancel()
		lastAttempt = time.Now()
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			if failures > 0 {
				scheduler.options.Logger.Info("daemon_report_recovered", "after_failures", failures, "previous_category", lastCategory)
			}
			failures = 0
			lastCategory = ""
			next = lastAttempt.Add(15 * time.Second)
		} else {
			previousFailures := failures
			failures++
			backoffStep := min(failures, 6)
			backoff := min(time.Duration(1<<backoffStep)*time.Second, 60*time.Second)
			delay := backoff + time.Duration(mathrand.Int63n(int64(backoff/4)+1))
			next = lastAttempt.Add(delay)
			category := ReportErrorCategory(err)
			if previousFailures == 0 || category != lastCategory || failures == 6 {
				scheduler.options.Logger.Warn("daemon_report_failed", "category", category, "consecutive_failures", failures, "retry_after", delay)
			}
			lastCategory = category
		}
	}
}

// ReportErrorCategory returns a bounded label safe for local logs.
func ReportErrorCategory(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, errDaemonReportTooLarge) {
		return "report_too_large"
	}
	var api *APIError
	if errors.As(err, &api) {
		switch {
		case api.Status == 401 || api.Status == 403:
			return "authentication"
		case api.Status == 404:
			return "instance_not_found"
		case api.Status == 408 || api.Status == 429 || api.Status >= 500:
			return "core_unavailable"
		default:
			return "request_rejected"
		}
	}
	return "transport"
}

func (r *Reporter) Flush(ctx context.Context, scheduler *Scheduler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flush(ctx, scheduler, "")
}

func (r *Reporter) FlushStopped(ctx context.Context, scheduler *Scheduler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flush(ctx, scheduler, "stopped")
}

func (r *Reporter) flush(ctx context.Context, scheduler *Scheduler, lifecycleOverride string) error {
	if !r.registered {
		if err := r.client.RegisterDaemon(ctx, r.register); err != nil {
			return err
		}
		r.registered = true
	}
	r.revision++
	report := scheduler.TelemetrySnapshot()
	report.Revision = r.revision
	if lifecycleOverride != "" {
		report.Lifecycle = lifecycleOverride
	}
	report.Events, report.DroppedEvents = r.telemetry.Pending()
	for {
		encoded, err := json.Marshal(report)
		if err != nil {
			return err
		}
		if len(encoded) <= 64<<10 {
			break
		}
		if len(report.Events) > 0 {
			report.Events = report.Events[:len(report.Events)-1]
			continue
		}
		if len(report.ActiveDispatches) > 0 {
			report.ActiveDispatches = report.ActiveDispatches[:len(report.ActiveDispatches)-1]
			report.OmittedActiveCount++
			continue
		}
		return errDaemonReportTooLarge
	}
	if err := r.client.ReportDaemon(ctx, r.register.ID, report); err != nil {
		if statusIs(err, 404) {
			r.registered = false
		}
		return err
	}
	if len(report.Events) > 0 {
		r.telemetry.AckThrough(report.Events[len(report.Events)-1].Sequence)
	}
	return nil
}
