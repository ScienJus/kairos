package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/daemonobs"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestEventForKeepsCandidateKind(t *testing.T) {
	event := eventFor("claim_acquired", Candidate{
		Kind: EmptyBlackboard, WorkItemID: domain.WorkItemID("work-a"),
	}, "claim-a")
	if event.CandidateKind == nil || *event.CandidateKind != string(EmptyBlackboard) {
		t.Fatalf("candidate kind = %v", event.CandidateKind)
	}
	if event.TaskID != nil {
		t.Fatalf("coordination event task id = %v", event.TaskID)
	}
}

func TestReporterKeepsEventsAcrossCoreFailure(t *testing.T) {
	telemetry, err := NewTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	telemetry.Emit(daemonobs.Event{Kind: "admission_paused"})
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing Identity Token")
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/daemon-instances" {
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"data":{}}`))
			return
		}
		calls++
		var report daemonobs.Report
		if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		if len(report.Events) != 1 || report.Events[0].Sequence != 1 {
			t.Errorf("unexpected report events: %+v", report.Events)
		}
		if calls == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"error":{"code":"unavailable","message":"offline"}}`))
			return
		}
		telemetry.Emit(daemonobs.Event{Kind: "admission_resumed"})
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, NewSecret("test-token"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	core, adapter, options := schedulerFixture(t)
	options.Telemetry = telemetry
	scheduler, err := NewScheduler(core, adapter, options)
	if err != nil {
		t.Fatal(err)
	}
	reporter := NewReporter(client, telemetry, "", "dev", "codex", options.Slots, []string{})
	if err := reporter.Flush(context.Background(), scheduler); err == nil {
		t.Fatal("first report should fail")
	}
	if pending, _ := telemetry.Pending(); len(pending) != 1 {
		t.Fatalf("lost event after failed batch: %+v", pending)
	}
	if err := reporter.Flush(context.Background(), scheduler); err != nil {
		t.Fatal(err)
	}
	if pending, _ := telemetry.Pending(); len(pending) != 1 || pending[0].Sequence != 2 {
		t.Fatalf("batch acknowledgement removed a concurrently queued event: %+v", pending)
	}
	if calls != 2 {
		t.Fatalf("report calls = %d", calls)
	}
}

func TestReporterFlushStoppedIsSingleBestEffortBatch(t *testing.T) {
	telemetry, err := NewTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	for range 60 {
		telemetry.Emit(daemonobs.Event{Kind: "admission_paused"})
	}
	telemetry.Emit(daemonobs.Event{Kind: "daemon_stopped"})
	var received daemonobs.Report
	reportCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/daemon-instances" {
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"data":{}}`))
			return
		}
		reportCalls++
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := received.Validate(); err != nil {
			t.Errorf("invalid report: %v", err)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, NewSecret("test-token"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	core, adapter, options := schedulerFixture(t)
	options.Telemetry = telemetry
	scheduler, err := NewScheduler(core, adapter, options)
	if err != nil {
		t.Fatal(err)
	}
	reporter := NewReporter(client, telemetry, "", "dev", "codex", options.Slots, nil)
	if err := reporter.FlushStopped(context.Background(), scheduler); err != nil {
		t.Fatal(err)
	}
	if reportCalls != 1 || received.Lifecycle != "stopped" || len(received.Events) != 50 {
		t.Fatalf("final report calls=%d report=%+v", reportCalls, received)
	}
	pending, _ := telemetry.Pending()
	if len(pending) != 11 || pending[0].Sequence != 51 {
		t.Fatalf("final report should leave later best-effort events queued: %+v", pending)
	}
}

func TestSchedulerTelemetryReportsStoppingAfterCancellation(t *testing.T) {
	telemetry, err := NewTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	core, adapter, options := schedulerFixture(t)
	options.Telemetry = telemetry
	scheduler, err := NewScheduler(core, adapter, options)
	if err != nil {
		t.Fatal(err)
	}
	if lifecycle := scheduler.TelemetrySnapshot().Lifecycle; lifecycle != "running" {
		t.Fatalf("initial lifecycle = %q", lifecycle)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scheduler.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if lifecycle := scheduler.TelemetrySnapshot().Lifecycle; lifecycle != "stopping" {
		t.Fatalf("shutdown lifecycle = %q", lifecycle)
	}
}

func TestSchedulerTelemetryOmitsTerminalDispatchWaitingForCleanup(t *testing.T) {
	core, adapter, options := schedulerFixture(t)
	scheduler, err := NewScheduler(core, adapter, options)
	if err != nil {
		t.Fatal(err)
	}
	terminal := taskCandidate()
	scheduler.active[terminal] = &scheduledRun{
		id: "4d383a89-154a-4794-bac3-e61cb8b3a6e4", dispatch: &Dispatch{candidate: terminal, state: Finished}, started: time.Now(),
	}
	active := Candidate{Kind: EmptyBlackboard, WorkItemID: "work-b", Mode: domain.CoordinationModeBlackboard}
	scheduler.active[active] = &scheduledRun{
		id: "b357a99e-1b52-419e-a8d4-184211cb7a6f", dispatch: &Dispatch{candidate: active, state: Running, claim: Claim{ID: "claim-b", Active: true}}, started: time.Now(),
	}
	scheduler.stats.Active = 2
	report := scheduler.TelemetrySnapshot()
	if report.ActiveCount != 1 || len(report.ActiveDispatches) != 1 || report.ActiveDispatches[0].ID != "b357a99e-1b52-419e-a8d4-184211cb7a6f" || report.OmittedActiveCount != 0 {
		t.Fatalf("terminal Dispatch reported as active: %+v", report)
	}
	report.Revision = 1
	if err := report.Validate(); err != nil {
		t.Fatalf("filtered snapshot is invalid: %v", err)
	}
}

type telemetryLogHandler struct{ records chan slog.Record }

func (h *telemetryLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *telemetryLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.records <- record.Clone()
	return nil
}
func (h *telemetryLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *telemetryLogHandler) WithGroup(string) slog.Handler      { return h }

func TestReporterLogsSafeFailureAndRecovery(t *testing.T) {
	telemetry, err := NewTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	reportCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/daemon-instances" {
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"data":{}}`))
			return
		}
		reportCalls++
		if reportCalls == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"error":{"code":"unavailable"}}`))
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, NewSecret("test-token"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	core, adapter, options := schedulerFixture(t)
	options.Telemetry = telemetry
	records := make(chan slog.Record, 2)
	options.Logger = slog.New(&telemetryLogHandler{records: records})
	scheduler, err := NewScheduler(core, adapter, options)
	if err != nil {
		t.Fatal(err)
	}
	reporter := NewReporter(client, telemetry, "", "dev", "codex", options.Slots, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); reporter.Run(ctx, scheduler) }()
	select {
	case record := <-records:
		if record.Message != "daemon_report_failed" {
			t.Fatalf("log message = %q", record.Message)
		}
		attributes := map[string]any{}
		record.Attrs(func(attribute slog.Attr) bool { attributes[attribute.Key] = attribute.Value.Any(); return true })
		if attributes["category"] != "core_unavailable" || attributes["consecutive_failures"] != int64(1) {
			t.Fatalf("log attributes = %#v", attributes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reporter did not log its failure")
	}
	select {
	case record := <-records:
		if record.Message != "daemon_report_recovered" {
			t.Fatalf("recovery log message = %q", record.Message)
		}
		attributes := map[string]any{}
		record.Attrs(func(attribute slog.Attr) bool { attributes[attribute.Key] = attribute.Value.Any(); return true })
		if attributes["after_failures"] != int64(1) || attributes["previous_category"] != "core_unavailable" {
			t.Fatalf("recovery log attributes = %#v", attributes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Reporter did not log its recovery")
	}
	cancel()
	<-done
}

func TestReportErrorCategory(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "timeout"},
		{errDaemonReportTooLarge, "report_too_large"},
		{&APIError{Status: http.StatusUnauthorized}, "authentication"},
		{&APIError{Status: http.StatusNotFound}, "instance_not_found"},
		{&APIError{Status: http.StatusTooManyRequests}, "core_unavailable"},
		{&APIError{Status: http.StatusBadRequest}, "request_rejected"},
	} {
		if got := ReportErrorCategory(test.err); got != test.want {
			t.Errorf("ReportErrorCategory(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}
