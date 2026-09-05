package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/artifactstore"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
)

const authenticatedTestAdminToken = "test-admin-token-with-at-least-32-characters"

func TestAuthenticatedHTTPModeEndToEnd(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "authenticated.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	applicationService, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	localArtifacts, err := artifactstore.NewLocal(privateArtifactDir(t))
	if err != nil {
		t.Fatalf("new local Artifact Store: %v", err)
	}
	if err := applicationService.ConfigureArtifactStore(localArtifacts); err != nil {
		t.Fatalf("configure Artifact Store: %v", err)
	}
	identityService, err := identity.NewService(repo, endToEndClock{}, identity.SecureTokenGenerator{})
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	handler, err := httpapi.NewWithIdentityManagement(
		applicationService,
		identity.AuthenticatedResolver{Authenticator: identityService},
		identityService,
		authenticatedTestAdminToken,
		httpapi.Options{AuthenticationMode: httpapi.AuthenticationModeAuthenticated},
	)
	if err != nil {
		t.Fatalf("new authenticated HTTP API: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	config := authenticatedRequestData[authenticationConfigPayload](t, client, http.MethodGet, server.URL+"/api/v1/auth/config", nil, "", http.StatusOK)
	if config.Mode != "authenticated" {
		t.Fatalf("authentication mode = %q, want authenticated", config.Mode)
	}
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/session", nil, "", http.StatusUnauthorized, "unauthenticated")

	agent := authenticatedRequestData[issuedTokenPayload](t, client, http.MethodPost, server.URL+"/api/v1/identities", map[string]any{
		"id": "codex-database", "kind": "agent", "role": "database",
	}, authenticatedTestAdminToken, http.StatusCreated)
	human := authenticatedRequestData[issuedTokenPayload](t, client, http.MethodPost, server.URL+"/api/v1/identities", map[string]any{
		"id": "reviewer", "kind": "human", "role": "",
	}, authenticatedTestAdminToken, http.StatusCreated)
	if agent.Token == "" || human.Token == "" || agent.Role != "database" {
		t.Fatalf("issued identities = %+v and %+v", agent, human)
	}
	session := authenticatedRequestData[sessionPayload](t, client, http.MethodGet, server.URL+"/api/v1/session", nil, human.Token, http.StatusOK)
	if session.ID != human.ID || session.Kind != domain.ActorHuman || session.Role != "" {
		t.Fatalf("authenticated session = %+v, want human identity %+v", session, human)
	}
	requestAuthenticatedError(t, client, http.MethodPost, server.URL+"/api/v1/identities", map[string]any{
		"id": "codex-database", "kind": "agent", "role": "database",
	}, authenticatedTestAdminToken, http.StatusConflict, "conflict")
	requestAuthenticatedError(t, client, http.MethodPost, server.URL+"/api/v1/identities", map[string]any{
		"id": "invalid-human", "kind": "human", "role": "reviewer",
	}, authenticatedTestAdminToken, http.StatusBadRequest, "invalid_request")
	stored, err := repo.GetIdentity(ctx, domain.ActorRef{Kind: domain.ActorAgent, ID: "codex-database"})
	if err != nil {
		t.Fatalf("get stored identity: %v", err)
	}
	if stored.TokenHash == agent.Token || len(stored.TokenHash) != 64 {
		t.Fatalf("stored credential is not a SHA-256 hash")
	}

	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/work", nil, "", http.StatusUnauthorized, "unauthenticated")
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/identities", nil, "wrong-admin-token", http.StatusUnauthorized, "unauthenticated")

	authenticatedRequestData[domain.BlackboardDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/blackboards/authenticated/versions", map[string]any{
		"name": "Authenticated",
	}, agent.Token, http.StatusCreated)
	workItem := authenticatedRequestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "authenticated", "mode": "blackboard",
		"title": "Authenticated execution", "goal": "Verify bearer authentication",
	}, agent.Token, http.StatusCreated)
	coordExecutorToken := testExecutorToken(1)
	planningClaim := authenticatedRequestData[domain.CoordinationClaim](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/coordination-claims", map[string]any{
		"kind": "empty_blackboard", "executor_token": coordExecutorToken,
	}, agent.Token, http.StatusCreated)
	authenticatedRequestData[application.WorkItemExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/context", nil, coordExecutorToken, http.StatusOK)
	requestAuthenticatedError(t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "authenticated", "mode": "blackboard", "title": "Denied", "goal": "Denied",
	}, coordExecutorToken, http.StatusForbidden, "forbidden")
	task := authenticatedRequestData[domain.Task](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/tasks", map[string]any{
		"coordination_claim_id": planningClaim.ID, "title": "Protected task", "executor": "agent", "allowed_roles": []string{"database"},
	}, agent.Token, http.StatusCreated)
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/context", nil,
		coordExecutorToken, http.StatusUnauthorized, "unauthenticated")

	taskExecutorToken := testExecutorToken(2)
	requestAuthenticatedError(t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/claims", map[string]any{
		"executor_token": identity.ExecutorTokenPrefix + "invalid",
	}, agent.Token, http.StatusBadRequest, "invalid_request")
	claim := authenticatedRequestData[domain.Claim](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/claims", map[string]any{
		"executor_token": taskExecutorToken,
	}, agent.Token, http.StatusCreated)
	if claim.Executor.ID != "codex-database" || claim.Executor.Kind != domain.ActorAgent {
		t.Fatalf("claim executor = %+v, trusted spoofing headers should be ignored", claim.Executor)
	}
	authenticatedRequestData[application.TaskExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, taskExecutorToken, http.StatusOK)
	for _, path := range []string{"/api/v1/session", "/api/v1/tasks/" + string(task.ID), "/api/v1/work", "/api/v1/work-items", "/api/v1/definitions/workflows"} {
		requestAuthenticatedError(t, client, http.MethodGet, server.URL+path, nil, taskExecutorToken, http.StatusForbidden, "forbidden")
	}
	otherWorkItem := authenticatedRequestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "authenticated", "mode": "blackboard", "title": "Other scope", "goal": "Remain isolated",
	}, agent.Token, http.StatusCreated)
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/work-items/"+string(otherWorkItem.ID)+"/context", nil,
		taskExecutorToken, http.StatusForbidden, "forbidden")
	requestAuthenticatedError(t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/submissions", map[string]any{
		"claim_id": claim.ID, "result": "Denied direct lifecycle mutation",
	}, taskExecutorToken, http.StatusForbidden, "forbidden")
	externalArtifact := authenticatedRequestData[domain.Artifact](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/artifacts", map[string]any{
		"claim_id": claim.ID, "name": "executor-note", "uri": "https://example.com/executor-note",
	}, taskExecutorToken, http.StatusCreated)
	managedArtifact := uploadAuthenticatedArtifact(t, client, server.URL+"/api/v1/tasks/"+string(task.ID)+"/artifact-uploads",
		claim.ID, "executor-report", []byte("executor managed report"), "executor-upload-1", taskExecutorToken)
	nextTask := authenticatedRequestData[domain.Task](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/tasks", map[string]any{
		"title": "Executor planned task", "executor": "agent", "allowed_roles": []string{"database"},
	}, taskExecutorToken, http.StatusCreated)

	rotated := authenticatedRequestData[issuedTokenPayload](t, client, http.MethodPost,
		server.URL+"/api/v1/identities/agent/codex-database/token", nil, authenticatedTestAdminToken, http.StatusOK)
	if rotated.Token == "" || rotated.Token == agent.Token {
		t.Fatalf("rotated token was not replaced")
	}
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil,
		agent.Token, http.StatusUnauthorized, "unauthenticated")
	authenticatedRequestData[application.TaskExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, rotated.Token, http.StatusOK)
	authenticatedRequestData[application.TaskExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, taskExecutorToken, http.StatusOK)
	authenticatedRequestNoContent(t, client, http.MethodDelete,
		server.URL+"/api/v1/identities/agent/codex-database/token", authenticatedTestAdminToken, http.StatusNoContent)
	authenticatedRequestData[application.TaskExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, taskExecutorToken, http.StatusOK)
	resumed := authenticatedRequestData[issuedTokenPayload](t, client, http.MethodPost,
		server.URL+"/api/v1/identities/agent/codex-database/token", nil, authenticatedTestAdminToken, http.StatusOK)

	authenticatedRequestData[domain.TaskSubmission](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/submissions", map[string]any{
		"claim_id": claim.ID, "result": "Authenticated result", "request_review": true,
		"artifact_ids": []domain.ArtifactID{externalArtifact.ID, managedArtifact.ID},
	}, resumed.Token, http.StatusCreated)
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil,
		taskExecutorToken, http.StatusUnauthorized, "unauthenticated")
	executionContext := authenticatedRequestData[application.TaskExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, resumed.Token, http.StatusOK)
	if len(executionContext.Task.Reviews) != 1 {
		t.Fatalf("reviews = %+v, want one", executionContext.Task.Reviews)
	}
	reviewID := executionContext.Task.Reviews[0].ID
	review := authenticatedRequestData[domain.Review](t, client, http.MethodPost,
		server.URL+"/api/v1/tasks/"+string(task.ID)+"/reviews/"+string(reviewID)+"/decision",
		map[string]any{"decision": "approved"}, human.Token, http.StatusOK)
	if review.Status != domain.ReviewStatusApproved {
		t.Fatalf("review status = %q", review.Status)
	}

	nextExecutorToken := testExecutorToken(3)
	nextClaim := authenticatedRequestData[domain.Claim](t, client, http.MethodPost,
		server.URL+"/api/v1/tasks/"+string(nextTask.ID)+"/claims", map[string]any{"executor_token": nextExecutorToken}, resumed.Token, http.StatusCreated)
	artifacts := authenticatedRequestData[[]domain.Artifact](t, client, http.MethodGet,
		server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/artifacts?limit=50", nil, nextExecutorToken, http.StatusOK)
	if len(artifacts) != 2 {
		t.Fatalf("executor submitted artifacts = %+v, want two", artifacts)
	}
	contentRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/artifacts/"+string(managedArtifact.ID)+"/content", nil)
	if err != nil {
		t.Fatalf("new executor Artifact content request: %v", err)
	}
	contentRequest.Header.Set("Authorization", "Bearer "+nextExecutorToken)
	contentResponse, err := client.Do(contentRequest)
	if err != nil {
		t.Fatalf("read executor Artifact content: %v", err)
	}
	content, readErr := io.ReadAll(contentResponse.Body)
	_ = contentResponse.Body.Close()
	if readErr != nil || contentResponse.StatusCode != http.StatusOK || string(content) != "executor managed report" {
		t.Fatalf("executor Artifact content status=%d content=%q err=%v", contentResponse.StatusCode, content, readErr)
	}
	authenticatedRequestNoContent(t, client, http.MethodDelete,
		server.URL+"/api/v1/tasks/"+string(nextTask.ID)+"/claims/"+string(nextClaim.ID), resumed.Token, http.StatusNoContent)
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/context", nil,
		nextExecutorToken, http.StatusUnauthorized, "unauthenticated")

	authenticatedRequestNoContent(t, client, http.MethodDelete,
		server.URL+"/api/v1/identities/agent/codex-database/token", authenticatedTestAdminToken, http.StatusNoContent)
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/work", nil,
		rotated.Token, http.StatusUnauthorized, "unauthenticated")

	records := authenticatedRequestData[[]identityRecordPayload](t, client, http.MethodGet,
		server.URL+"/api/v1/identities", nil, authenticatedTestAdminToken, http.StatusOK)
	if len(records) != 2 {
		t.Fatalf("identity records = %+v, want two", records)
	}
	for _, record := range records {
		if record.ID == "codex-database" && record.TokenActive {
			t.Fatal("revoked agent identity still reports an active token")
		}
	}
}

func uploadAuthenticatedArtifact(
	t *testing.T,
	client *http.Client,
	endpoint string,
	claimID domain.ClaimID,
	name string,
	content []byte,
	operationID string,
	token string,
) domain.Artifact {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("claim_id", string(claimID)); err != nil {
		t.Fatalf("write authenticated upload claim: %v", err)
	}
	if err := writer.WriteField("name", name); err != nil {
		t.Fatalf("write authenticated upload name: %v", err)
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create authenticated upload file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write authenticated upload file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close authenticated upload body: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		t.Fatalf("create authenticated upload request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Idempotency-Key", operationID)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("upload authenticated Artifact: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		responseContent, _ := io.ReadAll(response.Body)
		t.Fatalf("upload authenticated Artifact status = %d: %s", response.StatusCode, responseContent)
	}
	var envelope struct {
		Data domain.Artifact `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode authenticated uploaded Artifact: %v", err)
	}
	return envelope.Data
}

func testExecutorToken(seed byte) string {
	return identity.ExecutorTokenPrefix + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

type issuedTokenPayload struct {
	ID    domain.ActorID   `json:"id"`
	Kind  domain.ActorKind `json:"kind"`
	Role  string           `json:"role"`
	Token string           `json:"token"`
}

type authenticationConfigPayload struct {
	Mode httpapi.AuthenticationMode `json:"mode"`
}

type sessionPayload struct {
	ID   domain.ActorID   `json:"id"`
	Kind domain.ActorKind `json:"kind"`
	Role string           `json:"role"`
}

type identityRecordPayload struct {
	ID          domain.ActorID `json:"id"`
	TokenActive bool           `json:"token_active"`
}

func authenticatedRequestData[T any](
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body any,
	token string,
	wantStatus int,
) T {
	t.Helper()
	request := newAuthenticatedRequest(t, method, url, body, token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send authenticated request: %v", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read authenticated response: %v", err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", method, url, response.StatusCode, wantStatus, content)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		t.Fatalf("decode authenticated response %s: %v", content, err)
	}
	return envelope.Data
}

func authenticatedRequestNoContent(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	token string,
	wantStatus int,
) {
	t.Helper()
	response, err := client.Do(newAuthenticatedRequest(t, method, url, nil, token))
	if err != nil {
		t.Fatalf("send authenticated request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		content, _ := io.ReadAll(response.Body)
		t.Fatalf("%s %s status = %d, want %d: %s", method, url, response.StatusCode, wantStatus, content)
	}
}

func requestAuthenticatedError(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body any,
	token string,
	wantStatus int,
	wantCode string,
) {
	t.Helper()
	response, err := client.Do(newAuthenticatedRequest(t, method, url, body, token))
	if err != nil {
		t.Fatalf("send authenticated request: %v", err)
	}
	defer response.Body.Close()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode authenticated error: %v", err)
	}
	if response.StatusCode != wantStatus || envelope.Error.Code != wantCode {
		t.Fatalf("status/code = %d/%q, want %d/%q", response.StatusCode, envelope.Error.Code, wantStatus, wantCode)
	}
}

func newAuthenticatedRequest(t *testing.T, method, url string, body any, token string) *http.Request {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode authenticated request: %v", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, requestBody)
	if err != nil {
		t.Fatalf("new authenticated request: %v", err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	// Authenticated Mode must ignore all Trusted Mode identity declarations.
	request.Header.Set(identity.HeaderActorID, "spoofed-actor")
	request.Header.Set(identity.HeaderActorKind, string(domain.ActorHuman))
	return request
}
