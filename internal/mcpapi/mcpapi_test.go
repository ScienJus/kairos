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
	service, _ := newMCPFixture(t, domain.WorkItemAcceptanceAgent)
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
	if initialized := session.InitializeResult(); initialized == nil || initialized.Instructions != serverInstructions {
		t.Fatalf("server instructions = %#v, want Kairos execution guidance", initialized)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	wantTools := []string{
		"accept_blackboard_completion", "add_blackboard_child_task", "add_blackboard_relation", "claim_task",
		"create_blackboard_task", "decompose_blackboard_task", "fail_task", "find_work",
		"get_task_context", "get_work_item_context", "heartbeat_claim", "release_claim",
		"skip_blackboard_task", "submit_blackboard_completion", "submit_task",
	}
	gotTools := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		gotTools = append(gotTools, tool.Name)
	}
	slices.Sort(gotTools)
	if !slices.Equal(gotTools, wantTools) {
		t.Fatalf("tools = %v, want %v", gotTools, wantTools)
	}
	toolPayload, err := json.Marshal(tools.Tools)
	if err != nil {
		t.Fatalf("marshal tool definitions: %v", err)
	}
	if len(toolPayload) > 32_000 {
		t.Fatalf("tool definitions are %d bytes, want at most 32000", len(toolPayload))
	}
	prettyToolPayload, err := json.MarshalIndent(tools.Tools, "", "  ")
	if err != nil {
		t.Fatalf("marshal formatted tool definitions: %v", err)
	}
	if len(prettyToolPayload) > 70_000 {
		t.Fatalf("formatted tool definitions are %d bytes, want at most 70000", len(prettyToolPayload))
	}

	find := callTool[findWorkOutput](t, ctx, session, "find_work", findWorkInput{})
	if len(find.Candidates) != 1 || find.Candidates[0].Kind != string(application.WorkCandidateEmptyBlackboard) || find.Candidates[0].Task != nil {
		t.Fatalf("initial candidates = %+v, want one empty blackboard without a zero-value task", find.Candidates)
	}
	if find.Candidates[0].Definition.AgentInstructions == "" {
		t.Fatalf("empty blackboard definition = %+v", find.Candidates[0].Definition)
	}
	workItemID := find.Candidates[0].WorkItem.ID

	created := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{
		WorkItemID: workItemID, OperationID: "plan-task-1",
		Title: "Implement MCP lifecycle", Description: "Exercise the agent execution tools.",
		AcceptanceCriteria: "Task can be claimed and submitted through MCP.",
		Executor:           "agent", AllowedRoles: []string{"backend"}, Tags: []string{"mcp"},
	})
	if created.Task.ID == "" || created.Task.WorkItemID != workItemID {
		t.Fatalf("created task = %+v", created.Task)
	}
	retryTask := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{
		WorkItemID: workItemID, OperationID: "plan-task-2",
		Title: "Exercise failure recovery", Executor: "agent", AllowedRoles: []string{"backend"}, Tags: []string{"mcp"},
	})
	relation := callTool[relationOutput](t, ctx, session, "add_blackboard_relation", addBlackboardRelationInput{WorkItemID: workItemID, FromTaskID: created.Task.ID, ToTaskID: retryTask.Task.ID, OperationID: "relation-1"})
	if relation.Relation.FromTaskID != created.Task.ID || relation.Relation.ToTaskID != retryTask.Task.ID {
		t.Fatalf("relation = %+v", relation.Relation)
	}
	skippable := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{WorkItemID: workItemID, OperationID: "plan-task-3", Title: "Obsolete task", Executor: "agent", AllowedRoles: []string{"backend"}, Tags: []string{"mcp"}})
	skipped := callTool[taskOutput](t, ctx, session, "skip_blackboard_task", skipBlackboardTaskInput{TaskID: skippable.Task.ID, OperationID: "skip-task-1", Reason: "Covered by the main lifecycle task."})
	if skipped.Task.Status != string(domain.TaskStatusSkipped) {
		t.Fatalf("skipped task = %+v", skipped.Task)
	}

	find = callTool[findWorkOutput](t, ctx, session, "find_work", findWorkInput{Tags: []string{"mcp"}})
	if len(find.Candidates) != 2 || find.Candidates[0].Task == nil || find.Candidates[1].Task == nil ||
		find.Candidates[0].Task.ID != created.Task.ID || find.Candidates[1].Task.ID != retryTask.Task.ID {
		t.Fatalf("planned candidates = %+v, want tasks %q and %q", find.Candidates, created.Task.ID, retryTask.Task.ID)
	}
	if find.Candidates[0].Definition.AgentInstructions == "" {
		t.Fatalf("task candidate definition = %+v", find.Candidates[0].Definition)
	}

	claimed := callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: created.Task.ID, OperationID: "claim-task-1",
	})
	if claimed.Claim.Executor.ID != "codex-backend" {
		t.Fatalf("claim executor = %+v", claimed.Claim.Executor)
	}

	taskContext := callTool[taskContextOutput](t, ctx, session, "get_task_context", taskContextInput{
		TaskID: created.Task.ID,
	})
	if taskContext.Task.Status != string(domain.TaskStatusWorking) || taskContext.Blackboard == nil {
		t.Fatalf("task context = %+v", taskContext)
	}
	if taskContext.Blackboard.CurrentTaskID != created.Task.ID {
		t.Fatalf("blackboard current_task_id = %q, want %q", taskContext.Blackboard.CurrentTaskID, created.Task.ID)
	}
	if len(taskContext.Blackboard.Tasks) != 2 || taskContext.Blackboard.Tasks[0].ID != retryTask.Task.ID || taskContext.Blackboard.Tasks[1].ID != skippable.Task.ID {
		t.Fatalf("blackboard context duplicated current Task: %+v", taskContext.Blackboard.Tasks)
	}
	if !taskContext.Blackboard.CanDecompose {
		t.Fatalf("freshly claimed task cannot decompose: %+v", taskContext.Blackboard)
	}
	released := callTool[releasedOutput](t, ctx, session, "release_claim", releaseClaimInput{
		TaskID: created.Task.ID, ClaimID: claimed.Claim.ID,
		OperationID: "release-task-1", Reason: "Waiting for dependency access",
	})
	if !released.Released {
		t.Fatal("release_claim did not acknowledge release")
	}
	claimed = callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: created.Task.ID, OperationID: "claim-task-2",
	})

	submitted := callTool[submissionOutput](t, ctx, session, "submit_task", submitTaskInput{
		TaskID: created.Task.ID, ClaimID: claimed.Claim.ID,
		OperationID: "submit-task-1", Result: "MCP lifecycle verified end to end.",
	})
	if submitted.Submission.TaskID != created.Task.ID {
		t.Fatalf("submission = %+v", submitted.Submission)
	}

	retryClaim := callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: retryTask.Task.ID, OperationID: "claim-retry-task-1",
	})
	failure := callTool[failureOutput](t, ctx, session, "fail_task", failTaskInput{
		TaskID: retryTask.Task.ID, ClaimID: retryClaim.Claim.ID,
		OperationID: "fail-retry-task-1", Action: "reopen",
		Reason: "First attempt found missing context.", RetryPrompt: "Use the complete Blackboard context on retry.",
	})
	if failure.Failure.Action != string(domain.TaskFailureReopen) {
		t.Fatalf("failure = %+v", failure.Failure)
	}
	retryClaim = callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: retryTask.Task.ID, OperationID: "claim-retry-task-2",
	})
	callTool[submissionOutput](t, ctx, session, "submit_task", submitTaskInput{
		TaskID: retryTask.Task.ID, ClaimID: retryClaim.Claim.ID,
		OperationID: "submit-retry-task-1", Result: "Failure recovery verified.",
	})

	taskContext = callTool[taskContextOutput](t, ctx, session, "get_task_context", taskContextInput{
		TaskID: retryTask.Task.ID,
	})
	if taskContext.Task.Status != string(domain.TaskStatusCompleted) || taskContext.WorkItem.Status != string(domain.WorkItemStatusOpen) || len(taskContext.Task.Failures) != 1 {
		t.Fatalf("converged context = %+v", taskContext)
	}
	find = callTool[findWorkOutput](t, ctx, session, "find_work", findWorkInput{Tags: []string{"mcp"}})
	if len(find.Candidates) != 1 || find.Candidates[0].Kind != string(application.WorkCandidateBlackboardCompletion) {
		t.Fatalf("completion candidates = %+v", find.Candidates)
	}
	completion := callTool[workItemOutput](t, ctx, session, "submit_blackboard_completion", submitBlackboardCompletionInput{
		WorkItemID: workItemID, OperationID: "submit-completion-1", Result: "MCP lifecycle and recovery verified.",
	})
	if completion.WorkItem.Status != string(domain.WorkItemStatusAwaitingAgentAcceptance) {
		t.Fatalf("completion = %+v", completion.WorkItem)
	}
	find = callTool[findWorkOutput](t, ctx, session, "find_work", findWorkInput{Tags: []string{"mcp"}})
	if len(find.Candidates) != 1 || find.Candidates[0].Kind != string(application.WorkCandidateWorkItemAcceptance) {
		t.Fatalf("acceptance candidates = %+v", find.Candidates)
	}
	accepted := callTool[workItemOutput](t, ctx, session, "accept_blackboard_completion", acceptBlackboardCompletionInput{
		WorkItemID: workItemID, OperationID: "accept-completion-1",
	})
	if accepted.WorkItem.Status != string(domain.WorkItemStatusCompleted) || accepted.WorkItem.Result != completion.WorkItem.Result {
		t.Fatalf("accepted completion = %+v", accepted.WorkItem)
	}
	workItemContext := callTool[workItemContextOutput](t, ctx, session, "get_work_item_context", workItemContextInput{
		WorkItemID: workItemID,
	})
	if workItemContext.WorkItem.Status != string(domain.WorkItemStatusCompleted) || workItemContext.WorkItem.Result == "" || len(workItemContext.Tasks) != 3 {
		t.Fatalf("completed work item context = %+v", workItemContext)
	}
	find = callTool[findWorkOutput](t, ctx, session, "find_work", findWorkInput{Tags: []string{"mcp"}})
	if find.Candidates == nil || len(find.Candidates) != 0 {
		t.Fatalf("terminal candidate list = %#v, want non-nil empty list", find.Candidates)
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
	find := callTool[findWorkOutput](t, ctx, session, "find_work", findWorkInput{})
	if len(find.Candidates) != 1 || find.Candidates[0].Kind != string(application.WorkCandidateEmptyBlackboard) {
		t.Fatalf("authenticated candidates = %+v", find.Candidates)
	}

	created := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{
		WorkItemID: find.Candidates[0].WorkItem.ID, OperationID: "authenticated-plan-1",
		Title: "Authenticated task", Executor: "agent", AllowedRoles: []string{"backend"},
	})
	claimed := callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{
		TaskID: created.Task.ID, OperationID: "authenticated-claim-1",
	})
	if claimed.Claim.Executor.ID != "authenticated-agent" || claimed.Claim.Executor.Kind != string(domain.ActorAgent) {
		t.Fatalf("authenticated claim executor = %+v", claimed.Claim.Executor)
	}
}

func TestMCPBlackboardPlanningTools(t *testing.T) {
	ctx := context.Background()
	service, _ := newMCPFixture(t)
	handler, err := New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	session := connectMCP(t, ctx, server.URL, http.Header{identity.HeaderActorID: {"planner"}, identity.HeaderActorKind: {"agent"}, identity.HeaderActorRole: {"backend"}})
	t.Cleanup(func() { _ = session.Close() })
	find := callTool[findWorkOutput](t, ctx, session, "find_work", findWorkInput{})
	workItemID := find.Candidates[0].WorkItem.ID
	parent := callTool[taskOutput](t, ctx, session, "create_blackboard_task", createBlackboardTaskInput{WorkItemID: workItemID, OperationID: "parent-1", Title: "Aggregate", Executor: "agent", AllowedRoles: []string{"backend"}})
	claim := callTool[claimOutput](t, ctx, session, "claim_task", claimTaskInput{TaskID: parent.Task.ID, OperationID: "parent-claim-1"})
	decomposed := callTool[decompositionOutput](t, ctx, session, "decompose_blackboard_task", decomposeBlackboardTaskInput{TaskID: parent.Task.ID, ClaimID: claim.Claim.ID, OperationID: "decompose-1", Children: []createBlackboardTaskInput{{Title: "Child one", Executor: "agent", AllowedRoles: []string{"backend"}}, {Title: "Child two", Executor: "agent", AllowedRoles: []string{"backend"}}}})
	if decomposed.Parent.Status != string(domain.TaskStatusWaitingChildren) || len(decomposed.Children) != 2 {
		t.Fatalf("decomposition = %+v", decomposed)
	}
	child := callTool[taskOutput](t, ctx, session, "add_blackboard_child_task", addBlackboardChildTaskInput{ParentTaskID: parent.Task.ID, OperationID: "child-append-1", Task: addBlackboardChildSpec{Title: "Child three", Executor: "agent", AllowedRoles: []string{"backend"}}})
	if child.Task.ParentTaskID == nil || *child.Task.ParentTaskID != parent.Task.ID {
		t.Fatalf("child = %+v", child.Task)
	}

	secondService, _ := newMCPFixture(t)
	secondHandler, err := New(secondService, identity.TrustedResolver{})
	if err != nil {
		t.Fatal(err)
	}
	secondServer := httptest.NewServer(secondHandler)
	t.Cleanup(secondServer.Close)
	secondSession := connectMCP(t, ctx, secondServer.URL, http.Header{identity.HeaderActorID: {"planner"}, identity.HeaderActorKind: {"agent"}, identity.HeaderActorRole: {"backend"}})
	t.Cleanup(func() { _ = secondSession.Close() })
	empty := callTool[findWorkOutput](t, ctx, secondSession, "find_work", findWorkInput{})
	completed := callTool[workItemOutput](t, ctx, secondSession, "submit_blackboard_completion", submitBlackboardCompletionInput{WorkItemID: empty.Candidates[0].WorkItem.ID, OperationID: "complete-empty-1", Result: "No execution task was required."})
	if completed.WorkItem.Status != string(domain.WorkItemStatusCompleted) {
		t.Fatalf("completed = %+v", completed.WorkItem)
	}
}

func newMCPFixture(t *testing.T, acceptanceModes ...domain.WorkItemAcceptanceMode) (*application.Service, *repository.SQLRepository) {
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
	acceptanceMode := domain.WorkItemAcceptanceNone
	if len(acceptanceModes) > 0 {
		acceptanceMode = acceptanceModes[0]
	}
	_, err = service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: setup,
		Title: "MCP end-to-end", Goal: "Prove an agent can plan and execute through MCP.", Tags: []string{"mcp"}, AcceptanceMode: acceptanceMode,
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
	if len(result.Content) != 1 {
		t.Fatalf("tool %s content blocks = %d, want one concise summary", name, len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || len(text.Text) == 0 || len(text.Text) > 256 || text.Text[0] == '{' {
		t.Fatalf("tool %s content is not a concise summary: %#v", name, result.Content[0])
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
