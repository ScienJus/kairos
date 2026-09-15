package repository

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestWorkflowLongNodeLimitCommitsFailure(t *testing.T) {
	for _, action := range []string{"retry", "submit"} {
		t.Run(action, func(t *testing.T) {
			forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
				ctx := context.Background()
				service := repositoryTestService(t, repo)
				actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
				longID := domain.WorkflowTaskID(strings.Repeat("界", domain.MaxHistoryTextBytes/3+1))
				def := domain.WorkflowDefinition{
					DefinitionMetadata: domain.DefinitionMetadata{ID: "long-node", Version: 1, Name: "Long node", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime},
					Graph: domain.WorkflowGraph{StartTaskIDs: []domain.WorkflowTaskID{longID, "parallel"}, MaxTaskInstancesPerNode: 1,
						Relations: []domain.WorkflowRelationDefinition{{ID: "again", FromTaskID: longID, ToTaskID: longID}, {ID: "finish", FromTaskID: longID, ToTaskID: "done"}}},
				}
				for _, id := range []domain.WorkflowTaskID{longID, "parallel", "done"} {
					def.Graph.Tasks = append(def.Graph.Tasks, domain.WorkflowTaskDefinition{ID: id, Title: "Task", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone})
				}
				if err := def.Validate(); err != nil {
					t.Fatal(err)
				}
				if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
					t.Fatal(err)
				}
				work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: action, Goal: "Persist limit failures"})
				if err != nil {
					t.Fatal(err)
				}
				initial, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
				if err != nil {
					t.Fatal(err)
				}
				var source domain.Task
				var held domain.Claim
				for _, task := range initial.Tasks {
					claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
					if err != nil {
						t.Fatal(err)
					}
					if *task.WorkflowTaskID == longID {
						source, held = task, claim
					}
				}
				if action == "retry" {
					_, err = service.FailTask(ctx, application.FailTaskCommand{TaskID: source.ID, ClaimID: held.ID, Identity: actor, Action: domain.TaskFailureRetry, Reason: "Permission denied", RetryPrompt: "Repair permissions"})
				} else {
					_, err = service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: source.ID, ClaimID: held.ID, Identity: actor, Result: "Need another pass", Transition: &application.WorkflowTransitionCommand{ChoiceGroupID: "continue:again"}})
				}
				if err != nil {
					t.Fatalf("limit must commit the source operation: %v", err)
				}
				peer := repositoryTestService(t, openPeer(t))
				after, err := peer.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
				if err != nil {
					t.Fatal(err)
				}
				failure := after.WorkItem.Failure
				if after.WorkItem.Status != domain.WorkItemStatusFailed || failure == nil || failure.Kind != domain.FailureWorkflowTaskInstanceLimit || failure.WorkflowTaskID != longID || failure.TaskInstances != 1 || failure.Limit != 1 {
					t.Fatal("missing persisted failure or complete node identity")
				}
				if len(failure.Message) > domain.MaxHistoryTextBytes || !utf8.ValidString(failure.Message) || !strings.Contains(failure.Message, "reached max_task_instances_per_node (1); cannot create task instance 2") {
					t.Fatal("failure message lost its cause or exceeds the byte budget")
				}
				if len(after.Tasks) != 2 || len(after.ActiveClaims) != 0 {
					t.Fatal("limit created an extra Task or retained active Claims")
				}
				execution, err := peer.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{TaskID: source.ID, Identity: actor})
				if err != nil {
					t.Fatal(err)
				}
				if action == "retry" {
					if execution.Task.Status != domain.TaskStatusFailed || len(execution.Task.Failures) != 1 || execution.Task.Failures[0].RetryPrompt != "Repair permissions" {
						t.Fatal("source failure was rolled back")
					}
				} else if execution.Task.Status != domain.TaskStatusCompleted || len(execution.Task.Submissions) != 1 || len(execution.Task.TransitionDecisions) != 1 || execution.Task.TransitionDecisions[0].AppliedAt == nil {
					t.Fatal("source submission or decision was rolled back")
				}
				var event domain.WorkItemEvent
				rows, err := repo.db.QueryContext(ctx, rebind(repo.dialect, "SELECT payload FROM work_item_events WHERE work_item_id = ? ORDER BY sequence DESC"), work.ID)
				if err != nil {
					t.Fatal(err)
				}
				for rows.Next() {
					var payload string
					if err := rows.Scan(&payload); err != nil {
						rows.Close()
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(payload), &event); err != nil {
						rows.Close()
						t.Fatal(err)
					}
					if event.Type == domain.WorkItemEventWorkItemFailed {
						break
					}
				}
				rowsErr := rows.Err()
				rows.Close()
				if rowsErr != nil {
					t.Fatal(rowsErr)
				}
				if event.Type != domain.WorkItemEventWorkItemFailed || event.Message != failure.Message {
					t.Fatal("failure event differs from the persisted snapshot")
				}
				if _, err := peer.HeartbeatClaim(ctx, application.HeartbeatClaimCommand{TaskID: source.ID, ClaimID: held.ID, Identity: actor}); !errors.Is(err, application.ErrConflict) {
					t.Fatalf("terminal Claim must remain fenced: %v", err)
				}
				if _, err := peer.ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: after.WorkItem.Version, MaxTaskInstancesPerNode: 2}); err != nil {
					t.Fatalf("long-node failure must remain recoverable: %v", err)
				}
			})
		})
	}
}

func TestWorkflowPerNodeTaskInstanceLimitPersistsAcrossConnections(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		name := "exit-at-boundary"
		if overflow {
			name = "reject-next-instance"
		}
		t.Run(name, func(t *testing.T) {
			forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
				ctx := context.Background()
				actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
				definition := domain.WorkflowDefinition{
					DefinitionMetadata: domain.DefinitionMetadata{ID: "per-node", Version: 1, Name: "Per-node limit", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime},
					Graph: domain.WorkflowGraph{
						StartTaskIDs: []domain.WorkflowTaskID{"plan", "parallel"}, MaxTaskInstancesPerNode: 2,
						Relations: []domain.WorkflowRelationDefinition{
							{ID: "plan-a", FromTaskID: "plan", ToTaskID: "a"},
							{ID: "a-b", FromTaskID: "a", ToTaskID: "b"},
							{ID: "b-a", FromTaskID: "b", ToTaskID: "a"},
							{ID: "b-done", FromTaskID: "b", ToTaskID: "done"},
						},
					},
				}
				for _, id := range []domain.WorkflowTaskID{"plan", "parallel", "a", "b", "done"} {
					definition.Graph.Tasks = append(definition.Graph.Tasks, domain.WorkflowTaskDefinition{ID: id, Title: string(id), Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone})
				}
				if err := definition.Validate(); err != nil {
					t.Fatal(err)
				}
				if err := repo.CreateWorkflowDefinition(ctx, definition); err != nil {
					t.Fatal(err)
				}
				// The second WorkItem uses the same node IDs without inheriting the
				// first WorkItem's counters, even when the first exhausted its limit.
				for workIndex := 0; workIndex < 2; workIndex++ {
					service := repositoryTestService(t, repo)
					work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: definition.Binding(), Identity: actor, Title: name, Goal: "Count each node independently"})
					if err != nil {
						t.Fatal(err)
					}
					pending := func(id domain.WorkflowTaskID) domain.Task {
						t.Helper()
						var found domain.Task
						if err := repo.View(ctx, func(store application.ReadStore) error {
							tasks, err := store.ListTasks(work.ID)
							for _, task := range tasks {
								if task.WorkflowTaskID != nil && *task.WorkflowTaskID == id && task.Status == domain.TaskStatusPending {
									found = task
								}
							}
							return err
						}); err != nil || found.ID == "" {
							t.Fatalf("pending %s: %v", id, err)
						}
						return found
					}
					claim := func(task domain.Task) domain.Claim {
						t.Helper()
						c, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
						if err != nil {
							t.Fatal(err)
						}
						return c
					}
					submit := func(task domain.Task, c domain.Claim, group domain.WorkflowChoiceGroupID) {
						t.Helper()
						command := application.SubmitTaskCommand{TaskID: task.ID, ClaimID: c.ID, Identity: actor, Result: "Verified"}
						if group != "" {
							command.Transition = &application.WorkflowTransitionCommand{ChoiceGroupID: group}
						}
						if _, err := service.SubmitTask(ctx, command); err != nil {
							t.Fatal(err)
						}
					}
					parallel := pending("parallel")
					parallelClaim := claim(parallel)
					plan := pending("plan")
					submit(plan, claim(plan), "exit:plan")
					for round := 1; round <= 2; round++ {
						a := pending("a")
						c := claim(a)
						// Releasing/reclaiming the same Task must not consume another
						// node task instance, including at the exact boundary.
						if err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: a.ID, ClaimID: c.ID, Identity: actor}); err != nil {
							t.Fatal(err)
						}
						submit(a, claim(a), "continue:a-b")
						b := pending("b")
						choice := domain.WorkflowChoiceGroupID("continue:b-a")
						if round == 2 && !overflow {
							choice = "exit:b"
						}
						if round == 2 {
							// Read persisted activations through another connection.
							service = repositoryTestService(t, openPeer(t))
						}
						submit(b, claim(b), choice)
					}
					if !overflow {
						done := pending("done")
						submit(done, claim(done), "")
						submit(parallel, parallelClaim, "")
					}
					if err := repo.View(ctx, func(store application.ReadStore) error {
						persisted, err := store.GetWorkItem(work.ID)
						if err != nil {
							return err
						}
						want := domain.WorkItemStatusCompleted
						wantCount := 7
						if overflow {
							want, wantCount = domain.WorkItemStatusFailed, 6
						}
						tasks, err := store.ListTasks(work.ID)
						if persisted.Status != want || len(tasks) != wantCount {
							t.Fatalf("work status=%s tasks=%d, want %s/%d", persisted.Status, len(tasks), want, wantCount)
						}
						for _, task := range tasks {
							if task.ActiveClaimID != nil {
								t.Fatal("failed work retains active Claim")
							}
							if task.WorkflowTaskID != nil && *task.WorkflowTaskID == "b" && (task.Status != domain.TaskStatusCompleted || len(task.Submissions) != 1 || len(task.TransitionDecisions) != 1 || task.TransitionDecisions[0].AppliedAt == nil) {
								t.Fatal("source submission and decision were not committed")
							}
						}
						if err != nil {
							return err
						}
						claims, err := store.ListClaimsByWorkItem(work.ID)
						for _, claim := range claims {
							if claim.Active() {
								t.Fatal("failed work retains an active persisted Claim")
							}
							if overflow && claim.ID == parallelClaim.ID && claim.EndReason != domain.ClaimEndRevoked {
								t.Fatalf("parallel Claim end reason: %s", claim.EndReason)
							}
						}
						return err
					}); err != nil {
						t.Fatal(err)
					}
					if overflow {
						rows, err := repo.db.QueryContext(ctx, rebind(repo.dialect, `SELECT payload FROM work_item_events WHERE work_item_id = ? ORDER BY sequence`), work.ID)
						if err != nil {
							t.Fatal(err)
						}
						found := false
						for rows.Next() {
							var data []byte
							if err := rows.Scan(&data); err != nil {
								t.Fatal(err)
							}
							var event domain.WorkItemEvent
							if err := json.Unmarshal(data, &event); err != nil {
								t.Fatal(err)
							}
							if event.Type == domain.WorkItemEventWorkItemFailed && strings.Contains(event.Message, `workflow node "a" reached max_task_instances_per_node (2); cannot create task instance 3`) {
								found = true
							}
						}
						if err := rows.Err(); err != nil {
							t.Fatal(err)
						}
						rows.Close()
						if !found {
							t.Fatal("missing persisted node-specific failure reason")
						}
						if _, err := service.HeartbeatClaim(ctx, application.HeartbeatClaimCommand{TaskID: parallel.ID, ClaimID: parallelClaim.ID, Identity: actor}); !errors.Is(err, application.ErrConflict) {
							t.Fatalf("terminal Claim heartbeat: %v", err)
						}
						failedContext, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
						if err != nil {
							t.Fatal(err)
						}
						failed := failedContext.WorkItem
						if failed.Failure == nil || failed.Failure.Kind != domain.FailureWorkflowTaskInstanceLimit || failed.Failure.WorkflowTaskID != "a" || failed.Failure.TaskInstances != 2 || failed.Failure.Limit != 2 {
							t.Fatalf("missing structured failure: %+v", failed.Failure)
						}
						command := application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: failed.Version, MaxTaskInstancesPerNode: 3}
						agentCommand := command
						agentCommand.Identity = application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "developer"}
						if _, err := service.ContinueWorkflow(ctx, agentCommand); !errors.Is(err, application.ErrForbidden) {
							t.Fatalf("agent continue: %v", err)
						}
						for _, limit := range []int{0, 1, 2, 501} {
							bad := command
							bad.MaxTaskInstancesPerNode = limit
							if _, err := service.ContinueWorkflow(ctx, bad); !errors.Is(err, application.ErrInvalidCommand) {
								t.Fatalf("continue limit %d: %v", limit, err)
							}
						}
						// A stale failure snapshot must not bypass the actual runtime
						// counter. Prove rollback after the transaction starts replay.
						if err := repo.Update(ctx, func(store application.WriteStore) error {
							w, err := store.GetWorkItem(work.ID)
							if err != nil {
								return err
							}
							w.Failure = &domain.WorkItemFailure{Kind: domain.FailureWorkflowTaskInstanceLimit, Message: "older snapshot", WorkflowTaskID: "a", TaskInstances: 1, Limit: 1}
							w.Version++
							command.Version = w.Version
							return store.SaveWorkItem(w)
						}); err != nil {
							t.Fatal(err)
						}
						var sequence int64
						if err := repo.View(ctx, func(store application.ReadStore) error {
							var err error
							sequence, err = store.LastWorkItemEventSequence(work.ID)
							return err
						}); err != nil {
							t.Fatal(err)
						}
						insufficient := command
						insufficient.MaxTaskInstancesPerNode = 2
						if _, err := service.ContinueWorkflow(ctx, insufficient); !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "recovery still reaches a task instance limit") {
							t.Fatalf("runtime recovery guard: %v", err)
						}
						if err := repo.View(ctx, func(store application.ReadStore) error {
							w, err := store.GetWorkItem(work.ID)
							if err != nil {
								return err
							}
							last, err := store.LastWorkItemEventSequence(work.ID)
							if err != nil {
								return err
							}
							tasks, err := store.ListTasks(work.ID)
							if w.Status != domain.WorkItemStatusFailed || w.Version != command.Version || w.WorkflowMaxTaskInstancesPerNode != 0 || len(tasks) != 6 || last != sequence {
								t.Fatal("rejected recovery committed partial state")
							}
							return err
						}); err != nil {
							t.Fatal(err)
						}
						continued, err := repositoryTestService(t, openPeer(t)).ContinueWorkflow(ctx, command)
						if err != nil {
							t.Fatal(err)
						}
						if continued.Status != domain.WorkItemStatusOpen || continued.Failure != nil || continued.WorkflowMaxTaskInstancesPerNode != 3 || continued.Definition != work.Definition {
							t.Fatalf("continued work: %+v", continued)
						}
						if _, err := service.ContinueWorkflow(ctx, command); !errors.Is(err, application.ErrConflict) {
							t.Fatalf("duplicate recovery: %v", err)
						}
						// Only a3 is newly materialized; old source submissions,
						// decisions, IDs, and revoked Claims survive unchanged.
						recoveredContext, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
						if err != nil {
							t.Fatal(err)
						}
						if len(recoveredContext.Tasks) != 8 || len(recoveredContext.ActiveClaims) != 0 {
							t.Fatal("recovery duplicated tasks or resurrected Claims")
						}
						for _, original := range failedContext.Tasks {
							found := false
							for _, current := range recoveredContext.Tasks {
								if current.ID == original.ID {
									found = true
									if !reflect.DeepEqual(current, original) {
										t.Fatalf("recovery rewrote task %s", current.ID)
									}
								}
							}
							if !found {
								t.Fatal("lost original task")
							}
						}
						a := pending("a")
						submit(a, claim(a), "continue:a-b")
						b := pending("b")
						submit(b, claim(b), "exit:b")
						done := pending("done")
						submit(done, claim(done), "")
						parallelRetry := pending("parallel")
						submit(parallelRetry, claim(parallelRetry), "")
						final, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
						if err != nil || final.WorkItem.Status != domain.WorkItemStatusCompleted {
							t.Fatalf("recovered workflow did not finish: %v / %s", err, final.WorkItem.Status)
						}

					}
				}
			})
		})
	}
}

func TestWorkflowRecoveryContinuesPartialFanout(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, openPeer func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		service := repositoryTestService(t, repo)
		actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
		definition := domain.WorkflowDefinition{
			DefinitionMetadata: domain.DefinitionMetadata{ID: "fanout", Version: 1, Name: "Fanout", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime},
			Graph: domain.WorkflowGraph{StartTaskIDs: []domain.WorkflowTaskID{"a", "b"}, MaxTaskInstancesPerNode: 1,
				Relations: []domain.WorkflowRelationDefinition{{ID: "ac", FromTaskID: "a", ToTaskID: "c"}, {ID: "ab", FromTaskID: "a", ToTaskID: "b"}, {ID: "ad", FromTaskID: "a", ToTaskID: "d"}}},
		}
		for _, id := range []domain.WorkflowTaskID{"a", "b", "c", "d"} {
			definition.Graph.Tasks = append(definition.Graph.Tasks, domain.WorkflowTaskDefinition{ID: id, Title: string(id), Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone})
		}
		if err := definition.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateWorkflowDefinition(ctx, definition); err != nil {
			t.Fatal(err)
		}
		work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: definition.Binding(), Identity: actor, Title: "Partial fanout", Goal: "Continue every undelivered edge"})
		if err != nil {
			t.Fatal(err)
		}
		initial, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range initial.Tasks {
			if *task.WorkflowTaskID != "a" {
				continue
			}
			c, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: task.ID, ClaimID: c.ID, Identity: actor, Result: "Approved", Transition: &application.WorkflowTransitionCommand{ChoiceGroupID: "exit:a"}})
			if err != nil {
				t.Fatal(err)
			}
		}
		failed, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		if failed.WorkItem.Failure == nil || failed.WorkItem.Failure.WorkflowTaskID != "b" || len(failed.Tasks) != 3 {
			t.Fatalf("expected partial fanout before b: %+v", failed.WorkItem)
		}
		_, err = repositoryTestService(t, openPeer(t)).ContinueWorkflow(ctx, application.ContinueWorkflowCommand{WorkItemID: work.ID, Identity: actor, Version: failed.WorkItem.Version, MaxTaskInstancesPerNode: 2})
		if err != nil {
			t.Fatal(err)
		}
		continued, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		counts := map[domain.WorkflowTaskID]int{}
		for _, task := range continued.Tasks {
			counts[*task.WorkflowTaskID]++
		}
		if !reflect.DeepEqual(counts, map[domain.WorkflowTaskID]int{"a": 1, "b": 2, "c": 1, "d": 1}) || len(continued.Relations) != 3 {
			t.Fatalf("recovery lost or duplicated a fanout edge: %v", counts)
		}
	})
}
