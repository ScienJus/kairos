package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
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
	identityService, err := identity.NewService(repo, endToEndClock{}, identity.SecureTokenGenerator{})
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	handler, err := httpapi.NewWithIdentityManagement(
		applicationService,
		identity.AuthenticatedResolver{Authenticator: identityService},
		identityService,
		authenticatedTestAdminToken,
	)
	if err != nil {
		t.Fatalf("new authenticated HTTP API: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()

	agent := authenticatedRequestData[issuedTokenPayload](t, client, http.MethodPost, server.URL+"/api/v1/identities", map[string]any{
		"id": "codex-database", "kind": "agent", "role": "database",
	}, authenticatedTestAdminToken, http.StatusCreated)
	human := authenticatedRequestData[issuedTokenPayload](t, client, http.MethodPost, server.URL+"/api/v1/identities", map[string]any{
		"id": "reviewer", "kind": "human", "role": "",
	}, authenticatedTestAdminToken, http.StatusCreated)
	if agent.Token == "" || human.Token == "" || agent.Role != "database" {
		t.Fatalf("issued identities = %+v and %+v", agent, human)
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

	authenticatedRequestData[domain.BlackboardDefinition](t, client, http.MethodPost, server.URL+"/api/v1/definitions/blackboards", map[string]any{
		"id": "authenticated", "version": 1, "name": "Authenticated", "status": "published",
	}, agent.Token, http.StatusCreated)
	workItem := authenticatedRequestData[domain.WorkItem](t, client, http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "authenticated", "definition_version": 1, "mode": "blackboard",
		"title": "Authenticated execution", "goal": "Verify bearer authentication",
	}, agent.Token, http.StatusCreated)
	task := authenticatedRequestData[domain.Task](t, client, http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/tasks", map[string]any{
		"title": "Protected task", "executor": "agent", "allowed_roles": []string{"database"},
	}, agent.Token, http.StatusCreated)

	claim := authenticatedRequestData[domain.Claim](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/claims", nil, agent.Token, http.StatusCreated)
	if claim.Executor.ID != "codex-database" || claim.Executor.Kind != domain.ActorAgent {
		t.Fatalf("claim executor = %+v, trusted spoofing headers should be ignored", claim.Executor)
	}

	rotated := authenticatedRequestData[issuedTokenPayload](t, client, http.MethodPost,
		server.URL+"/api/v1/identities/agent/codex-database/token", nil, authenticatedTestAdminToken, http.StatusOK)
	if rotated.Token == "" || rotated.Token == agent.Token {
		t.Fatalf("rotated token was not replaced")
	}
	requestAuthenticatedError(t, client, http.MethodGet, server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil,
		agent.Token, http.StatusUnauthorized, "unauthenticated")
	authenticatedRequestData[application.TaskExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, rotated.Token, http.StatusOK)

	authenticatedRequestData[domain.TaskSubmission](t, client, http.MethodPost, server.URL+"/api/v1/tasks/"+string(task.ID)+"/submissions", map[string]any{
		"claim_id": claim.ID, "result": "Authenticated result", "request_review": true,
	}, rotated.Token, http.StatusCreated)
	executionContext := authenticatedRequestData[application.TaskExecutionContext](t, client, http.MethodGet,
		server.URL+"/api/v1/tasks/"+string(task.ID)+"/context", nil, rotated.Token, http.StatusOK)
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

type issuedTokenPayload struct {
	ID    domain.ActorID   `json:"id"`
	Kind  domain.ActorKind `json:"kind"`
	Role  string           `json:"role"`
	Token string           `json:"token"`
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
