package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSchedulerUnclaimedWorkspaceCleanup(t *testing.T) {
	for _, scenario := range []string{"immediate_rejection", "unsent_rejection", "unknown_rejection", "unsent_cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			core, adapter, options := schedulerFixture(t)
			rejection := &ClaimAttemptError{State: ClaimRejected, Err: &APIError{Status: 409, Code: "conflict"}}
			core.claimError = rejection
			if scenario == "unsent_rejection" || scenario == "unsent_cancellation" {
				core.claimError = &ClaimAttemptError{State: ClaimNotSent, Err: errors.New("preflight unavailable")}
			} else if scenario == "unknown_rejection" {
				core.claimError = errors.New("unknown Claim response")
			}
			starts := 0
			adapter.start = func(context.Context, StartRequest) (RunRef, error) {
				starts++
				return RunRef{ID: "unexpected"}, nil
			}
			s, err := NewScheduler(core, adapter, options)
			if err != nil {
				t.Fatal(err)
			}
			var pending *scheduledRun
			s.admit(context.Background(), func(run *scheduledRun) { pending = run })
			if scenario != "immediate_rejection" {
				if pending == nil || pending.dispatch.Snapshot().ClaimID != "" {
					t.Fatal("expected a retained acquisition without a confirmed Claim")
				}
				workspace := pending.dispatch.options.Workspace
				if _, err := os.Stat(workspace); err != nil {
					t.Fatal(err)
				}
				if scenario == "unknown_rejection" {
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					result, err := pending.dispatch.Run(ctx)
					if !errors.Is(err, context.Canceled) || result.Terminal() {
						t.Fatalf("unknown acquisition must remain unresolved: %+v %v", result, err)
					}
					s.finish(pending)
					if _, err := os.Stat(workspace); err != nil || s.Stats().Active != 1 {
						t.Fatalf("unresolved workspace or slot was removed: %v %+v", err, s.Stats())
					}
				}
				core.claimError = rejection
				if scenario == "unsent_cancellation" {
					pending.dispatch.RequestStop(StopRequested)
				}
				result, err := pending.dispatch.Run(context.Background())
				if scenario == "unsent_cancellation" {
					if err != nil || result.StopReason != StopRequested {
						t.Fatalf("unsent cancellation: %+v %v", result, err)
					}
				} else if !statusIs(err, 409) {
					t.Fatalf("expected definitive Claim rejection: %v", err)
				}
				if !result.Terminal() || result.ClaimID != "" {
					t.Fatalf("acquisition did not end without a Claim: %+v", result)
				}
				s.finish(pending)
			} else if pending != nil {
				t.Fatal("rejected acquisition launched a worker")
			}
			entries, err := os.ReadDir(options.WorkspaceRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("ended unclaimed acquisition left a workspace: %v %v", entries, err)
			}
			if stats := s.Stats(); stats.Active != 0 || stats.Claims != 0 || stats.Finished != 1 || starts != 0 {
				t.Fatalf("cleanup changed execution responsibility: %+v starts=%d", stats, starts)
			}
			for _, op := range core.claimOperations {
				if op != core.claimOperations[0] {
					t.Fatal("acquisition retry replaced the operation ID")
				}
			}
		})
	}
}

func TestSchedulerWorkspaceCleanupPreservesClaimedOrNonemptyDirectories(t *testing.T) {
	for _, scenario := range []string{"claimed_empty", "unclaimed_nonempty"} {
		t.Run(scenario, func(t *testing.T) {
			core, adapter, options := schedulerFixture(t)
			adapter.observe = func(context.Context, RunRef) (RunObservation, error) {
				return RunObservation{State: OutcomeReady, Outcome: &HarnessOutcome{Task: &TaskOutcome{Kind: Abandoned}}}, nil
			}
			if scenario == "unclaimed_nonempty" {
				core.claimError = &ClaimAttemptError{State: ClaimNotSent, Err: errors.New("preflight unavailable")}
			}
			s, err := NewScheduler(core, adapter, options)
			if err != nil {
				t.Fatal(err)
			}
			var pending *scheduledRun
			s.admit(context.Background(), func(run *scheduledRun) { pending = run })
			if pending == nil {
				t.Fatal("expected pending Dispatch")
			}
			workspace := pending.dispatch.options.Workspace
			diagnostic := filepath.Join(workspace, "diagnostic.txt")
			if scenario == "unclaimed_nonempty" {
				if err := os.WriteFile(diagnostic, []byte("keep for inspection"), 0600); err != nil {
					t.Fatal(err)
				}
				core.claimError = &ClaimAttemptError{State: ClaimRejected, Err: &APIError{Status: 409, Code: "conflict"}}
			}
			result, err := pending.dispatch.Run(context.Background())
			if scenario == "claimed_empty" {
				if err != nil || !result.ClaimEnded || result.ClaimID == "" || result.Outcome != Abandoned {
					t.Fatalf("claimed completion: %+v %v", result, err)
				}
			} else if !statusIs(err, 409) || result.ClaimID != "" {
				t.Fatalf("unclaimed rejection: %+v %v", result, err)
			}
			if !result.Terminal() {
				t.Fatalf("expected terminal Dispatch: %+v", result)
			}
			s.finish(pending)
			if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
				t.Fatalf("inspection workspace was removed: %v", err)
			}
			if scenario == "unclaimed_nonempty" {
				data, err := os.ReadFile(diagnostic)
				if err != nil || string(data) != "keep for inspection" {
					t.Fatalf("diagnostic content was modified: %q %v", data, err)
				}
			}
		})
	}
}
