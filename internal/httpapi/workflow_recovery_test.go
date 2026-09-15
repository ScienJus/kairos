package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
)

func TestHTTPWorkflowExecutionLimitRecovery(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	human := trustedTestIdentity{ID: "operator", Kind: domain.ActorHuman}
	actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
	definition := domain.WorkflowDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "recovery", Version: 1, Name: "Recovery", CreatedAt: endToEndClock{}.Now(), UpdatedAt: endToEndClock{}.Now()}, Graph: domain.WorkflowGraph{MaxTaskExecutions: 1, StartTaskIDs: []domain.WorkflowTaskID{"dev"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "dev", Title: "开发", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone}, {ID: "done", Title: "Done", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone}}, Relations: []domain.WorkflowRelationDefinition{{ID: "again", FromTaskID: "dev", ToTaskID: "dev"}, {ID: "finish", FromTaskID: "dev", ToTaskID: "done"}}}}
	if err := repo.CreateWorkflowDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: definition.Binding(), Identity: actor, Title: "Review feedback", Goal: "Resume after limit"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{WorkItemID: work.ID, Identity: actor})
	if err != nil {
		t.Fatal(err)
	}
	task := current.Tasks[0]
	claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: actor, Result: "需要返工", Transition: &application.WorkflowTransitionCommand{ChoiceGroupID: "continue:again"}}); err != nil {
		t.Fatal(err)
	}
	url := server.URL + "/api/v1/work-items/" + string(work.ID)
	failed := requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", http.StatusOK, human)
	if failed.WorkItem.Failure == nil || failed.WorkItem.Failure.WorkflowTaskID != "dev" || failed.WorkItem.Failure.Executions != 1 {
		t.Fatalf("failure response: %+v", failed.WorkItem.Failure)
	}
	for _, body := range []map[string]any{{"max_task_executions": 2}, {"version": failed.WorkItem.Version, "max_task_executions": 1}, {"version": failed.WorkItem.Version, "max_task_executions": 1.5}} {
		requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", body, "", 400, "invalid_request", human)
	}
	body := map[string]any{"version": failed.WorkItem.Version, "max_task_executions": 500}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", body, "", 403, "forbidden", trustedTestIdentity{ID: "agent", Kind: domain.ActorAgent, Role: "developer"})
	resumed := requestDataAs[domain.WorkItem](t, server.Client(), http.MethodPost, url+"/resume", body, "", 200, human)
	if resumed.Failure != nil || resumed.Status != domain.WorkItemStatusOpen || resumed.WorkflowMaxTaskExecutions != 500 {
		t.Fatalf("resume response: %+v", resumed)
	}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", body, "", 409, "conflict", human)
}

func TestHTTPWorkflowTaskRetryAndRestart(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	human := trustedTestIdentity{ID: "operator", Kind: domain.ActorHuman}
	agent := trustedTestIdentity{ID: "agent", Kind: domain.ActorAgent, Role: "developer"}
	actor := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
	def := domain.WorkflowDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "retry-http", Version: 1, Name: "Retry", CreatedAt: endToEndClock{}.Now(), UpdatedAt: endToEndClock{}.Now()}, Graph: domain.WorkflowGraph{MaxTaskExecutions: 3, StartTaskIDs: []domain.WorkflowTaskID{"dev"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "dev", Title: "Dev", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone}}}}
	if err := repo.CreateWorkflowDefinition(ctx, def); err != nil {
		t.Fatal(err)
	}
	work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: def.Binding(), Identity: actor, Title: "Retry", Goal: "Preserve history"})
	if err != nil {
		t.Fatal(err)
	}
	url := server.URL + "/api/v1/work-items/" + string(work.ID)
	current := requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	task := current.Tasks[0]
	fail := func(task domain.Task, action domain.TaskFailureAction) {
		t.Helper()
		claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: actor})
		if err != nil {
			t.Fatal(err)
		}
		requestDataAs[domain.TaskFailure](t, server.Client(), http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/failures", map[string]any{"claim_id": claim.ID, "action": action, "reason": "需要修正配置"}, "", 201, human)
	}
	fail(task, domain.TaskFailureStop)
	current = requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	notes := strings.Repeat("界", domain.MaxHistoryTextBytes/3) + "xx"
	body := map[string]any{"version": current.WorkItem.Version, "instructions": notes}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", body, "", 403, "forbidden", agent)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", map[string]any{}, "", 400, "invalid_request", human)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", map[string]any{"version": current.WorkItem.Version, "instructions": notes + "x"}, "", 400, "invalid_request", human)
	requestDataAs[domain.WorkItem](t, server.Client(), http.MethodPost, url+"/resume", body, "", 200, human)
	after := requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	replacement := after.Tasks[len(after.Tasks)-1]
	if replacement.RetryOfTaskID == nil || *replacement.RetryOfTaskID != task.ID || replacement.RetryInstructions != notes {
		t.Fatal("retry response lost context")
	}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", body, "", 409, "conflict", human)
	fail(replacement, domain.TaskFailureFailWorkItem)
	current = requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	body = map[string]any{"version": current.WorkItem.Version, "instructions": notes}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/restart", body, "restart-agent", 403, "forbidden", agent)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/restart", body, "", 400, "invalid_request", human)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/restart", map[string]any{"version": current.WorkItem.Version, "instructions": notes + "x"}, "too-long", 400, "invalid_request", human)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", map[string]any{"version": current.WorkItem.Version, "instructions": notes + "x"}, "", 400, "invalid_request", human)
	restarted := requestDataAs[domain.WorkItem](t, server.Client(), http.MethodPost, url+"/restart", body, "restart-1", 201, human)
	replay := requestDataAs[domain.WorkItem](t, server.Client(), http.MethodPost, url+"/restart", body, "restart-1", 201, human)
	if restarted.ID == work.ID || replay.ID != restarted.ID || restarted.RestartOfWorkItemID == nil || *restarted.RestartOfWorkItemID != work.ID || restarted.RecoveryInstructions != notes {
		t.Fatal("restart response lost source or instructions")
	}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/resume", map[string]any{"version": current.WorkItem.Version}, "", 409, "conflict", human)
}
