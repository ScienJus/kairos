package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/daemonobs"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
)

func TestDaemonObservationHTTPContract(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "daemon.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	service, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{}, httpapi.Options{DaemonStore: repo})
	if err != nil {
		t.Fatal(err)
	}
	const id = "4d383a89-154a-4794-bac3-e61cb8b3a6e4"
	call := func(method, path, kind, actor string, body any, want int) map[string]any {
		t.Helper()
		var payload []byte
		if body != nil {
			payload, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		request := httptest.NewRequest(method, path, bytes.NewReader(payload))
		request.Header.Set(identity.HeaderActorID, actor)
		request.Header.Set(identity.HeaderActorKind, kind)
		if kind == string(domain.ActorAgent) {
			request.Header.Set(identity.HeaderActorRole, "backend")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s %s as %s: status %d, want %d: %s", method, path, kind, response.Code, want, response.Body.String())
		}
		if response.Code == 204 {
			if response.Body.Len() != 0 {
				t.Fatalf("%s %s returned a body with 204: %q", method, path, response.Body.String())
			}
			return nil
		}
		var value map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	list := call("GET", "/api/v1/daemon-instances", "human", "operator", nil, 200)
	if data, ok := list["data"].([]any); !ok || len(data) != 0 {
		t.Fatalf("empty daemon list = %v", list["data"])
	}
	registration := daemonobs.Registration{ID: id, Name: "  laptop  ", ProcessStartedAt: time.Now().UTC().Add(-time.Minute), DaemonVersion: "dev", Adapter: "codex", Slots: 2, Tags: []string{}}
	call("POST", "/api/v1/daemon-instances", "human", "operator", registration, 403)
	created := call("POST", "/api/v1/daemon-instances", "agent", "agent-a", registration, 201)["data"].(map[string]any)
	if created["name"] != "laptop" {
		t.Fatalf("normalized Daemon name = %v", created["name"])
	}
	call("POST", "/api/v1/daemon-instances", "agent", "agent-a", registration, 200)
	call("POST", "/api/v1/daemon-instances", "agent", "agent-b", registration, 409)
	invalidReport := daemonobs.Report{Revision: 1, Lifecycle: "running", Health: "healthy", ActiveCount: 1, ActiveDispatches: []daemonobs.Dispatch{{
		ID: "b357a99e-1b52-419e-a8d4-184211cb7a6f", CandidateKind: "empty_blackboard", WorkItemID: "work-a",
		ClaimStatus: "active", State: "running", Attempts: 1, StartedAt: time.Now().UTC(),
	}}}
	call("POST", "/api/v1/daemon-instances/"+id+"/reports", "agent", "agent-a", invalidReport, 400)
	missingClaim := "missing-claim"
	candidateKind := "empty_blackboard"
	dispatchID, workItemID := "dispatch-a", "work-a"
	report := daemonobs.Report{Revision: 1, Lifecycle: "running", Health: "healthy", ActiveDispatches: []daemonobs.Dispatch{}, Events: []daemonobs.Event{{
		Sequence: 1, Kind: "claim_acquired", CandidateKind: &candidateKind, OccurredAt: time.Now().UTC(),
		DispatchID: &dispatchID, WorkItemID: &workItemID, ClaimID: &missingClaim,
	}}}
	call("POST", "/api/v1/daemon-instances/"+id+"/reports", "agent", "agent-b", report, 403)
	call("POST", "/api/v1/daemon-instances/"+id+"/reports", "agent", "agent-a", report, 204)
	call("POST", "/api/v1/daemon-instances/"+id+"/reports", "agent", "agent-a", report, 204)
	read := call("GET", "/api/v1/daemon-instances/"+id, "human", "operator", nil, 200)["data"].(map[string]any)
	if read["health"] != "healthy" || read["connectivity"] != "reporting" || read["last_revision"] != float64(1) {
		t.Fatalf("daemon detail = %v", read)
	}
	if active, ok := read["active_dispatches"].([]any); !ok || len(active) != 0 {
		t.Fatalf("active dispatches = %v", read["active_dispatches"])
	}
	events := call("GET", "/api/v1/daemon-instances/"+id+"/events", "human", "operator", nil, 200)["data"].([]any)
	if len(events) != 1 {
		t.Fatalf("duplicate event persisted: %v", events)
	}
	if events[0].(map[string]any)["candidate_kind"] != candidateKind {
		t.Fatalf("event candidate kind = %v", events[0])
	}
	call("GET", "/api/v1/daemon-instances", "agent", "agent-a", nil, 403)
	call("GET", "/api/v1/daemon-instances/"+id+"/events", "agent", "agent-a", nil, 403)
	report.Revision = 2
	report.Events[0].Kind = "candidate_quarantined"
	call("POST", "/api/v1/daemon-instances/"+id+"/reports", "agent", "agent-a", report, 204)
	events = call("GET", "/api/v1/daemon-instances/"+id+"/events", "human", "operator", nil, 200)["data"].([]any)
	if len(events) != 1 || events[0].(map[string]any)["kind"] != "claim_acquired" {
		t.Fatalf("duplicate sequence replaced persisted event: %v", events)
	}
}
