//go:build darwin || linux

package codexadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/artifactstore"
	"github.com/ScienJus/kairos/internal/daemon"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/mcpapi"
	"github.com/ScienJus/kairos/internal/repository"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authTransport struct{ token string }

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func helperMCP(prompt []byte, output string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	meta := map[string]string{}
	for _, word := range strings.Fields(string(prompt)) {
		if key, value, ok := strings.Cut(word, "="); ok {
			meta[key] = strings.TrimSuffix(value, ".")
		}
	}
	endpoint := ""
	for _, arg := range os.Args {
		if value, ok := strings.CutPrefix(arg, "mcp_servers.kairos.url="); ok {
			endpoint, _ = strconv.Unquote(value)
		}
	}
	client := &http.Client{Transport: authTransport{os.Getenv(executorEnv)}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "fake-managed-harness", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	initialized := session.InitializeResult()
	profile := "task_executor"
	if meta["kind"] != "task" {
		profile = "coordination_executor"
	}
	if initialized == nil || !strings.Contains(initialized.Instructions, profile) || !strings.Contains(initialized.Instructions, "Daemon") {
		return errors.New("missing credential-specific MCP execution instructions")
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	forbidden, err := client.Get(strings.TrimSuffix(endpoint, "/mcp") + "/api/v1/session")
	if err != nil {
		return err
	}
	forbidden.Body.Close()
	if forbidden.StatusCode != http.StatusForbidden {
		return errors.New("Executor credential accessed an ordinary identity operation")
	}
	for _, tool := range tools.Tools {
		switch tool.Name {
		case "find_work", "claim_task", "heartbeat_claim", "submit_task":
			return errors.New("executor received lifecycle tools")
		}
		if meta["kind"] != "task" && tool.Name == "upload_artifact" {
			return errors.New("coordination received write tools")
		}
	}
	call := func(name string, args any, result any) error {
		value, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			return err
		}
		if value.IsError {
			return fmt.Errorf("tool %s failed", name)
		}
		data, err := json.Marshal(value.StructuredContent)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, result)
	}
	var work struct {
		Tasks     []domain.Task     `json:"tasks"`
		Artifacts []domain.Artifact `json:"artifacts"`
	}
	if err = call("get_work_item_context", map[string]any{"work_item_id": meta["work_item_id"]}, &work); err != nil {
		return err
	}
	for _, artifact := range work.Artifacts {
		response, err := client.Get(strings.TrimSuffix(endpoint, "/mcp") + "/api/v1/artifacts/" + string(artifact.ID) + "/content")
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != 200 || string(data) != "managed result" {
			return errors.New("submitted Artifact content unavailable")
		}
	}
	var taskContext struct {
		Workflow *struct {
			ChoiceGroups []struct {
				ID string `json:"id"`
			} `json:"choice_groups"`
		} `json:"workflow"`
	}
	taskID := meta["task_id"]
	if taskID == "" && len(work.Tasks) > 0 {
		taskID = string(work.Tasks[0].ID)
	}
	if taskID != "" {
		if err = call("get_task_context", map[string]any{"task_id": taskID}, &taskContext); err != nil {
			return err
		}
	}
	outcome := daemon.HarnessOutcome{}
	if meta["kind"] == "task" {
		if os.Getenv("KAIROS_FAKE_ACTION") == "decompose" {
			outcome.Task = &daemon.TaskOutcome{Kind: daemon.Decomposed, Children: []daemon.TaskSpec{{Title: "Child", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}}}}
		} else {
			var created struct {
				Artifact domain.Artifact `json:"artifact"`
			}
			if err = call("upload_artifact", map[string]any{"task_id": taskID, "claim_id": meta["claim_id"], "operation_id": "harness-upload-" + meta["claim_id"], "name": "result", "content_base64": base64.StdEncoding.EncodeToString([]byte("managed result"))}, &created); err != nil {
				return err
			}
			outcome.Task = &daemon.TaskOutcome{Kind: daemon.Completed, Result: "done", ArtifactIDs: []domain.ArtifactID{created.Artifact.ID}, RequestReview: os.Getenv("KAIROS_FAKE_ACTION") == "review"}
			if taskContext.Workflow != nil && len(taskContext.Workflow.ChoiceGroups) > 0 && os.Getenv("KAIROS_FAKE_ACTION") == "transition" {
				outcome.Task.Transition = &daemon.Transition{ChoiceGroupID: domain.WorkflowChoiceGroupID(taskContext.Workflow.ChoiceGroups[0].ID)}
			}
		}
	} else {
		switch meta["kind"] {
		case "empty_blackboard":
			outcome.Coordination = &daemon.CoordinationDecision{Kind: daemon.CreateTask, Task: &daemon.TaskSpec{Title: "Execute", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}}}
		case "blackboard_completion":
			if len(work.Artifacts) == 0 {
				return errors.New("completion lacked submitted Artifact")
			}
			outcome.Coordination = &daemon.CoordinationDecision{Kind: daemon.SubmitCompletion, Result: "complete"}
		case "work_item_acceptance":
			if len(work.Artifacts) == 0 {
				return errors.New("acceptance lacked submitted Artifact")
			}
			outcome.Coordination = &daemon.CoordinationDecision{Kind: daemon.AcceptCompletion}
		default:
			return errors.New("unknown candidate")
		}
	}
	data, err := json.Marshal(outcome)
	if err != nil {
		return err
	}
	return os.WriteFile(output, data, 0600)
}

type testClock struct{}

func (testClock) Now() time.Time { return time.Now() }

type testIDs struct{ next atomic.Uint64 }

func (g *testIDs) NewID() string { return fmt.Sprint("adapter-", g.next.Add(1)) }

func TestProcessAdapterWithCoreHTTPMCPAndArtifacts(t *testing.T) {
	for _, scenario := range []string{"blackboard", "workflow", "scheduler", "review", "transition", "decompose"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "core.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			service, err := application.NewService(repo, testClock{}, &testIDs{})
			if err != nil {
				t.Fatal(err)
			}
			store, err := artifactstore.NewLocal(filepath.Join(t.TempDir(), "artifacts"))
			if err != nil {
				t.Fatal(err)
			}
			if err = service.ConfigureArtifactStore(store); err != nil {
				t.Fatal(err)
			}
			identities, err := identity.NewService(repo, testClock{}, identity.SecureTokenGenerator{})
			if err != nil {
				t.Fatal(err)
			}
			issued, err := identities.CreateIdentity(ctx, domain.ActorRef{Kind: domain.ActorAgent, ID: "managed"}, "backend")
			if err != nil {
				t.Fatal(err)
			}
			actor := identity.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "setup"}}
			var binding domain.DefinitionBinding
			if scenario == "blackboard" || scenario == "decompose" {
				definition, err := service.CreateBlackboardDefinition(ctx, application.CreateBlackboardDefinitionCommand{Identity: actor, Metadata: application.DefinitionMetadataCommand{ID: "test", Name: "Test"}})
				if err != nil {
					t.Fatal(err)
				}
				binding = definition.Binding()
			} else {
				graph := domain.WorkflowGraph{StartTaskIDs: []domain.WorkflowTaskID{"execute"}, Tasks: []domain.WorkflowTaskDefinition{{ID: "execute", Title: "Execute", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewExecutorDecides, Artifacts: []domain.ArtifactDefinition{{Name: "result", Description: "Managed result"}}}}}
				if scenario == "transition" {
					graph.Tasks = append(graph.Tasks, domain.WorkflowTaskDefinition{ID: "next", Title: "Next", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone})
					graph.Relations = []domain.WorkflowRelationDefinition{{ID: "next", FromTaskID: "execute", ToTaskID: "next"}}
				}
				definition, err := service.CreateWorkflowDefinition(ctx, application.CreateWorkflowDefinitionCommand{Identity: actor, Metadata: application.DefinitionMetadataCommand{ID: "test", Name: "Test"}, Graph: graph})
				if err != nil {
					t.Fatal(err)
				}
				binding = definition.Binding()
			}
			acceptance := domain.WorkItemAcceptanceNone
			if scenario == "blackboard" {
				acceptance = domain.WorkItemAcceptanceAgent
			}
			work, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Identity: actor, Definition: binding, Title: "Adapter test", Goal: "Execute through the real process boundary", AcceptanceMode: acceptance})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "decompose" {
				_, err = service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{Identity: actor, WorkItemID: work.ID, Title: "Parent", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			resolver := identity.AuthenticatedResolver{Authenticator: identities}
			httpHandler, err := httpapi.New(service, resolver)
			if err != nil {
				t.Fatal(err)
			}
			mcpHandler, err := mcpapi.New(service, resolver)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.Handle("/mcp", mcpHandler)
			mux.Handle("/", httpHandler)
			server := httptest.NewServer(mux)
			defer server.Close()
			client, err := daemon.NewHTTPClient(server.URL, daemon.NewSecret(issued.Token), nil)
			if err != nil {
				t.Fatal(err)
			}
			adapter, _ := fixture(t, "mcp")
			adapter.environment = append(adapter.environment, "KAIROS_FAKE_ACTION="+scenario)
			count := 1
			if scenario == "transition" {
				count = 2
			}
			if scenario == "blackboard" {
				count = 4
			}
			if scenario == "scheduler" {
				// Match the documented library configuration, including both URLs.
				options := daemon.DefaultSchedulerOptions()
				options.Dispatch.CoreURL = server.URL
				options.Dispatch.MCPURL = server.URL + "/mcp"
				options.WorkspaceRoot = t.TempDir()
				options.Dispatch.PollInterval = 10 * time.Millisecond
				options.DiscoveryInterval = 10 * time.Millisecond
				scheduler, err := daemon.NewScheduler(client, adapter, options)
				if err != nil {
					t.Fatal(err)
				}
				runCtx, stop := context.WithCancel(ctx)
				done := make(chan error, 1)
				go func() { done <- scheduler.Run(runCtx) }()
				for scheduler.Stats().Finished == 0 && ctx.Err() == nil {
					time.Sleep(10 * time.Millisecond)
				}
				stop()
				if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				stats := scheduler.Stats()
				if stats.Finished != 1 || stats.Active != 0 || stats.Retries != 0 || stats.Lost != 0 {
					t.Fatalf("documented configuration did not complete a single run: %+v", stats)
				}
				count = 0
			}
			for range count {
				rows, err := client.Discover(ctx, nil, 50, 5*time.Second)
				if err != nil || len(rows) == 0 {
					t.Fatalf("discover: %v %v", rows, err)
				}
				options := daemon.DefaultOptions()
				options.MCPURL = server.URL + "/mcp"
				options.CoreURL = server.URL
				options.Workspace = t.TempDir()
				options.PollInterval = 10 * time.Millisecond
				dispatch, err := daemon.NewDispatch(client, adapter, rows[0].Candidate, options)
				if err != nil {
					t.Fatal(err)
				}
				result, err := dispatch.Run(ctx)
				if err != nil || !result.OutcomeApplied || !result.ClaimEnded {
					t.Fatalf("dispatch: %+v %v", result, err)
				}
			}
			view, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{Identity: actor, WorkItemID: work.ID})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "blackboard", "workflow", "scheduler":
				if view.WorkItem.Status != domain.WorkItemStatusCompleted || len(view.Artifacts) != 1 {
					t.Fatalf("final state: %s artifacts=%d", view.WorkItem.Status, len(view.Artifacts))
				}
			case "review":
				if view.Tasks[0].Status != domain.TaskStatusInReview {
					t.Fatal("review intent lost")
				}
			case "transition":
				if len(view.Tasks) != 2 || len(view.Artifacts) != 2 || view.WorkItem.Status != domain.WorkItemStatusCompleted {
					t.Fatal("transition was not applied")
				}
			case "decompose":
				if len(view.Tasks) != 2 {
					t.Fatal("decomposition was not applied")
				}
			}
			if len(adapter.runs) != 0 {
				t.Fatal("Dispatch did not forget terminal Adapter metadata")
			}
		})
	}
}
