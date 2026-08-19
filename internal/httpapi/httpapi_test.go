package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
)

var endToEndTime = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func TestTrustedHTTPBlackboardExecutionEndToEnd(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "kairos.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	service, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatalf("new HTTP API: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := server.Client()
	blackboardDefinition := requestData[domain.BlackboardDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/blackboards", map[string]any{
		"id": "engineering", "version": 1, "name": "Engineering", "status": "published",
		"suggested_tags": []string{"backend", "database"},
	}, "create-blackboard-definition", http.StatusCreated)
	if blackboardDefinition.ID != "engineering" {
		t.Fatalf("blackboard definition ID = %q", blackboardDefinition.ID)
	}

	workflowDefinition := requestData[domain.WorkflowDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/workflows", map[string]any{
		"id": "delivery", "version": 1, "name": "Delivery", "status": "published",
		"suggested_tags": []string{},
		"graph": map[string]any{
			"start_task_ids": []string{"implement"},
			"tasks": []map[string]any{{
				"id": "implement", "title": "Implement", "executor": "agent",
				"allowed_roles": []string{"database"}, "execution": "required",
				"review_policy": "none", "default_tags": []string{"backend"},
			}},
		},
	}, "create-workflow-definition", http.StatusCreated)
	if len(workflowDefinition.Graph.Tasks) != 1 || workflowDefinition.Graph.Tasks[0].ID != "implement" {
		t.Fatalf("workflow graph = %+v", workflowDefinition.Graph)
	}
	if workflowDefinition.SuggestedTags == nil || workflowDefinition.Graph.Relations == nil {
		t.Fatalf("workflow definition contains nil collections: %#v", workflowDefinition)
	}

	workflows := requestData[[]domain.WorkflowDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/workflows", nil, "", http.StatusOK)
	blackboards := requestData[[]domain.BlackboardDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/blackboards", nil, "", http.StatusOK)
	if len(workflows) != 1 || len(blackboards) != 1 {
		t.Fatalf("definition lists have %d workflows and %d blackboards", len(workflows), len(blackboards))
	}
	retrievedWorkflow := requestData[domain.WorkflowDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/workflows/delivery/versions/1", nil, "", http.StatusOK)
	retrievedBlackboard := requestData[domain.BlackboardDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/blackboards/engineering/versions/1", nil, "", http.StatusOK)
	if retrievedWorkflow.ID != workflowDefinition.ID || retrievedBlackboard.ID != blackboardDefinition.ID {
		t.Fatalf("retrieved definitions = %q and %q", retrievedWorkflow.ID, retrievedBlackboard.ID)
	}

	workItem := requestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "engineering", "mode": "blackboard",
		"title": "Ship storage change", "goal": "Deliver a tested storage change", "tags": []string{"backend"},
	}, "create-work-item", http.StatusCreated)
	listedWorkItems := requestData[[]domain.WorkItem](t, client, http.MethodGet,
		server.URL+"/api/v1/work-items?status=open&mode=blackboard&tag=backend", nil, "", http.StatusOK)
	if len(listedWorkItems) != 1 || listedWorkItems[0].ID != workItem.ID {
		t.Fatalf("listed WorkItems = %+v, want %q", listedWorkItems, workItem.ID)
	}

	candidates := requestData[[]application.WorkCandidate](t, client, http.MethodGet, server.URL+"/api/v1/work?tag=backend", nil, "", http.StatusOK)
	if len(candidates) != 1 || candidates[0].Kind != application.WorkCandidateEmptyBlackboard {
		t.Fatalf("new Blackboard candidates = %+v, want one empty Blackboard", candidates)
	}

	task := requestData[domain.Task](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/tasks", map[string]any{
		"title": "Implement migration", "executor": "agent",
		"allowed_roles": []string{"database"}, "tags": []string{"backend", "database"},
	}, "create-task", http.StatusCreated)

	candidates = requestData[[]application.WorkCandidate](t, client, http.MethodGet, server.URL+"/api/v1/work?tag=backend", nil, "", http.StatusOK)
	if len(candidates) != 1 || candidates[0].Task.ID != task.ID {
		t.Fatalf("candidates = %+v, want task %q", candidates, task.ID)
	}

	claim := requestData[domain.Claim](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/claims", nil, "claim-task", http.StatusCreated)
	retriedClaim := requestData[domain.Claim](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/claims", nil, "claim-task", http.StatusCreated)
	if retriedClaim.ID != claim.ID {
		t.Fatalf("idempotent claim ID = %q, want %q", retriedClaim.ID, claim.ID)
	}

	executionContext := requestData[application.TaskExecutionContext](t, client, http.MethodGet, server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, "", http.StatusOK)
	if executionContext.Task.ActiveClaimID == nil || *executionContext.Task.ActiveClaimID != claim.ID {
		t.Fatalf("execution context has active claim %v, want %q", executionContext.Task.ActiveClaimID, claim.ID)
	}
	activeWorkItemContext := requestData[application.WorkItemExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/context", nil, "", http.StatusOK)
	if len(activeWorkItemContext.ActiveClaims) != 1 || activeWorkItemContext.ActiveClaims[0].ID != claim.ID ||
		activeWorkItemContext.ActiveClaims[0].Executor.ID != claim.Executor.ID {
		t.Fatalf("active WorkItem claims = %+v, want claim %q by %q", activeWorkItemContext.ActiveClaims, claim.ID, claim.Executor.ID)
	}

	submission := requestData[domain.TaskSubmission](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/submissions", map[string]any{
		"claim_id": claim.ID, "result": "Migration implemented and tested",
	}, "submit-task", http.StatusCreated)
	if submission.Result != "Migration implemented and tested" {
		t.Fatalf("submission result = %q", submission.Result)
	}
	workItemContext := requestData[application.WorkItemExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/context", nil, "", http.StatusOK)
	if workItemContext.WorkItem.Status != domain.WorkItemStatusCompleted || workItemContext.WorkItem.Result != submission.Result {
		t.Fatalf("completed WorkItem context = %+v", workItemContext)
	}
	if len(workItemContext.Tasks) != 1 || workItemContext.Tasks[0].ID != task.ID || workItemContext.Relations == nil {
		t.Fatalf("completed WorkItem coordination context = %+v", workItemContext)
	}
	if workItemContext.ActiveClaims == nil || len(workItemContext.ActiveClaims) != 0 {
		t.Fatalf("completed WorkItem active claims = %#v, want non-nil empty slice", workItemContext.ActiveClaims)
	}
	if len(workItemContext.Claims) != 1 || workItemContext.Claims[0].ID != claim.ID || workItemContext.Claims[0].Executor.ID != claim.Executor.ID {
		t.Fatalf("completed WorkItem claim history = %#v, want claim %q by %q", workItemContext.Claims, claim.ID, claim.Executor.ID)
	}

	candidates = requestData[[]application.WorkCandidate](t, client, http.MethodGet, server.URL+"/api/v1/work", nil, "", http.StatusOK)
	if candidates == nil || len(candidates) != 0 {
		t.Fatalf("candidates after submission = %#v, want a non-nil empty list", candidates)
	}

	workflowWorkItem := requestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "delivery", "mode": "workflow",
		"title": "Ship API", "goal": "Deliver the API", "tags": []string{"backend"},
	}, "create-workflow-item", http.StatusCreated)
	candidates = requestData[[]application.WorkCandidate](t, client, http.MethodGet, server.URL+"/api/v1/work?tag=backend", nil, "", http.StatusOK)
	if len(candidates) != 1 || candidates[0].WorkItem.ID != workflowWorkItem.ID || candidates[0].Task.WorkflowTaskID == nil {
		t.Fatalf("new Workflow candidates = %+v, want its instantiated start task", candidates)
	}
	detail := requestDataAs[application.TaskDetail](t, client, http.MethodGet, server.URL+"/api/v1/tasks/"+string(candidates[0].Task.ID), nil, "", http.StatusOK, trustedTestIdentity{ID: "operator", Kind: domain.ActorHuman})
	if detail.Task.ID != candidates[0].Task.ID || detail.Task.AllowedRoles == nil || detail.Task.Tags == nil || detail.Task.Reviews == nil || detail.Task.Submissions == nil || detail.Task.Failures == nil || detail.Task.TransitionDecisions == nil || detail.History.Claims == nil || detail.History.Submissions == nil || detail.History.Reviews == nil || detail.History.Failures == nil || detail.History.TransitionDecisions == nil {
		t.Fatalf("human task detail or empty collection contract: %#v", detail)
	}

	err = repo.View(ctx, func(store application.ReadStore) error {
		stored, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		if stored.Status != domain.WorkItemStatusCompleted {
			return fmt.Errorf("work item status = %q, want completed", stored.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify persisted result: %v", err)
	}
}

func TestHTTPAPIRejectsMissingTrustedIdentity(t *testing.T) {
	repo, err := repository.OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	service, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatalf("new HTTP API: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/work", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestHTTPAPIRejectsPublishedWorkflowWithoutGraph(t *testing.T) {
	repo, err := repository.OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	service, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatalf("new HTTP API: %v", err)
	}

	body := bytes.NewBufferString(`{"id":"invalid","version":1,"name":"Invalid","status":"published","graph":{}}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/definitions/workflows", body)
	request.Header.Set(identity.HeaderActorID, "planner")
	request.Header.Set(identity.HeaderActorRole, "architect")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.Error.Code != "invalid_request" {
		t.Fatalf("error code = %q, want invalid_request", envelope.Error.Code)
	}
}

func TestTrustedHTTPIdentityEnforcementEndToEnd(t *testing.T) {
	repo, err := repository.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	service, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatalf("new HTTP API: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()

	owner := trustedTestIdentity{ID: "codex-owner", Role: "database"}
	wrongRole := trustedTestIdentity{ID: "codex-frontend", Role: "frontend"}
	otherActor := trustedTestIdentity{ID: "codex-other", Role: "database"}
	humanReviewer := trustedTestIdentity{ID: "reviewer", Kind: domain.ActorHuman}

	requestDataAs[domain.BlackboardDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/blackboards", map[string]any{
		"id": "identity-test", "version": 1, "name": "Identity test", "status": "published",
	}, "create-definition", http.StatusCreated, owner)
	workItem := requestDataAs[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "identity-test", "mode": "blackboard",
		"title": "Verify identity", "goal": "Enforce execution ownership",
	}, "create-work-item", http.StatusCreated, owner)
	task := requestDataAs[domain.Task](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/tasks", map[string]any{
		"title": "Protected task", "executor": "agent", "allowed_roles": []string{"database"},
	}, "create-task", http.StatusCreated, owner)

	candidates := requestDataAs[[]application.WorkCandidate](t, client, http.MethodGet, server.URL+"/api/v1/work", nil, "", http.StatusOK, wrongRole)
	if len(candidates) != 0 {
		t.Fatalf("wrong-role actor discovered protected work: %+v", candidates)
	}
	requestErrorAs(t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/claims", nil,
		"wrong-role-claim", http.StatusForbidden, "forbidden", wrongRole)

	claim := requestDataAs[domain.Claim](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/claims", nil,
		"owner-claim", http.StatusCreated, owner)
	requestErrorAs(t, client, http.MethodGet, server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil,
		"", http.StatusForbidden, "forbidden", otherActor)
	requestErrorAs(t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/submissions", map[string]any{
		"claim_id": claim.ID, "result": "Attempted takeover",
	}, "other-submit", http.StatusForbidden, "forbidden", otherActor)

	requestDataAs[domain.TaskSubmission](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/submissions", map[string]any{
		"claim_id": claim.ID, "result": "Ready for review", "request_review": true,
	}, "owner-submit", http.StatusCreated, owner)
	executionContext := requestDataAs[application.TaskExecutionContext](t, client, http.MethodGet, server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil,
		"", http.StatusOK, owner)
	if len(executionContext.Task.Reviews) != 1 {
		t.Fatalf("reviews = %+v, want one pending review", executionContext.Task.Reviews)
	}
	reviewID := executionContext.Task.Reviews[0].ID
	reviewURL := server.URL + "/api/v1/tasks/" + string(task.ID) + "/reviews/" + string(reviewID) + "/decision"
	requestErrorAs(t, client, http.MethodPost, reviewURL, map[string]any{"decision": "approved"},
		"agent-review", http.StatusForbidden, "forbidden", owner)
	review := requestDataAs[domain.Review](t, client, http.MethodPost, reviewURL, map[string]any{"decision": "approved"},
		"human-review", http.StatusOK, humanReviewer)
	if review.Status != domain.ReviewStatusApproved || review.DecidedBy == nil || *review.DecidedBy != domain.ActorID(humanReviewer.ID) {
		t.Fatalf("review = %+v, want approval by %q", review, humanReviewer.ID)
	}

	err = repo.View(context.Background(), func(store application.ReadStore) error {
		stored, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		if stored.Status != domain.WorkItemStatusCompleted {
			return fmt.Errorf("work item status = %q, want completed", stored.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify reviewed work item: %v", err)
	}
}

func requestData[T any](
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body any,
	operationID string,
	wantStatus int,
) T {
	t.Helper()
	return requestDataAs[T](t, client, method, url, body, operationID, wantStatus, trustedTestIdentity{
		ID: "codex-storage", Role: "database",
	})
}

type trustedTestIdentity struct {
	ID   string
	Kind domain.ActorKind
	Role string
}

func requestDataAs[T any](
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body any,
	operationID string,
	wantStatus int,
	actor trustedTestIdentity,
) T {
	t.Helper()
	request := newTrustedRequest(t, method, url, body, operationID, actor)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", method, url, response.StatusCode, wantStatus, content)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		t.Fatalf("decode response %s: %v", content, err)
	}
	return envelope.Data
}

func requestErrorAs(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body any,
	operationID string,
	wantStatus int,
	wantCode string,
	actor trustedTestIdentity,
) {
	t.Helper()
	request := newTrustedRequest(t, method, url, body, operationID, actor)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer response.Body.Close()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.StatusCode != wantStatus || envelope.Error.Code != wantCode {
		t.Fatalf("%s %s returned status %d and code %q, want %d and %q", method, url, response.StatusCode, envelope.Error.Code, wantStatus, wantCode)
	}
}

func newTrustedRequest(
	t *testing.T,
	method string,
	url string,
	body any,
	operationID string,
	actor trustedTestIdentity,
) *http.Request {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, requestBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set(identity.HeaderActorID, actor.ID)
	if actor.Kind != "" {
		request.Header.Set(identity.HeaderActorKind, string(actor.Kind))
	}
	if actor.Role != "" {
		request.Header.Set(identity.HeaderActorRole, actor.Role)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if operationID != "" {
		request.Header.Set("Idempotency-Key", operationID)
	}
	return request
}

type endToEndClock struct{}

func (endToEndClock) Now() time.Time { return endToEndTime }

type endToEndIDs struct{}

var endToEndIDSequence atomic.Uint64

func (*endToEndIDs) NewID() string {
	return fmt.Sprintf("e2e-%d", endToEndIDSequence.Add(1))
}
