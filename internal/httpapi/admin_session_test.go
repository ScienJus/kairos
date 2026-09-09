package httpapi_test

import (
	"context"
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

func TestAdminTokenIsAnOrdinaryHumanSession(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	app, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatal(err)
	}
	token, err := (identity.SecureTokenGenerator{}).NewToken()
	if err != nil {
		t.Fatal(err)
	}
	start := func(token string) *httptest.Server {
		ids, err := identity.NewService(repo, endToEndClock{}, identity.SecureTokenGenerator{})
		if err != nil {
			t.Fatal(err)
		}
		if err := ids.ConfigureAdmin(ctx, token); err != nil {
			t.Fatal(err)
		}
		h, err := httpapi.NewWithIdentityManagement(app, identity.AuthenticatedResolver{Authenticator: ids}, ids, token, httpapi.Options{AuthenticationMode: httpapi.AuthenticationModeAuthenticated})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(h)
		t.Cleanup(server.Close)
		return server
	}
	server := start(token)
	client := server.Client()
	base := server.URL + "/api/v1"
	session := authenticatedRequestData[sessionPayload](t, client, http.MethodGet, base+"/session", nil, token, 200)
	if session.Kind != domain.ActorHuman || session.Role != "" || session.ID == "" || session.ID == "spoofed-actor" || session.DisplayName != "system admin" {
		t.Fatal("Admin session is not the dedicated Human")
	}
	actor := domain.ActorRef{Kind: domain.ActorHuman, ID: session.ID}
	human := authenticatedRequestData[issuedTokenPayload](t, client, "POST", base+"/identities", map[string]any{"kind": "human", "id": "admin-ordinary-human"}, token, 201)
	agent := authenticatedRequestData[issuedTokenPayload](t, client, "POST", base+"/identities", map[string]any{"kind": "agent", "id": "ordinary-agent", "role": "developer"}, token, 201)
	for _, credential := range []string{human.Token, agent.Token} {
		ordinary := authenticatedRequestData[map[string]any](t, client, "GET", base+"/session", nil, credential, 200)
		if _, exists := ordinary["display_name"]; exists {
			t.Fatal("ordinary identity received Admin presentation metadata")
		}
	}
	for _, credential := range []string{human.Token, agent.Token, testExecutorToken(7), "", "invalid"} {
		requestAuthenticatedError(t, client, "GET", base+"/identities", nil, credential, 401, "unauthenticated")
	}
	record := authenticatedRequestData[struct {
		CredentialSource string `json:"credential_source"`
		TokenActive      bool   `json:"token_active"`
	}](t, client, "GET", base+"/identities/human/"+string(session.ID), nil, token, 200)
	if record.CredentialSource != "admin" || record.TokenActive {
		t.Fatal("incorrect Admin credential metadata")
	}
	for _, method := range []string{"POST", "DELETE"} {
		requestAuthenticatedError(t, client, method, base+"/identities/human/"+string(session.ID)+"/token", nil, token, 403, "forbidden")
	}
	authenticatedRequestData[domain.BlackboardDefinition](t, client, "POST", base+"/definitions/blackboards/admin-session/versions", map[string]any{"name": "Admin session"}, token, 201)
	work := authenticatedRequestData[domain.WorkItem](t, client, "POST", base+"/work-items", map[string]any{"definition_id": "admin-session", "mode": "blackboard", "title": "Human work", "goal": "Exercise Human authorization"}, token, 201)
	task := authenticatedRequestData[domain.Task](t, client, "POST", base+"/work-items/"+string(work.ID)+"/tasks", map[string]any{"title": "Human task", "executor": "human"}, token, 201)
	claim := authenticatedRequestData[domain.Claim](t, client, "POST", base+"/tasks/"+string(task.ID)+"/claims", nil, token, 201)
	if claim.Executor != actor {
		t.Fatal("Claim attributed to another actor")
	}
	requestAuthenticatedError(t, client, "POST", base+"/tasks/"+string(task.ID)+"/submissions", map[string]any{"claim_id": claim.ID, "result": "Other actor"}, human.Token, 403, "forbidden")
	sub := authenticatedRequestData[domain.TaskSubmission](t, client, "POST", base+"/tasks/"+string(task.ID)+"/submissions", map[string]any{"claim_id": claim.ID, "result": "Completed"}, token, 201)
	if sub.ClaimID != claim.ID {
		t.Fatal("Submission lost Claim attribution")
	}
	requestAuthenticatedError(t, client, "DELETE", base+"/tasks/"+string(task.ID)+"/claims/"+string(claim.ID), nil, token, 409, "conflict")
	agentTask := authenticatedRequestData[domain.Task](t, client, "POST", base+"/work-items/"+string(work.ID)+"/tasks", map[string]any{"title": "Agent task", "executor": "agent", "allowed_roles": []string{"developer"}}, token, 201)
	for _, credential := range []string{token, human.Token} {
		requestAuthenticatedError(t, client, "POST", base+"/tasks/"+string(agentTask.ID)+"/claims", nil, credential, 403, "forbidden")
	}
	otherTask := authenticatedRequestData[domain.Task](t, client, "POST", base+"/work-items/"+string(work.ID)+"/tasks", map[string]any{"title": "Other Human task", "executor": "human"}, token, 201)
	otherClaim := authenticatedRequestData[domain.Claim](t, client, "POST", base+"/tasks/"+string(otherTask.ID)+"/claims", nil, human.Token, 201)
	requestAuthenticatedError(t, client, "POST", base+"/tasks/"+string(otherTask.ID)+"/submissions", map[string]any{"claim_id": otherClaim.ID, "result": "Admin cannot impersonate"}, token, 403, "forbidden")
	server.Close()
	rotated, err := (identity.SecureTokenGenerator{}).NewToken()
	if err != nil {
		t.Fatal(err)
	}
	server = start(rotated)
	base = server.URL + "/api/v1"
	got := authenticatedRequestData[sessionPayload](t, client, "GET", base+"/session", nil, rotated, 200)
	if got != session {
		t.Fatal("restart/rotation changed actor")
	}
	for _, path := range []string{"/session", "/work-items", "/identities"} {
		requestAuthenticatedError(t, client, "GET", base+path, nil, token, 401, "unauthenticated")
	}
	for _, credential := range []string{human.Token, agent.Token} {
		authenticatedRequestData[sessionPayload](t, client, "GET", base+"/session", nil, credential, 200)
	}
}
