package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestHumanAttentionRecoveryHistoryScale(t *testing.T) {
	repo, service, human := prepareHumanAttentionHistory(t, 10000, 1000)
	page := application.PageRequest[application.HumanAttentionCursor]{Limit: 50}
	query, args := humanAttentionQuery(human.Actor, page)
	rows, err := repo.db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	parents, details := map[int]int{}, map[int]string{}
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		parents[id], details[id] = parent, detail
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// Check the actual query's plan, not wall-clock thresholds that vary in CI.
	// A source-history scan is fine once, but not inside a correlated subquery.
	for id, detail := range details {
		if strings.Contains(detail, "SCAN replacement") {
			for parent := parents[id]; parent != 0; parent = parents[parent] {
				if strings.Contains(details[parent], "CORRELATED") {
					t.Fatalf("start-over history scanned per candidate: %v", details)
				}
			}
		}
	}
	result, err := service.ListHumanAttention(context.Background(), human, page)
	if err != nil || len(result.Items) != 11 || result.HasMore {
		t.Fatalf("scale feed: %+v, %v", result, err)
	}
	recoveries := 0
	for _, item := range result.Items {
		if item.Kind == application.HumanAttentionRecovery {
			recoveries++
			if item.WorkItem.ID == "scale-work-10000" {
				t.Fatal("start-over source remains in recovery feed")
			}
		}
	}
	if recoveries != 9 {
		t.Fatalf("got %d recovery entries, want 9", recoveries)
	}
}

func BenchmarkHumanAttentionRecoveryHistory(b *testing.B) {
	for _, active := range []int{100, 1000} {
		b.Run(fmt.Sprintf("history_10000_active_%d", active), func(b *testing.B) {
			_, service, human := prepareHumanAttentionHistory(b, 10000, active)
			page := application.PageRequest[application.HumanAttentionCursor]{Limit: 50}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := service.ListHumanAttention(context.Background(), human, page)
				if err != nil || len(result.Items) != 11 {
					b.Fatalf("scale feed returned %d items: %v", len(result.Items), err)
				}
			}
		})
	}
}

func prepareHumanAttentionHistory(t testing.TB, history, active int) (*SQLRepository, *application.Service, application.Identity) {
	t.Helper()
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	definition := attemptDefinition()
	if err := repo.CreateWorkflowDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	service, err := application.NewService(repo, repositoryTestClock{}, &repositoryTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	human := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
	base, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: definition.Binding(), Identity: human, Title: "Scale", Goal: "Read attention with accumulated history"})
	if err != nil {
		t.Fatal(err)
	}
	// Aggregate snapshots use the repository's domain validation, without timing setup.
	if err := repo.Update(ctx, func(store application.WriteStore) error {
		tasks, err := store.ListTasks(base.ID)
		if err != nil {
			return err
		}
		for i := 0; i < history+active; i++ {
			work := base
			work.ID = domain.WorkItemID(fmt.Sprintf("scale-work-%d", i))
			if i < history {
				work.Status, work.CompletedAt = domain.WorkItemStatusCompleted, &repositoryTestTime
			}
			if err := store.CreateWorkItem(work); err != nil {
				return err
			}
			if i < history {
				continue
			}
			task := tasks[0]
			task.ID, task.WorkItemID, task.Executor = domain.TaskID(fmt.Sprintf("scale-task-%d", i)), work.ID, domain.ExecutorAgent
			if i < history+10 {
				task.Status = domain.TaskStatusFailed
				task.Failures = []domain.TaskFailure{{ID: domain.TaskFailureID(fmt.Sprintf("scale-failure-%d", i)), TaskID: task.ID, ClaimID: domain.ClaimID(fmt.Sprintf("scale-claim-%d", i)), Action: domain.TaskFailureAwaitHuman, Reason: "Needs access", FailedAt: repositoryTestTime}}
			}
			if err := store.CreateTask(task); err != nil {
				return err
			}
		}
		source := domain.WorkItemID(fmt.Sprintf("scale-work-%d", history))
		replacement := base
		replacement.ID, replacement.StartedOverFromWorkItemID = "scale-replacement", &source
		return store.CreateWorkItem(replacement)
	}); err != nil {
		t.Fatal(err)
	}
	return repo, service, human
}
