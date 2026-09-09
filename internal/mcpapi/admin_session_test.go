package mcpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAdminMCPUsesOrdinaryHumanAndRejectsRotatedCredential(t *testing.T) {
	ctx := context.Background()
	app, repo := newMCPFixture(t)
	token, err := (identity.SecureTokenGenerator{}).NewToken()
	if err != nil {
		t.Fatal(err)
	}
	ids, err := identity.NewService(repo, mcpClock{}, identity.SecureTokenGenerator{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ids.ConfigureAdmin(ctx, token); err != nil {
		t.Fatal(err)
	}
	actor, err := ids.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	work, err := app.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Identity: actor, Definition: domain.DefinitionBinding{ID: "mcp-blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Title: "Admin MCP", Goal: "Human attribution", AcceptanceMode: domain.WorkItemAcceptanceNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(app, identity.AuthenticatedResolver{Authenticator: ids})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	session := connectMCP(t, ctx, server.URL, http.Header{"Authorization": {"Bearer " + token}, identity.HeaderActorID: {"spoofed"}, identity.HeaderActorKind: {"agent"}, identity.HeaderActorRole: {"admin"}})
	defer session.Close()
	task := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{WorkItemID: string(work.ID), OperationID: "human-plan", Title: "Human task", Executor: "human"})
	claim := callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{TaskID: task.Task.ID, OperationID: "human-claim"})
	if claim.Claim.Executor.Kind != "human" || claim.Claim.Executor.ID != string(actor.Actor.ID) || actor.Role != "" || actor.Executor != nil {
		t.Fatal("MCP did not preserve ordinary Human identity")
	}
	callTool[any](t, ctx, session, "submit_task", submitTaskInput{TaskID: task.Task.ID, ClaimID: claim.Claim.ID, Result: "Human finished"})
	agentTask := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{WorkItemID: string(work.ID), OperationID: "agent-plan", Title: "Agent task", Executor: "agent", AllowedRoles: []string{"worker"}})
	for _, params := range []*mcp.CallToolParams{
		{Name: "claim_task", Arguments: claimTaskInput{TaskID: agentTask.Task.ID, OperationID: "denied"}},
	} {
		result, err := session.CallTool(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError || len(result.Content) == 0 {
			t.Fatalf("Admin bypassed Human restriction for %s", params.Name)
		}
		message, ok := result.Content[0].(*mcp.TextContent)
		if !ok || !strings.Contains(message.Text, "forbidden") {
			t.Fatalf("expected forbidden for %s", params.Name)
		}
	}
	// Discovery must match an ordinary Human, including Human lifecycle candidates.
	workCandidates := callTool[findWorkOutput](t, ctx, session, "find_work", findWorkInput{})
	ordinary, err := ids.CreateIdentity(ctx, domain.ActorRef{Kind: domain.ActorHuman, ID: "ordinary-mcp-human"}, "")
	if err != nil {
		t.Fatal(err)
	}
	ordinarySession := connectMCP(t, ctx, server.URL, http.Header{"Authorization": {"Bearer " + ordinary.Token}})
	defer ordinarySession.Close()
	ordinaryCandidates := callTool[findWorkOutput](t, ctx, ordinarySession, "find_work", findWorkInput{})
	if !reflect.DeepEqual(workCandidates, ordinaryCandidates) {
		t.Fatal("Admin discovery differs from ordinary Human")
	}
	rotated, err := (identity.SecureTokenGenerator{}).NewToken()
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := identity.NewService(repo, mcpClock{}, identity.SecureTokenGenerator{})
	if err := fresh.ConfigureAdmin(ctx, rotated); err != nil {
		t.Fatal(err)
	}
	next, err := New(app, identity.AuthenticatedResolver{Authenticator: fresh})
	if err != nil {
		t.Fatal(err)
	}
	replacement := httptest.NewServer(next)
	defer replacement.Close()
	for _, credential := range []string{token, "invalid", ""} {
		req, _ := http.NewRequestWithContext(ctx, "POST", replacement.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "application/json")
		resp, err := replacement.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatal("old or invalid MCP credential accepted")
		}
	}
	renewed := connectMCP(t, ctx, replacement.URL, http.Header{"Authorization": {"Bearer " + rotated}})
	defer renewed.Close()
	callTool[taskContextOutput](t, ctx, renewed, "get_task_context", taskContextInput{TaskID: task.Task.ID})
	got, err := fresh.Authenticate(ctx, rotated)
	if err != nil || got != actor {
		t.Fatal("rotation changed MCP identity")
	}
}
