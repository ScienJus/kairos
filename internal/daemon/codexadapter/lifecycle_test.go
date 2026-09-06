//go:build darwin || linux

package codexadapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/daemon"
	"github.com/ScienJus/kairos/internal/domain"
)

type endingCore struct {
	mu    sync.Mutex
	claim daemon.Claim
}

func (c *endingCore) Claim(ctx context.Context, _ daemon.Candidate, _ string, _ daemon.Secret, lease int64) (daemon.Claim, error) {
	if err := ctx.Err(); err != nil {
		return daemon.Claim{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.claim = daemon.Claim{ID: "claim", Executor: domain.ActorRef{Kind: domain.ActorAgent, ID: "executor"}, LeaseSeconds: lease, Active: true}
	return c.claim, nil
}
func (c *endingCore) Heartbeat(ctx context.Context, _ daemon.Candidate, _ string, _ int64) (daemon.Claim, error) {
	if err := ctx.Err(); err != nil {
		return daemon.Claim{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.claim, nil
}
func (c *endingCore) Inspect(ctx context.Context, _ daemon.Candidate, _ string) (daemon.ClaimStatus, error) {
	if err := ctx.Err(); err != nil {
		return daemon.ClaimStatus{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return daemon.ClaimStatus{Claim: c.claim}, nil
}
func (c *endingCore) Apply(context.Context, daemon.Candidate, string, string, daemon.HarnessOutcome) error {
	return errors.New("unexpected Apply")
}
func (c *endingCore) Release(ctx context.Context, _ daemon.Candidate, _ string, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.claim.Active = false
	c.claim.EndReason = reason
	c.claim.EndedAt = &now
	return nil
}

func TestDispatchForgetBeforeAdapterWait(t *testing.T) {
	a, r := fixture(t, "ignore_interrupt")
	options := daemon.DefaultOptions()
	options.MCPURL = r.MCPURL
	options.CoreURL = r.CoreURL
	options.Workspace = r.Workspace
	options.StopTimeout = 10 * time.Millisecond
	options.PollInterval = 10 * time.Millisecond
	d, err := daemon.NewDispatch(&endingCore{}, a, r.Candidate, options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := d.Run(ctx); done <- err }()
	var ref daemon.RunRef
	ready := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ref = d.Snapshot().RunRef
		if ref.ID != "" {
			if _, err := os.Stat(filepath.Join(ref.ID, "started")); err == nil {
				ready = true
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ready {
		t.Fatal("run did not start")
	}
	defer a.Stop(context.Background(), ref, daemon.StopRequested)
	started := time.Now()
	d.RequestStop(daemon.StopRequested)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	result := d.Snapshot()
	if !result.Terminal() || result.State != daemon.Lost || !result.ClaimEnded {
		t.Fatalf("dispatch did not reach confirmed-Claim lost terminal: %+v", result)
	}
	t.Logf("Dispatch terminal after %s", time.Since(started))
	a.mu.Lock()
	run := a.runs[ref.ID]
	a.mu.Unlock()
	if run == nil {
		t.Fatal("test must exercise Forget before Adapter completion")
	}
	run.mu.Lock()
	pending, ended := run.forget, run.done
	run.mu.Unlock()
	if !pending || ended {
		t.Fatal("early Forget was not retained for the live run")
	}
	// Idempotent early requests must not remove a live process from observation.
	a.Forget(ref)
	if obs, err := a.Observe(ctx, ref); err != nil || obs.State != daemon.RunRunning {
		t.Fatalf("live run forgotten: %+v %v", obs, err)
	}
	for {
		a.mu.Lock()
		retained := len(a.runs)
		a.mu.Unlock()
		if retained == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("completed run metadata was not reclaimed")
		case <-time.After(5 * time.Millisecond):
		}
	}
	run.mu.Lock()
	ended = run.done
	state := run.observation.State
	run.mu.Unlock()
	if !ended || state != daemon.RunLost {
		t.Fatalf("removed before lost completion: done=%v state=%s", ended, state)
	}
	if _, err := os.Stat(ref.ID); err != nil {
		t.Fatal("Forget removed workspace")
	}
	a.Forget(ref)
}

func TestConcurrentForgetAndCompletion(t *testing.T) {
	a, request := fixture(t, "success")
	ref, err := a.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background(), ref, daemon.StopRequested)
	var callers sync.WaitGroup
	for range 32 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			for range 32 {
				a.Forget(ref)
				// Observation can legitimately lose ownership to reclamation.
				_, _ = a.Observe(context.Background(), ref)
			}
		}()
	}
	callers.Wait()
	deadline := time.Now().Add(4 * time.Second)
	for {
		a.mu.Lock()
		retained := len(a.runs)
		a.mu.Unlock()
		if retained == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("concurrent Forget lost the reclamation request")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(ref.ID); err != nil {
		t.Fatal("Forget removed workspace")
	}
}
