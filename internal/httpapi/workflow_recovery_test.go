package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
)

func TestHTTPWorkflowTaskInstanceLimitRecovery(t *testing.T) {
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
	definition := domain.WorkflowDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "recovery", Version: 1, Name: "Recovery", CreatedAt: endToEndClock{}.Now(), UpdatedAt: endToEndClock{}.Now()}, Graph: domain.WorkflowGraph{MaxTaskInstancesPerNode: 1, StartTaskIDs: []domain.WorkflowTaskID{"dev"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "dev", Title: "开发", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone}, {ID: "done", Title: "Done", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone}}, Relations: []domain.WorkflowRelationDefinition{{ID: "again", FromTaskID: "dev", ToTaskID: "dev"}, {ID: "finish", FromTaskID: "dev", ToTaskID: "done"}}}}
	if err := repo.CreateWorkflowDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: definition.Binding(), Identity: actor, Title: "Review feedback", Goal: "Continue after limit"})
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
	if failed.WorkItem.Failure == nil || failed.WorkItem.Failure.WorkflowTaskID != "dev" || failed.WorkItem.Failure.TaskInstances != 1 {
		t.Fatalf("failure response: %+v", failed.WorkItem.Failure)
	}

	rawContext := requestDataAs[map[string]any](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	rawFailure := rawContext["work_item"].(map[string]any)["failure"].(map[string]any)
	assertJSONKeys(t, rawFailure, []string{"task_instances"}, []string{"executions"})
	if rawFailure["kind"] != "workflow_task_instance_limit" || rawFailure["task_instances"] != float64(1) {
		t.Fatalf("failure contract: %v", rawFailure)
	}
	rawDefinition := requestDataAs[map[string]any](t, server.Client(), http.MethodGet, server.URL+"/api/v1/definitions/workflows/recovery/versions/1", nil, "", 200, human)
	graph := rawDefinition["graph"].(map[string]any)
	assertJSONKeys(t, graph, []string{"max_task_instances_per_node"}, []string{"max_task_executions"})
	if graph["max_task_instances_per_node"] != float64(1) {
		t.Fatalf("node instance limit: %v", graph)
	}
	for _, route := range []string{"/resume", "/restart"} {
		response, err := server.Client().Do(newTrustedRequest(t, http.MethodPost, url+route, map[string]any{"version": failed.WorkItem.Version}, "removed-route", human))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("removed route %s: %d", route, response.StatusCode)
		}
	}
	for _, request := range []struct {
		url  string
		body map[string]any
	}{
		{url + "/continue", map[string]any{"version": failed.WorkItem.Version, "max_task_executions": 500}},
		{server.URL + "/api/v1/definitions/workflows/old-field/versions", map[string]any{"name": "Old field", "graph": map[string]any{"max_task_executions": 1}}},
	} {
		data := rawTrustedJSONResponse(t, server.Client(), http.MethodPost, request.url, request.body, "", 400, human)
		if !strings.Contains(string(data), "unknown field") || !strings.Contains(string(data), "max_task_executions") {
			t.Fatalf("wrong field guard: %s", data)
		}
	}
	unchanged := requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	if !reflect.DeepEqual(failed, unchanged) {
		t.Fatal("rejected contract names changed Workflow state")
	}
	for _, body := range []map[string]any{{"max_task_instances_per_node": 2}, {"version": failed.WorkItem.Version, "max_task_instances_per_node": 1}, {"version": failed.WorkItem.Version, "max_task_instances_per_node": 1.5}} {
		requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", body, "", 400, "invalid_request", human)
	}
	body := map[string]any{"version": failed.WorkItem.Version, "max_task_instances_per_node": 500}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", body, "", 403, "forbidden", trustedTestIdentity{ID: "agent", Kind: domain.ActorAgent, Role: "developer"})
	continued := requestDataAs[domain.WorkItem](t, server.Client(), http.MethodPost, url+"/continue", body, "", 200, human)
	if continued.Failure != nil || continued.Status != domain.WorkItemStatusOpen || continued.WorkflowMaxTaskInstancesPerNode != 500 {
		t.Fatalf("continue response: %+v", continued)
	}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", body, "", 409, "conflict", human)
}

func TestHTTPWorkflowTaskRetryAndStartOver(t *testing.T) {
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
	def := domain.WorkflowDefinition{DefinitionMetadata: domain.DefinitionMetadata{ID: "retry-http", Version: 1, Name: "Retry", CreatedAt: endToEndClock{}.Now(), UpdatedAt: endToEndClock{}.Now()}, Graph: domain.WorkflowGraph{MaxTaskInstancesPerNode: 3, StartTaskIDs: []domain.WorkflowTaskID{"dev"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "dev", Title: "Dev", Executor: domain.ExecutorHuman, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone}}}}
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
		before := requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
		for _, removed := range []string{"reopen", "fail_task"} {
			requestErrorAs(t, server.Client(), http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/failures", map[string]any{"claim_id": claim.ID, "action": removed, "reason": "invalid action"}, "", 400, "invalid_request", human)
		}
		after := requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("removed failure action changed persisted state")
		}
		requestDataAs[domain.TaskFailure](t, server.Client(), http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/failures", map[string]any{"claim_id": claim.ID, "action": action, "reason": "需要修正配置"}, "", 201, human)
	}
	fail(task, domain.TaskFailureAwaitHuman)
	current = requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	notes := strings.Repeat("界", domain.MaxHistoryTextBytes/3) + "xx"
	body := map[string]any{"version": current.WorkItem.Version, "instructions": notes}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", body, "", 403, "forbidden", agent)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", map[string]any{}, "", 400, "invalid_request", human)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", map[string]any{"version": current.WorkItem.Version, "instructions": notes + "x"}, "", 400, "invalid_request", human)
	requestDataAs[domain.WorkItem](t, server.Client(), http.MethodPost, url+"/continue", body, "", 200, human)
	after := requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	replacement := after.Tasks[len(after.Tasks)-1]
	if replacement.RetryOfTaskID == nil || *replacement.RetryOfTaskID != task.ID || replacement.RetryInstructions != notes {
		t.Fatal("retry response lost context")
	}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", body, "", 409, "conflict", human)
	fail(replacement, domain.TaskFailureFailWorkItem)
	current = requestDataAs[application.WorkItemExecutionContext](t, server.Client(), http.MethodGet, url+"/context", nil, "", 200, human)
	body = map[string]any{"version": current.WorkItem.Version, "instructions": notes}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/start-over", body, "start-over-agent", 403, "forbidden", agent)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/start-over", body, "", 400, "invalid_request", human)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/start-over", map[string]any{"version": current.WorkItem.Version, "instructions": notes + "x"}, "too-long", 400, "invalid_request", human)
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", map[string]any{"version": current.WorkItem.Version, "instructions": notes + "x"}, "", 400, "invalid_request", human)
	startedOver := requestDataAs[domain.WorkItem](t, server.Client(), http.MethodPost, url+"/start-over", body, "start-over-1", 201, human)
	replay := requestDataAs[domain.WorkItem](t, server.Client(), http.MethodPost, url+"/start-over", body, "start-over-1", 201, human)
	if startedOver.ID == work.ID || replay.ID != startedOver.ID || startedOver.StartedOverFromWorkItemID == nil || *startedOver.StartedOverFromWorkItemID != work.ID || startedOver.RecoveryInstructions != notes {
		t.Fatal("start-over response lost source or instructions")
	}
	requestErrorAs(t, server.Client(), http.MethodPost, url+"/continue", map[string]any{"version": current.WorkItem.Version}, "", 409, "conflict", human)
}
