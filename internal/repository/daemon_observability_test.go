package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/daemonobs"
)

func TestDaemonReportRevisionAndConnectivity(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, t.TempDir()+"/daemon.db")
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	service := daemonobs.New(repo, func() time.Time { return now })
	const id = "4d383a89-154a-4794-bac3-e61cb8b3a6e4"
	registration := daemonobs.Registration{ID: id, Name: "  laptop  ", ProcessStartedAt: now, DaemonVersion: "dev", Adapter: "codex", Slots: 1, Tags: []string{}}
	registered, created, err := service.Register(ctx, "agent-a", registration)
	if err != nil || !created || registered.Name != "laptop" {
		t.Fatalf("register: created=%v err=%v", created, err)
	}
	corrupted := registered
	corrupted.AgentID = "agent-b"
	corrupted.LastRevision = 99
	corrupted.Lifecycle = "stopped"
	payload, err := json.Marshal(corrupted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, "UPDATE daemon_instances SET payload = ? WHERE id = ?", string(payload), id); err != nil {
		t.Fatal(err)
	}
	replayed, created, err := service.Register(ctx, "agent-a", registration)
	if err != nil || created || replayed.AgentID != "agent-a" || replayed.LastRevision != 0 || replayed.Lifecycle != "running" {
		t.Fatalf("registration did not use canonical columns: created=%v value=%+v err=%v", created, replayed, err)
	}
	now = now.Add(10 * time.Second)
	report := daemonobs.Report{Revision: 1, Lifecycle: "running", Health: "healthy", ActiveDispatches: []daemonobs.Dispatch{}, Events: []daemonobs.Event{{Sequence: 1, Kind: "admission_resumed", OccurredAt: now}}}
	if err := service.Report(ctx, "agent-a", id, report); err != nil {
		t.Fatal(err)
	}
	now = now.Add(46 * time.Second)
	if err := service.Report(ctx, "agent-a", id, report); err != nil {
		t.Fatal(err)
	}
	value, err := service.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if value.Connectivity != "stale" || value.LastRevision != 1 {
		t.Fatalf("old revision refreshed instance: %+v", value)
	}
	report.Revision = 2
	report.Lifecycle = "stopped"
	if err := service.Report(ctx, "agent-a", id, report); err != nil {
		t.Fatal(err)
	}
	value, err = service.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if value.Connectivity != "stopped" || value.StoppedAt == nil {
		t.Fatalf("stopped report: %+v", value)
	}
	replayed, created, err = service.Register(ctx, "agent-a", registration)
	if err != nil || created || replayed.Connectivity != "stopped" || !replayed.AsOf.Equal(now) {
		t.Fatalf("registration replay: created=%v value=%+v err=%v", created, replayed, err)
	}
	report.Revision = 3
	report.Lifecycle = "running"
	if err := service.Report(ctx, "agent-a", id, report); err != daemonobs.ErrConflict {
		t.Fatalf("restart stopped instance error = %v", err)
	}
	now = now.Add(31 * 24 * time.Hour)
	if err := repo.PruneDaemonObservations(ctx, now); err != nil {
		t.Fatal(err)
	}
	events, err := service.Events(ctx, id, 50, 0)
	if err != nil || len(events) != 0 {
		t.Fatalf("expired events = %+v, err = %v", events, err)
	}
	if _, err := service.Get(ctx, id); err != nil {
		t.Fatalf("instance removed with events: %v", err)
	}
	now = now.Add(60 * 24 * time.Hour)
	if err := repo.PruneDaemonObservations(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, id); err != daemonobs.ErrNotFound {
		t.Fatalf("expired instance error = %v", err)
	}
}
