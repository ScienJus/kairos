// Package daemonobs keeps best-effort Daemon runtime observations separate from
// durable work coordination.
package daemonobs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	ErrInvalid   = errors.New("invalid daemon report")
	ErrForbidden = errors.New("daemon report forbidden")
	ErrNotFound  = errors.New("daemon instance not found")
	ErrConflict  = errors.New("daemon report conflict")
)

const StaleAfter = 45 * time.Second
const MaxPageLimit = 200

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Registration struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	ProcessStartedAt time.Time `json:"process_started_at"`
	DaemonVersion    string    `json:"daemon_version"`
	Adapter          string    `json:"adapter"`
	Slots            int       `json:"slots"`
	Tags             []string  `json:"tags"`
}

type Dispatch struct {
	ID            string    `json:"id"`
	CandidateKind string    `json:"candidate_kind"`
	WorkItemID    string    `json:"work_item_id"`
	TaskID        *string   `json:"task_id"`
	ClaimID       *string   `json:"claim_id"`
	ClaimStatus   string    `json:"claim_status"`
	State         string    `json:"state"`
	Attempts      int       `json:"attempts"`
	StartedAt     time.Time `json:"started_at"`
}

type EventDetails struct {
	Reason     string `json:"reason,omitempty"`
	State      string `json:"state,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Applied    *bool  `json:"applied,omitempty"`
	Attempts   *int   `json:"attempts,omitempty"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
}

type Event struct {
	Sequence      int64        `json:"sequence"`
	Kind          string       `json:"kind"`
	CandidateKind *string      `json:"candidate_kind"`
	OccurredAt    time.Time    `json:"occurred_at"`
	ReceivedAt    *time.Time   `json:"received_at,omitempty"`
	DispatchID    *string      `json:"dispatch_id"`
	WorkItemID    *string      `json:"work_item_id"`
	TaskID        *string      `json:"task_id"`
	ClaimID       *string      `json:"claim_id"`
	Details       EventDetails `json:"details"`
}

type Report struct {
	Revision           int64      `json:"revision"`
	Lifecycle          string     `json:"lifecycle"`
	Health             string     `json:"health"`
	ActiveCount        int        `json:"active_count"`
	ActiveDispatches   []Dispatch `json:"active_dispatches"`
	OmittedActiveCount int        `json:"omitted_active_count"`
	DroppedEvents      uint64     `json:"dropped_events"`
	Events             []Event    `json:"events"`
}

type Instance struct {
	Registration
	AgentID            string     `json:"agent_id"`
	RegisteredAt       time.Time  `json:"registered_at"`
	LastReportAt       time.Time  `json:"last_report_at"`
	StoppedAt          *time.Time `json:"stopped_at"`
	LastRevision       int64      `json:"last_revision"`
	Lifecycle          string     `json:"lifecycle"`
	Health             string     `json:"health"`
	ActiveCount        int        `json:"active_count"`
	ActiveDispatches   []Dispatch `json:"active_dispatches"`
	OmittedActiveCount int        `json:"omitted_active_count"`
	DroppedEvents      uint64     `json:"dropped_events"`
	Connectivity       string     `json:"connectivity"`
	AsOf               time.Time  `json:"as_of"`
}

type ListFilter struct {
	AgentID        string
	IncludeHistory bool
	Limit          int
	Before         *InstanceCursor
	Since          time.Time
}

type InstanceCursor struct {
	LastReportAt time.Time `json:"last_report_at"`
	ID           string    `json:"id"`
}

type Store interface {
	RegisterDaemon(context.Context, Instance) (Instance, bool, error)
	SaveDaemonReport(context.Context, string, string, Report, time.Time) error
	GetDaemon(context.Context, string) (Instance, error)
	ListDaemons(context.Context, ListFilter) ([]Instance, error)
	ListDaemonEvents(context.Context, string, int, int64) ([]Event, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func New(store Store, now func() time.Time) *Service { return &Service{store: store, now: now} }

func (s *Service) Register(ctx context.Context, agentID string, value Registration) (Instance, bool, error) {
	if s == nil || s.store == nil {
		return Instance{}, false, errors.New("daemon observations are unavailable")
	}
	value.Name = strings.TrimSpace(value.Name)
	if err := value.Validate(); err != nil {
		return Instance{}, false, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	instance := Instance{Registration: value, AgentID: agentID, RegisteredAt: now, LastReportAt: now,
		Lifecycle: "running", Health: "unknown", ActiveDispatches: []Dispatch{}, Connectivity: "reporting", AsOf: now}
	result, created, err := s.store.RegisterDaemon(ctx, instance)
	if err != nil {
		return Instance{}, false, err
	}
	return withConnectivity(result, now), created, nil
}

func (s *Service) Report(ctx context.Context, agentID, id string, report Report) error {
	if s == nil || s.store == nil {
		return errors.New("daemon observations are unavailable")
	}
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("%w: invalid instance id", ErrInvalid)
	}
	if err := report.Validate(); err != nil {
		return err
	}
	return s.store.SaveDaemonReport(ctx, agentID, id, report, s.now().UTC().Truncate(time.Microsecond))
}

func (s *Service) Get(ctx context.Context, id string) (Instance, error) {
	if !uuidPattern.MatchString(id) {
		return Instance{}, fmt.Errorf("%w: invalid instance id", ErrInvalid)
	}
	instance, err := s.store.GetDaemon(ctx, id)
	if err != nil {
		return Instance{}, err
	}
	return withConnectivity(instance, s.now()), nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]Instance, error) {
	if filter.Limit < 1 || filter.Limit > MaxPageLimit {
		return nil, fmt.Errorf("%w: limit must be 1..%d", ErrInvalid, MaxPageLimit)
	}
	now := s.now()
	if !filter.IncludeHistory {
		filter.Since = now.Add(-30 * 24 * time.Hour)
	}
	values, err := s.store.ListDaemons(ctx, filter)
	if err != nil {
		return nil, err
	}
	for i := range values {
		values[i] = withConnectivity(values[i], now)
	}
	if values == nil {
		values = []Instance{}
	}
	return values, nil
}

func (s *Service) Events(ctx context.Context, id string, limit int, before int64) ([]Event, error) {
	if !uuidPattern.MatchString(id) || limit < 1 || limit > MaxPageLimit || before < 0 {
		return nil, fmt.Errorf("%w: invalid event query", ErrInvalid)
	}
	values, err := s.store.ListDaemonEvents(ctx, id, limit, before)
	if values == nil {
		values = []Event{}
	}
	return values, err
}

func withConnectivity(value Instance, now time.Time) Instance {
	value.AsOf = now.UTC().Truncate(time.Microsecond)
	if value.Lifecycle == "stopped" {
		value.Connectivity = "stopped"
	} else if now.Sub(value.LastReportAt) > StaleAfter {
		value.Connectivity = "stale"
	} else {
		value.Connectivity = "reporting"
	}
	if value.Tags == nil {
		value.Tags = []string{}
	}
	if value.ActiveDispatches == nil {
		value.ActiveDispatches = []Dispatch{}
	}
	return value
}

func (v Registration) Validate() error {
	if !uuidPattern.MatchString(v.ID) || v.ProcessStartedAt.IsZero() || v.Slots < 1 ||
		len(v.Name) > 128 || len(v.DaemonVersion) > 64 || len(v.Adapter) > 32 ||
		strings.TrimSpace(v.DaemonVersion) == "" || strings.TrimSpace(v.Adapter) == "" || len(v.Tags) > 50 {
		return fmt.Errorf("%w: invalid registration", ErrInvalid)
	}
	for _, tag := range v.Tags {
		if tag == "" || len(tag) > 128 {
			return fmt.Errorf("%w: invalid tag", ErrInvalid)
		}
	}
	return nil
}

func (v Report) Validate() error {
	if v.Revision < 1 || !slices.Contains([]string{"running", "stopping", "stopped"}, v.Lifecycle) ||
		!slices.Contains([]string{"unknown", "healthy", "paused"}, v.Health) ||
		v.ActiveCount < 0 || v.OmittedActiveCount < 0 || v.ActiveCount != len(v.ActiveDispatches)+v.OmittedActiveCount ||
		len(v.ActiveDispatches) > 100 || len(v.Events) > 50 {
		return fmt.Errorf("%w: invalid report", ErrInvalid)
	}
	for _, d := range v.ActiveDispatches {
		claimIDRequired := d.ClaimStatus == "active" || d.ClaimStatus == "ended"
		claimIDValid := (!claimIDRequired && d.ClaimID == nil) ||
			(claimIDRequired && d.ClaimID != nil && strings.TrimSpace(*d.ClaimID) != "")
		taskIDRequired := d.CandidateKind == "task"
		taskIDValid := (!taskIDRequired && d.TaskID == nil) ||
			(taskIDRequired && d.TaskID != nil && strings.TrimSpace(*d.TaskID) != "")
		if !uuidPattern.MatchString(d.ID) || d.WorkItemID == "" || d.StartedAt.IsZero() || d.Attempts < 0 ||
			!slices.Contains([]string{"task", "empty_blackboard", "blackboard_completion", "work_item_acceptance"}, d.CandidateKind) ||
			!slices.Contains([]string{"not_attempted", "uncertain", "active", "ended"}, d.ClaimStatus) ||
			!slices.Contains([]string{"prepared", "claimed", "starting", "running", "finalizing", "stopping", "finished", "lost"}, d.State) ||
			!taskIDValid || !claimIDValid {
			return fmt.Errorf("%w: invalid active dispatch", ErrInvalid)
		}
	}
	for _, e := range v.Events {
		if e.Sequence < 1 || e.OccurredAt.IsZero() || !slices.Contains(eventKinds, e.Kind) ||
			e.ReceivedAt != nil || !validEvent(e) {
			return fmt.Errorf("%w: invalid event", ErrInvalid)
		}
	}
	return nil
}

var eventKinds = []string{"admission_paused", "admission_resumed", "claim_acquired", "harness_started", "harness_retry", "outcome_applied", "dispatch_ended", "candidate_quarantined", "shutdown_started", "daemon_stopped"}
var candidateKinds = []string{"task", "empty_blackboard", "blackboard_completion", "work_item_acceptance"}
var eventReasons = []string{"requested", "lease_lost", "authority_lost", "runtime_failure", "protocol_error", "core_rejected"}
var eventOutcomes = []string{"completed", "decomposed", "retryable_failure", "work_item_failure", "human_intervention_required", "candidate_declined", "create_task", "submit_completion", "accept_completion"}

func validEvent(event Event) bool {
	if event.CandidateKind != nil && !slices.Contains(candidateKinds, *event.CandidateKind) {
		return false
	}
	for _, value := range []*string{event.DispatchID, event.WorkItemID, event.TaskID, event.ClaimID} {
		if value != nil && strings.TrimSpace(*value) == "" {
			return false
		}
	}
	return event.Details.valid()
}

func (v EventDetails) valid() bool {
	return (v.Reason == "" || slices.Contains(eventReasons, v.Reason)) &&
		(v.State == "" || v.State == "finished" || v.State == "lost") &&
		(v.Outcome == "" || slices.Contains(eventOutcomes, v.Outcome)) &&
		(v.Attempts == nil || *v.Attempts >= 0) && (v.DurationMS == nil || *v.DurationMS >= 0) &&
		(v.Applied == nil || !*v.Applied || v.Outcome != "")
}
