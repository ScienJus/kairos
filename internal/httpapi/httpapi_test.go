package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/artifactstore"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
)

var endToEndTime = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func TestArtifactUploadLimitIsConfigurable(t *testing.T) {
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
	handler, err := httpapi.New(service, identity.TrustedResolver{}, httpapi.Options{MaxArtifactUploadBytes: 64})
	if err != nil {
		t.Fatalf("new HTTP API: %v", err)
	}
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	part, err := multipartWriter.CreateFormFile("file", "large.bin")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), 65)); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/task/artifact-uploads", &body)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set(identity.HeaderActorID, "operator")
	request.Header.Set(identity.HeaderActorKind, string(domain.ActorHuman))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("upload status = %d, want %d: %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != "artifact_too_large" {
		t.Fatalf("error code = %q", response.Error.Code)
	}
}

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
	localArtifacts, err := artifactstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local Artifact Store: %v", err)
	}
	if err := service.ConfigureArtifactStore(localArtifacts); err != nil {
		t.Fatalf("configure Artifact Stores: %v", err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatalf("new HTTP API: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := server.Client()
	config := requestData[authenticationConfigPayload](t, client, http.MethodGet, server.URL+"/api/v1/auth/config", nil, "", http.StatusOK)
	if config.Mode != httpapi.AuthenticationModeTrusted {
		t.Fatalf("authentication mode = %q, want trusted", config.Mode)
	}
	session := requestData[sessionPayload](t, client, http.MethodGet, server.URL+"/api/v1/session", nil, "", http.StatusOK)
	if session.ID != "codex-storage" || session.Kind != domain.ActorAgent || session.Role != "database" {
		t.Fatalf("trusted session = %+v", session)
	}
	blackboardDefinition := requestData[domain.BlackboardDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/blackboards/engineering/versions", map[string]any{
		"name":           "Engineering",
		"suggested_tags": []string{"backend", "database"},
	}, "create-blackboard-definition", http.StatusCreated)
	if blackboardDefinition.ID != "engineering" {
		t.Fatalf("blackboard definition ID = %q", blackboardDefinition.ID)
	}

	workflowDefinition := requestData[domain.WorkflowDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/workflows/delivery/versions", map[string]any{
		"name":           "Delivery",
		"suggested_tags": []string{},
		"graph": map[string]any{
			"start_task_ids": []string{"implement"},
			"tasks": []map[string]any{{
				"id": "implement", "title": "Implement", "executor": "agent",
				"allowed_roles": []string{"database"}, "execution": "required",
				"review_policy": "none", "default_tags": []string{"backend"},
				"artifacts": []map[string]any{{"name": "commit", "description": " Provide the immutable Git commit. "}},
			}, {
				"id": "verify", "title": "Verify", "executor": "agent",
				"allowed_roles": []string{"database"}, "execution": "optional",
				"review_policy": "none", "default_tags": []string{},
			}},
			"relations": []map[string]any{{
				"id": "implement-verify", "from_task_id": "implement", "to_task_id": "verify",
				"label": " Needs verification ", "agent_guidance": " Keep this step when storage behavior changed. ",
			}},
		},
	}, "create-workflow-definition", http.StatusCreated)
	if len(workflowDefinition.Graph.Tasks) != 2 || workflowDefinition.Graph.Tasks[0].ID != "implement" {
		t.Fatalf("workflow graph = %+v", workflowDefinition.Graph)
	}
	if len(workflowDefinition.Graph.Tasks[0].Artifacts) != 1 || workflowDefinition.Graph.Tasks[0].Artifacts[0].Description != "Provide the immutable Git commit." {
		t.Fatalf("workflow Artifact Definitions = %+v", workflowDefinition.Graph.Tasks[0].Artifacts)
	}
	if len(workflowDefinition.Graph.Relations) != 1 || workflowDefinition.Graph.Relations[0].Label != "Needs verification" || workflowDefinition.Graph.Relations[0].AgentGuidance != "Keep this step when storage behavior changed." {
		t.Fatalf("workflow relation guidance = %+v", workflowDefinition.Graph.Relations)
	}
	if workflowDefinition.SuggestedTags == nil || workflowDefinition.Graph.Relations == nil {
		t.Fatalf("workflow definition contains nil collections: %#v", workflowDefinition)
	}

	workflows := requestData[[]domain.WorkflowDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/workflows", nil, "", http.StatusOK)
	blackboards := requestData[[]domain.BlackboardDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/blackboards", nil, "", http.StatusOK)
	if len(workflows) != 1 || len(blackboards) != 1 {
		t.Fatalf("definition lists have %d workflows and %d blackboards", len(workflows), len(blackboards))
	}
	requestData[domain.WorkflowDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/workflows/delivery/versions", map[string]any{
		"base_version": workflowDefinition.Version, "name": "Delivery v2", "suggested_tags": []string{},
		"graph": map[string]any{
			"start_task_ids": []string{"implement"},
			"tasks": []map[string]any{{
				"id": "implement", "title": "Implement", "executor": "agent", "allowed_roles": []string{"database"},
				"execution": "required", "review_policy": "none", "default_tags": []string{}, "artifacts": []map[string]any{},
			}},
			"relations": []map[string]any{},
		},
	}, "create-workflow-definition-v2", http.StatusCreated)
	requestErrorAs(t, client, http.MethodPost, server.URL+"/api/v1/definitions/workflows/delivery/versions", map[string]any{
		"base_version": workflowDefinition.Version, "name": "Stale Delivery edit", "suggested_tags": []string{},
		"graph": map[string]any{
			"start_task_ids": []string{"implement"},
			"tasks": []map[string]any{{
				"id": "implement", "title": "Implement", "executor": "agent", "allowed_roles": []string{"database"},
				"execution": "required", "review_policy": "none", "default_tags": []string{}, "artifacts": []map[string]any{},
			}},
			"relations": []map[string]any{},
		},
	}, "stale-workflow-definition", http.StatusConflict, "conflict", trustedTestIdentity{ID: "codex-storage", Role: "database"})
	latestWorkflow := requestData[domain.WorkflowDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/workflows/delivery", nil, "", http.StatusOK)
	if latestWorkflow.ID != "delivery" || latestWorkflow.Version != 2 {
		t.Fatalf("latest Workflow = %#v", latestWorkflow)
	}
	workflowCatalog := requestPage[domain.WorkflowDefinition](t, client, server.URL+"/api/v1/definitions/workflows")
	if len(workflowCatalog.Data) != 1 || workflowCatalog.Data[0].ID != "delivery" || workflowCatalog.Data[0].Version != 2 {
		t.Fatalf("Workflow catalog = %#v", workflowCatalog)
	}
	workflowVersions := requestPage[domain.WorkflowDefinition](t, client, server.URL+"/api/v1/definitions/workflows/delivery/versions?limit=1")
	if len(workflowVersions.Data) != 1 || workflowVersions.Data[0].Version != 2 || workflowVersions.NextCursor == nil {
		t.Fatalf("Workflow version page = %#v", workflowVersions)
	}
	requestErrorAs(t, client, http.MethodGet, server.URL+"/api/v1/definitions/workflows/missing/versions", nil,
		"", http.StatusNotFound, "not_found", trustedTestIdentity{ID: "codex-storage", Role: "database"})
	secondBlackboard := requestData[domain.BlackboardDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/blackboards/operations/versions", map[string]any{
		"name": "Operations", "suggested_tags": []string{},
	}, "create-second-blackboard-definition", http.StatusCreated)
	firstDefinitionPage := requestPage[domain.BlackboardDefinition](t, client, server.URL+"/api/v1/definitions/blackboards?limit=1")
	if len(firstDefinitionPage.Data) != 1 || firstDefinitionPage.NextCursor == nil {
		t.Fatalf("first Definition page = %#v", firstDefinitionPage)
	}
	secondDefinitionPage := requestPage[domain.BlackboardDefinition](t, client, server.URL+"/api/v1/definitions/blackboards?limit=1&cursor="+*firstDefinitionPage.NextCursor)
	if len(secondDefinitionPage.Data) != 1 || secondDefinitionPage.NextCursor != nil || secondDefinitionPage.Data[0].ID == firstDefinitionPage.Data[0].ID ||
		(secondDefinitionPage.Data[0].ID != blackboardDefinition.ID && secondDefinitionPage.Data[0].ID != secondBlackboard.ID) {
		t.Fatalf("second Definition page = %#v after %#v", secondDefinitionPage, firstDefinitionPage)
	}
	requestErrorAs(t, client, http.MethodGet, server.URL+"/api/v1/definitions/workflows?cursor="+*firstDefinitionPage.NextCursor, nil,
		"", http.StatusBadRequest, "invalid_request", trustedTestIdentity{ID: "codex-storage", Role: "database"})
	retrievedWorkflow := requestData[domain.WorkflowDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/workflows/delivery/versions/1", nil, "", http.StatusOK)
	retrievedBlackboard := requestData[domain.BlackboardDefinition](t, client, http.MethodGet, server.URL+"/api/v1/definitions/blackboards/engineering/versions/1", nil, "", http.StatusOK)
	if retrievedWorkflow.ID != workflowDefinition.ID || retrievedBlackboard.ID != blackboardDefinition.ID {
		t.Fatalf("retrieved definitions = %q and %q", retrievedWorkflow.ID, retrievedBlackboard.ID)
	}
	if len(retrievedWorkflow.Graph.Relations) != 1 || retrievedWorkflow.Graph.Relations[0].AgentGuidance != workflowDefinition.Graph.Relations[0].AgentGuidance {
		t.Fatalf("retrieved workflow relation guidance = %+v", retrievedWorkflow.Graph.Relations)
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
	cancelTarget := requestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "engineering", "mode": "blackboard",
		"title": "Superseded work", "goal": "Exercise cancellation", "tags": []string{"cancel-test"},
	}, "create-cancel-target", http.StatusCreated)
	firstWorkItemPage := requestPage[domain.WorkItem](t, client, server.URL+"/api/v1/work-items?limit=1")
	if len(firstWorkItemPage.Data) != 1 || firstWorkItemPage.NextCursor == nil {
		t.Fatalf("first WorkItem page = %#v", firstWorkItemPage)
	}
	secondWorkItemPage := requestPage[domain.WorkItem](t, client, server.URL+"/api/v1/work-items?limit=1&cursor="+*firstWorkItemPage.NextCursor)
	if len(secondWorkItemPage.Data) != 1 || secondWorkItemPage.Data[0].ID == firstWorkItemPage.Data[0].ID {
		t.Fatalf("second WorkItem page = %#v after %#v", secondWorkItemPage, firstWorkItemPage)
	}
	requestErrorAs(t, client, http.MethodGet, server.URL+"/api/v1/work-items?limit=0", nil, "", http.StatusBadRequest, "invalid_request",
		trustedTestIdentity{ID: "codex-storage", Role: "database"})
	requestErrorAs(t, client, http.MethodGet, server.URL+"/api/v1/work-items?cursor=not-a-cursor", nil, "", http.StatusBadRequest, "invalid_request",
		trustedTestIdentity{ID: "codex-storage", Role: "database"})
	cancelTask := requestData[domain.Task](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(cancelTarget.ID)+"/tasks", map[string]any{
		"title": "Obsolete task", "executor": "agent", "allowed_roles": []string{"database"}, "tags": []string{},
	}, "create-cancel-task", http.StatusCreated)
	cancelClaim := requestData[domain.Claim](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(cancelTask.ID)+"/claims", nil, "claim-cancel-task", http.StatusCreated)
	humanOperator := trustedTestIdentity{ID: "operator", Kind: domain.ActorHuman}
	cancelled := requestDataAs[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(cancelTarget.ID)+"/cancellation", map[string]any{
		"reason": "This request was superseded.",
	}, "cancel-work-item", http.StatusOK, humanOperator)
	if cancelled.Status != domain.WorkItemStatusCancelled || cancelled.CancelledAt == nil || cancelled.CancelledBy == nil || cancelled.CancelledBy.ID != "operator" || cancelled.CancellationReason != "This request was superseded." {
		t.Fatalf("cancelled WorkItem = %+v", cancelled)
	}
	cancelledContext := requestDataAs[application.WorkItemExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/work-items/"+string(cancelTarget.ID)+"/context", nil, "", http.StatusOK, humanOperator)
	if cancelledContext.WorkItem.CancelledAt == nil || cancelledContext.WorkItem.CancelledBy == nil || cancelledContext.WorkItem.CancellationReason != cancelled.CancellationReason ||
		cancelledContext.ActiveClaims == nil || len(cancelledContext.ActiveClaims) != 0 || len(cancelledContext.Claims) != 1 || cancelledContext.Claims[0].EndReason != domain.ClaimEndWorkItemCancelled {
		t.Fatalf("persisted cancelled WorkItem context = %+v", cancelledContext)
	}
	requestErrorAs(t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(cancelTask.ID)+"/claims/"+string(cancelClaim.ID)+"/heartbeat", nil,
		"heartbeat-cancelled-task", http.StatusConflict, "work_item_cancelled", trustedTestIdentity{ID: "codex-storage", Role: "database"})

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
	if candidates[0].WorkItem.Tags == nil || candidates[0].Definition.SuggestedTags == nil ||
		candidates[0].Task.AllowedRoles == nil || candidates[0].Task.Tags == nil || candidates[0].Task.Reviews == nil ||
		candidates[0].Task.Submissions == nil || candidates[0].Task.Failures == nil || candidates[0].Task.TransitionDecisions == nil {
		t.Fatalf("candidate contains null collections: %#v", candidates[0])
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
	artifact := requestData[domain.Artifact](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/artifacts", map[string]any{
		"claim_id": claim.ID, "name": "commit", "uri": "https://example.test/repo/commit/abc123",
	}, "create-artifact", http.StatusCreated)
	if artifact.SubmissionID != nil || artifact.Name != "commit" {
		t.Fatalf("staged Artifact = %#v", artifact)
	}
	managedArtifact := uploadArtifact(t, client, server.URL+"/api/v1/tasks/"+string(task.ID)+"/artifact-uploads", claim.ID, "report", []byte("managed report"), "upload-artifact")
	if managedArtifact.URI[:len("kairos://")] != "kairos://" {
		t.Fatalf("managed Artifact URI = %q", managedArtifact.URI)
	}
	stagedArtifactPage := requestPage[domain.Artifact](t, client, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/artifacts?limit=1")
	if len(stagedArtifactPage.Data) != 0 || stagedArtifactPage.Data == nil || stagedArtifactPage.NextCursor != nil {
		t.Fatalf("staged Artifact page = %#v, want an empty terminal page", stagedArtifactPage)
	}
	executionContext = requestData[application.TaskExecutionContext](t, client, http.MethodGet, server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, "", http.StatusOK)
	if len(executionContext.Artifacts) != 2 || executionContext.Artifacts[0].ID != artifact.ID {
		t.Fatalf("task context Artifacts = %#v", executionContext.Artifacts)
	}

	submission := requestData[domain.TaskSubmission](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/submissions", map[string]any{
		"claim_id": claim.ID, "result": "Migration implemented and tested", "artifact_ids": []domain.ArtifactID{artifact.ID, managedArtifact.ID},
	}, "submit-task", http.StatusCreated)
	if submission.Result != "Migration implemented and tested" {
		t.Fatalf("submission result = %q", submission.Result)
	}
	firstArtifactPage := requestPage[domain.Artifact](t, client, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/artifacts?limit=1")
	if len(firstArtifactPage.Data) != 1 || firstArtifactPage.NextCursor == nil {
		t.Fatalf("first Artifact page = %#v", firstArtifactPage)
	}
	secondArtifactPage := requestPage[domain.Artifact](t, client, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/artifacts?limit=1&cursor="+*firstArtifactPage.NextCursor)
	if len(secondArtifactPage.Data) != 1 || secondArtifactPage.NextCursor != nil || secondArtifactPage.Data[0].ID == firstArtifactPage.Data[0].ID {
		t.Fatalf("second Artifact page = %#v after %#v", secondArtifactPage, firstArtifactPage)
	}
	taskDetail := requestData[application.TaskDetail](t, client, http.MethodGet,
		server.URL+"/api/v1/tasks/"+string(task.ID), nil, "", http.StatusOK)
	if len(taskDetail.Artifacts) != 2 || taskDetail.Artifacts[0].TaskID != task.ID || taskDetail.Artifacts[0].SubmissionID == nil {
		t.Fatalf("Task Detail Artifacts = %#v", taskDetail.Artifacts)
	}
	workItemContext := requestData[application.WorkItemExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/context", nil, "", http.StatusOK)
	if workItemContext.WorkItem.Status != domain.WorkItemStatusOpen || workItemContext.WorkItem.Result != "" {
		t.Fatalf("converged WorkItem context = %+v", workItemContext)
	}
	if len(workItemContext.Artifacts) != 2 || workItemContext.Artifacts[0].SubmissionID == nil || *workItemContext.Artifacts[0].SubmissionID != submission.ID {
		t.Fatalf("committed WorkItem Artifacts = %#v", workItemContext.Artifacts)
	}
	download := newTrustedRequest(t, http.MethodGet, server.URL+"/api/v1/artifacts/"+string(managedArtifact.ID)+"/content", nil, "", trustedTestIdentity{ID: "codex-storage", Role: "database"})
	downloadResponse, err := client.Do(download)
	if err != nil {
		t.Fatalf("download managed Artifact: %v", err)
	}
	downloaded, readErr := io.ReadAll(downloadResponse.Body)
	downloadResponse.Body.Close()
	if readErr != nil || downloadResponse.StatusCode != http.StatusOK || string(downloaded) != "managed report" {
		t.Fatalf("download managed Artifact: status=%d error=%v content=%q", downloadResponse.StatusCode, readErr, downloaded)
	}
	candidates = requestData[[]application.WorkCandidate](t, client, http.MethodGet, server.URL+"/api/v1/work?tag=backend", nil, "", http.StatusOK)
	if len(candidates) != 1 || candidates[0].Kind != application.WorkCandidateBlackboardCompletion || candidates[0].WorkItem.ID != workItem.ID {
		t.Fatalf("completion candidates = %+v", candidates)
	}
	completed := requestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/completion", map[string]any{
		"result": submission.Result,
	}, "submit-completion", http.StatusOK)
	if completed.Status != domain.WorkItemStatusCompleted || completed.Result != submission.Result {
		t.Fatalf("completed WorkItem = %+v", completed)
	}
	if len(workItemContext.Tasks) != 1 || workItemContext.Tasks[0].ID != task.ID || workItemContext.Relations == nil {
		t.Fatalf("completed WorkItem coordination context = %+v", workItemContext)
	}
	workItemContext = requestData[application.WorkItemExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/context", nil, "", http.StatusOK)
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

	agentAcceptanceWorkItem := requestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "engineering", "mode": "blackboard", "acceptance_mode": "agent",
		"title": "Confirm delivery", "goal": "Exercise explicit agent acceptance", "tags": []string{"acceptance"},
	}, "create-agent-acceptance-work-item", http.StatusCreated)
	submittedForAcceptance := requestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(agentAcceptanceWorkItem.ID)+"/completion", map[string]any{
		"result": "Delivery is ready for acceptance.",
	}, "submit-agent-acceptance", http.StatusOK)
	if submittedForAcceptance.Status != domain.WorkItemStatusAwaitingAgentAcceptance || submittedForAcceptance.Result != "Delivery is ready for acceptance." {
		t.Fatalf("submitted agent acceptance WorkItem = %+v", submittedForAcceptance)
	}
	candidates = requestData[[]application.WorkCandidate](t, client, http.MethodGet, server.URL+"/api/v1/work?tag=acceptance", nil, "", http.StatusOK)
	if len(candidates) != 1 || candidates[0].Kind != application.WorkCandidateWorkItemAcceptance || candidates[0].WorkItem.ID != agentAcceptanceWorkItem.ID {
		t.Fatalf("agent acceptance candidates = %+v", candidates)
	}
	accepted := requestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(agentAcceptanceWorkItem.ID)+"/acceptance", nil, "accept-agent-completion", http.StatusOK)
	if accepted.Status != domain.WorkItemStatusCompleted || accepted.Result != submittedForAcceptance.Result {
		t.Fatalf("accepted WorkItem = %+v", accepted)
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
	if detail.Task.ID != candidates[0].Task.ID || detail.Task.AllowedRoles == nil || detail.Task.Tags == nil || detail.Task.Reviews == nil || detail.Task.Submissions == nil || detail.Task.Failures == nil || detail.Task.TransitionDecisions == nil || detail.History.Claims == nil || detail.History.Submissions == nil || detail.History.Reviews == nil || detail.History.Failures == nil || detail.History.TransitionDecisions == nil || detail.Artifacts == nil {
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

func TestHTTPAPIRejectsWorkflowWithoutValidGraph(t *testing.T) {
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

	body := bytes.NewBufferString(`{"name":"Invalid","graph":{}}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/definitions/workflows/invalid/versions", body)
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

	requestDataAs[domain.BlackboardDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/blackboards/identity-test/versions", map[string]any{
		"name": "Identity test",
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
	requestDataAs[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/completion", map[string]any{
		"result": "Reviewed work completed.",
	}, "owner-completion", http.StatusOK, owner)

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

func uploadArtifact(t *testing.T, client *http.Client, endpoint string, claimID domain.ClaimID, name string, content []byte, operationID string) domain.Artifact {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("claim_id", string(claimID)); err != nil {
		t.Fatalf("write upload claim: %v", err)
	}
	if err := writer.WriteField("name", name); err != nil {
		t.Fatalf("write upload name: %v", err)
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create upload file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write upload file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close upload body: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		t.Fatalf("create upload request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Idempotency-Key", operationID)
	request.Header.Set(identity.HeaderActorID, "codex-storage")
	request.Header.Set(identity.HeaderActorRole, "database")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("upload Artifact: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		responseContent, _ := io.ReadAll(response.Body)
		t.Fatalf("upload Artifact status = %d: %s", response.StatusCode, responseContent)
	}
	var envelope struct {
		Data domain.Artifact `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode uploaded Artifact: %v", err)
	}
	return envelope.Data
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

type testPage[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

func requestPage[T any](t *testing.T, client *http.Client, url string) testPage[T] {
	t.Helper()
	request := newTrustedRequest(t, http.MethodGet, url, nil, "", trustedTestIdentity{ID: "codex-storage", Role: "database"})
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send paginated request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		content, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s status = %d: %s", url, response.StatusCode, content)
	}
	var page testPage[T]
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode paginated response: %v", err)
	}
	return page
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
