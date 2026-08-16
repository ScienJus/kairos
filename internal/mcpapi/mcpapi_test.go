package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTrustedMCPBlackboardLifecycle(t *testing.T) {
	ctx := context.Background()
	service, _ := newMCPFixture(t)
	handler, err := New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatalf("new MCP handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	headers := http.Header{
		identity.HeaderActorID:   {"codex-backend"},
		identity.HeaderActorKind: {"agent"},
		identity.HeaderActorRole: {"backend"},
	}
	session := connectMCP(t, ctx, server.URL, headers)
	t.Cleanup(func() { _ = session.Close() })

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	wantTools := []string{
		"claim_task", "create_blackboard_task", "fail_task", "find_work",
		"get_task_context", "release_claim", "submit_task",
	}
	gotTools := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		gotTools = append(gotTools, tool.Name)
	}
	slices.Sort(gotTools)
	if !slices.Equal(gotTools, wantTools) {
		t.Fatalf("tools = %v, want %v", gotTools, wantTools)
	}

	find := callTool[workCandidatesOutput](t, ctx, session, "find_work", findWorkInput{})
	if len(find.Data) != 1 || find.Data[0].Kind != application.WorkCandidateEmptyBlackboard {
		t.Fatalf("initial candidates = %+v, want one empty blackboard", find.Data)
	}
	workItemID := find.Data[0].WorkItem.ID

	created := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{
		WorkItemID: string(workItemID), OperationID: "plan-task-1",
		Title: "Implement MCP lifecycle", Description: "Exercise the agent execution tools.",
		AcceptanceCriteria: "Task can be claimed and submitted through MCP.",
		Executor:           "agent", AllowedRoles: []string{"backend"}, Tags: []string{"mcp"},
	})
	if created.Data.ID == "" || created.Data.WorkItemID != workItemID {
		t.Fatalf("created task = %+v", created.Data)
	}
	retryTask := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{
		WorkItemID: string(workItemID), OperationID: "plan-task-2",
		Title: "Exercise failure recovery", Executor: "agent", AllowedRoles: []string{"backend"}, Tags: []string{"mcp"},
	})

	find = callTool[workCandidatesOutput](t, ctx, session, "find_work", findWorkInput{Tags: []string{"mcp"}})
	if len(find.Data) != 2 || find.Data[0].Task.ID != created.Data.ID || find.Data[1].Task.ID != retryTask.Data.ID {
		t.Fatalf("planned candidates = %+v, want tasks %q and %q", find.Data, created.Data.ID, retryTask.Data.ID)
	}

	claimed := callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: string(created.Data.ID), OperationID: "claim-task-1",
	})
	if claimed.Data.Executor.ID != "codex-backend" {
		t.Fatalf("claim executor = %+v", claimed.Data.Executor)
	}

	taskContext := callTool[taskContextOutput](t, ctx, session, "get_task_context", taskContextInput{
		TaskID: string(created.Data.ID),
	})
	if taskContext.Data.Task.Status != domain.TaskStatusWorking || taskContext.Data.Blackboard == nil {
		t.Fatalf("task context = %+v", taskContext.Data)
	}
	released := callTool[releasedOutput](t, ctx, session, "release_claim", releaseClaimInput{
		TaskID: string(created.Data.ID), ClaimID: string(claimed.Data.ID),
		OperationID: "release-task-1", Reason: "Verify claim release before completing the task.",
	})
	if !released.Released {
		t.Fatal("release_claim did not acknowledge release")
	}
	claimed = callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: string(created.Data.ID), OperationID: "claim-task-2",
	})

	submitted := callTool[submissionOutput](t, ctx, session, "submit_task", submitTaskInput{
		TaskID: string(created.Data.ID), ClaimID: string(claimed.Data.ID),
		OperationID: "submit-task-1", Result: "MCP lifecycle verified end to end.",
	})
	if submitted.Data.TaskID != created.Data.ID {
		t.Fatalf("submission = %+v", submitted.Data)
	}

	retryClaim := callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: string(retryTask.Data.ID), OperationID: "claim-retry-task-1",
	})
	failure := callTool[failureOutput](t, ctx, session, "fail_task", failTaskInput{
		TaskID: string(retryTask.Data.ID), ClaimID: string(retryClaim.Data.ID),
		OperationID: "fail-retry-task-1", Action: "reopen",
		Reason: "First attempt found missing context.", RetryPrompt: "Use the complete Blackboard context on retry.",
	})
	if failure.Data.Action != domain.TaskFailureReopen {
		t.Fatalf("failure = %+v", failure.Data)
	}
	retryClaim = callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: string(retryTask.Data.ID), OperationID: "claim-retry-task-2",
	})
	callTool[submissionOutput](t, ctx, session, "submit_task", submitTaskInput{
		TaskID: string(retryTask.Data.ID), ClaimID: string(retryClaim.Data.ID),
		OperationID: "submit-retry-task-1", Result: "Failure recovery verified.",
	})

	taskContext = callTool[taskContextOutput](t, ctx, session, "get_task_context", taskContextInput{
		TaskID: string(retryTask.Data.ID),
	})
	if taskContext.Data.Task.Status != domain.TaskStatusCompleted || taskContext.Data.WorkItem.Status != domain.WorkItemStatusCompleted || len(taskContext.Data.Task.Failures) != 1 {
		t.Fatalf("completed context = %+v", taskContext.Data)
	}
}

func TestAuthenticatedMCPRequiresBearerAndUsesManagedIdentity(t *testing.T) {
	ctx := context.Background()
	service, repo := newMCPFixture(t)
	identityService, err := identity.NewService(repo, mcpClock{}, identity.SecureTokenGenerator{})
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	issued, err := identityService.CreateIdentity(ctx, domain.ActorRef{Kind: domain.ActorAgent, ID: "authenticated-agent"}, "backend")
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	handler, err := New(service, identity.AuthenticatedResolver{Authenticator: identityService})
	if err != nil {
		t.Fatalf("new authenticated MCP handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`))
	if err != nil {
		t.Fatalf("new unauthenticated request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("send unauthenticated request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("unauthenticated status = %d, want 401: %s", response.StatusCode, body)
	}

	session := connectMCP(t, ctx, server.URL, http.Header{"Authorization": {"Bearer " + issued.Token}})
	t.Cleanup(func() { _ = session.Close() })
	find := callTool[workCandidatesOutput](t, ctx, session, "find_work", findWorkInput{})
	if len(find.Data) != 1 || find.Data[0].Kind != application.WorkCandidateEmptyBlackboard {
		t.Fatalf("authenticated candidates = %+v", find.Data)
	}

	created := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{
		WorkItemID: string(find.Data[0].WorkItem.ID), OperationID: "authenticated-plan-1",
		Title: "Authenticated task", Executor: "agent", AllowedRoles: []string{"backend"},
	})
	claimed := callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: string(created.Data.ID), OperationID: "authenticated-claim-1",
	})
	if claimed.Data.Executor.ID != "authenticated-agent" || claimed.Data.Executor.Kind != domain.ActorAgent {
		t.Fatalf("authenticated claim executor = %+v", claimed.Data.Executor)
	}
}

func newMCPFixture(t *testing.T) (*application.Service, *repository.SQLRepository) {
	t.Helper()
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	service, err := application.NewService(repo, mcpClock{}, &mcpIDs{})
	if err != nil {
		t.Fatalf("new application service: %v", err)
	}
	setup := identity.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "setup"}}
	definition, err := service.CreateBlackboardDefinition(ctx, application.CreateBlackboardDefinitionCommand{
		Identity: setup,
		Metadata: application.DefinitionMetadataCommand{
			ID: "mcp-blackboard", Version: 1, Name: "MCP Blackboard",
			AgentInstructions: "Plan missing tasks, then claim and complete executable work.",
			SuggestedTags:     []string{"mcp"}, Status: domain.DefinitionStatusPublished,
		},
	})
	if err != nil {
		t.Fatalf("create blackboard definition: %v", err)
	}
	_, err = service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: setup,
		Title: "MCP end-to-end", Goal: "Prove an agent can plan and execute through MCP.", Tags: []string{"mcp"},
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	return service, repo
}

func connectMCP(t *testing.T, ctx context.Context, endpoint string, headers http.Header) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "kairos-test", Version: "v1"}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{},
	})
	httpClient := &http.Client{Transport: headerTransport{base: http.DefaultTransport, headers: headers}}
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint, HTTPClient: httpClient, MaxRetries: -1, DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	return session
}

func callTool[T any](t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, input any) T {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		t.Fatalf("call tool %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("tool %s returned error: %+v", name, result.Content)
	}
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal %s structured content: %v", name, err)
	}
	var output T
	if err := json.Unmarshal(payload, &output); err != nil {
		t.Fatalf("decode %s structured content %s: %v", name, payload, err)
	}
	return output
}

type headerTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for name, values := range t.headers {
		for _, value := range values {
			clone.Header.Add(name, value)
		}
	}
	return t.base.RoundTrip(clone)
}

type mcpClock struct{}

func (mcpClock) Now() time.Time { return time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC) }

type mcpIDs struct{ next atomic.Uint64 }

func (g *mcpIDs) NewID() string { return fmt.Sprintf("mcp-%d", g.next.Add(1)) }
