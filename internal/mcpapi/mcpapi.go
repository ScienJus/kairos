// Package mcpapi exposes the agent execution surface through MCP.
package mcpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMaxArtifactUploadBytes int64 = 16 << 20
	mcpRequestOverheadBytes       int64 = 1 << 20
	workItemCancelledErrorCode          = "work_item_cancelled"
)

const serverInstructions = "Use find_work to discover eligible work. For empty_blackboard or blackboard_completion, create more work when needed; otherwise submit_blackboard_completion. Accept only work_item_acceptance candidates with accept_blackboard_completion. Read task context before claim_task; execute only after a successful claim. Follow expected_artifacts, create external deliverables with create_artifact or managed files with upload_artifact, and pass their IDs to submit_task. End every claim with submit_task, fail_task, or release_claim unless a tool returns work_item_cancelled; that terminal Human decision ends the claim, so stop immediately without another mutation. Resource-creating tools that accept operation_id replay an identical retry; use a new ID when their arguments change. Use get_work_item_context to inspect open or terminal WorkItems by ID. Identity comes from the MCP transport, never tool arguments."

// Options configures MCP transport limits.
type Options struct {
	MaxArtifactUploadBytes int64
}

// Handler serves a stateless Streamable HTTP MCP endpoint. Every request is
// authenticated independently before the SDK dispatches a tool call.
type Handler struct {
	service  *application.Service
	identity identity.Resolver
	handler  http.Handler
}

// New creates an MCP handler backed by the Kairos application service.
func New(service *application.Service, resolver identity.Resolver, options ...Options) (*Handler, error) {
	if service == nil {
		return nil, errors.New("application service is required")
	}
	if resolver == nil {
		return nil, errors.New("identity resolver is required")
	}
	configured, err := mcpOptions(options)
	if err != nil {
		return nil, err
	}
	maxRequestBodyBytes, err := maxMCPRequestBodyBytes(configured.MaxArtifactUploadBytes)
	if err != nil {
		return nil, err
	}

	h := &Handler{service: service, identity: resolver}
	schemaCache := mcp.NewSchemaCache()
	streamable := mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		actor, ok := identityFromContext(request)
		if !ok {
			return nil
		}
		return newServer(service, actor, schemaCache, configured.MaxArtifactUploadBytes)
	}, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		MaxRequestBodyBytes:          maxRequestBodyBytes,
		PropagateRequestCancellation: true,
	})
	h.handler = http.NewCrossOriginProtection().Handler(streamable)
	return h, nil
}

func mcpOptions(values []Options) (Options, error) {
	if len(values) > 1 {
		return Options{}, errors.New("MCP options must be provided at most once")
	}
	if len(values) == 0 {
		return Options{MaxArtifactUploadBytes: defaultMaxArtifactUploadBytes}, nil
	}
	if values[0].MaxArtifactUploadBytes <= 0 {
		return Options{}, errors.New("max Artifact upload bytes must be positive")
	}
	return values[0], nil
}

func maxMCPRequestBodyBytes(maxUploadBytes int64) (int64, error) {
	const maxInt64 = int64(^uint64(0) >> 1)
	if maxUploadBytes > ((maxInt64-mcpRequestOverheadBytes)/4)*3-2 {
		return 0, errors.New("max Artifact upload bytes is too large for Base64 MCP transport")
	}
	return ((maxUploadBytes + 2) / 3 * 4) + mcpRequestOverheadBytes, nil
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	actor, err := h.identity.Resolve(request)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, identity.ErrUnauthenticated) {
			status = http.StatusUnauthorized
			writer.Header().Set("WWW-Authenticate", "Bearer")
		}
		http.Error(writer, http.StatusText(status), status)
		return
	}
	h.handler.ServeHTTP(writer, request.WithContext(withIdentity(request.Context(), actor)))
}

func newServer(service *application.Service, actor identity.Identity, schemaCache *mcp.SchemaCache, maxArtifactUploadBytes int64) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "kairos", Version: "v1"}, &mcp.ServerOptions{
		Instructions: serverInstructions,
		SchemaCache:  schemaCache,
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "find_work",
		Title:       "Find Kairos work",
		Description: "Find executable tasks and Blackboard planning, completion, or acceptance decisions for this actor.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input findWorkInput) (*mcp.CallToolResult, findWorkOutput, error) {
		candidates, err := service.FindWork(ctx, application.FindWorkQuery{
			Identity: actor,
			Tags:     input.Tags,
			Limit:    input.Limit,
		})
		output := findWorkView(candidates)
		return successResult(fmt.Sprintf("Found %d eligible candidate(s).", len(output.Candidates))), output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "upload_artifact",
		Title:       "Upload artifact",
		Description: "Upload a small managed file as Base64 under an active Claim; use create_artifact with a durable external URI for large files.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input uploadArtifactInput) (*mcp.CallToolResult, artifactOutput, error) {
		if err := requireOperationID(input.OperationID); err != nil {
			return nil, artifactOutput{}, err
		}
		content := strings.TrimSpace(input.ContentBase64)
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return nil, artifactOutput{}, fmt.Errorf("decode artifact content_base64: %w", err)
		}
		if int64(len(decoded)) > maxArtifactUploadBytes {
			return nil, artifactOutput{}, fmt.Errorf("artifact content exceeds %d bytes", maxArtifactUploadBytes)
		}
		artifact, err := service.UploadArtifact(ctx, application.UploadArtifactCommand{
			TaskID: domain.TaskID(input.TaskID), ClaimID: domain.ClaimID(input.ClaimID), Identity: actor,
			OperationID: input.OperationID, Name: input.Name,
		}, bytes.NewReader(decoded))
		return successResult(fmt.Sprintf("Uploaded Artifact %s for Task %s.", artifact.ID, artifact.TaskID)), artifactOutput{Artifact: artifactViewFrom(artifact)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_work_item_context",
		Title:       "Get work item context",
		Description: "Get an open or terminal WorkItem with its Definition, tasks, relations, result, and status.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input workItemContextInput) (*mcp.CallToolResult, workItemContextOutput, error) {
		result, err := service.GetWorkItemExecutionContext(ctx, application.GetWorkItemExecutionContextQuery{
			WorkItemID: domain.WorkItemID(input.WorkItemID),
			Identity:   actor,
		})
		output := workItemContextView(result)
		return successResult(fmt.Sprintf("Loaded WorkItem %s with status %s.", output.WorkItem.ID, output.WorkItem.Status)), output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task_context",
		Title:       "Get task context",
		Description: "Get a task's durable instructions, history, and coordination context.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input taskContextInput) (*mcp.CallToolResult, taskContextOutput, error) {
		result, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{
			TaskID:   domain.TaskID(input.TaskID),
			Identity: actor,
		})
		output := taskContextView(result)
		return successResult(fmt.Sprintf("Loaded Task %s with status %s.", output.Task.ID, output.Task.Status)), output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "claim_task",
		Title:       "Claim task",
		Description: "Atomically claim a pending task before work begins.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input claimTaskInput) (*mcp.CallToolResult, claimOutput, error) {
		if err := requireOperationID(input.OperationID); err != nil {
			return nil, claimOutput{}, err
		}
		claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{
			TaskID:       domain.TaskID(input.TaskID),
			Identity:     actor,
			OperationID:  input.OperationID,
			LeaseSeconds: input.LeaseSeconds,
		})
		return successResult(fmt.Sprintf("Claimed Task %s with Claim %s.", claim.TaskID, claim.ID)), claimOutput{Claim: claimViewFrom(claim)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "heartbeat_claim", Title: "Heartbeat claim", Description: "Extend an active claim before reaping.", Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input heartbeatClaimInput) (*mcp.CallToolResult, claimOutput, error) {
		claim, err := service.HeartbeatClaim(ctx, application.HeartbeatClaimCommand{TaskID: domain.TaskID(input.TaskID), ClaimID: domain.ClaimID(input.ClaimID), Identity: actor, LeaseSeconds: input.LeaseSeconds})
		return successResult(fmt.Sprintf("Heartbeated Claim %s.", input.ClaimID)), claimOutput{Claim: claimViewFrom(claim)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_artifact",
		Title:       "Create artifact",
		Description: "Register an external Artifact under an active Claim.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createArtifactInput) (*mcp.CallToolResult, artifactOutput, error) {
		if err := requireOperationID(input.OperationID); err != nil {
			return nil, artifactOutput{}, err
		}
		artifact, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{
			TaskID: domain.TaskID(input.TaskID), ClaimID: domain.ClaimID(input.ClaimID), Identity: actor,
			OperationID: input.OperationID, Name: input.Name, URI: input.URI,
		})
		return successResult(fmt.Sprintf("Created Artifact %s for Task %s.", artifact.ID, artifact.TaskID)), artifactOutput{Artifact: artifactViewFrom(artifact)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_task",
		Title:       "Submit task result",
		Description: "Submit a Claim's result, Artifacts, Review request, and Workflow transition.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input submitTaskInput) (*mcp.CallToolResult, submissionOutput, error) {
		var transition *application.WorkflowTransitionCommand
		if input.Transition != nil {
			transition = &application.WorkflowTransitionCommand{
				ChoiceGroupID:        domain.WorkflowChoiceGroupID(input.Transition.ChoiceGroupID),
				SkipOptionalTaskIDs:  workflowTaskIDs(input.Transition.SkipOptionalTaskIDs),
				ReviewSkippedTaskIDs: workflowTaskIDs(input.Transition.ReviewSkippedTaskIDs),
				Reason:               input.Transition.Reason,
			}
		}
		submission, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
			TaskID:        domain.TaskID(input.TaskID),
			ClaimID:       domain.ClaimID(input.ClaimID),
			Identity:      actor,
			Result:        input.Result,
			ArtifactIDs:   artifactIDs(input.ArtifactIDs),
			RequestReview: input.RequestReview,
			Transition:    transition,
		})
		return successResult(fmt.Sprintf("Submitted result %s for Task %s.", submission.ID, submission.TaskID)), submissionOutput{Submission: submissionViewFrom(submission)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "fail_task",
		Title:       "Report task failure",
		Description: "End an active claim by reopening the task for retry or failing the whole work item.",
		Annotations: mutationAnnotations(true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input failTaskInput) (*mcp.CallToolResult, failureOutput, error) {
		failure, err := service.FailTask(ctx, application.FailTaskCommand{
			TaskID:      domain.TaskID(input.TaskID),
			ClaimID:     domain.ClaimID(input.ClaimID),
			Identity:    actor,
			Action:      domain.TaskFailureAction(input.Action),
			Reason:      input.Reason,
			RetryPrompt: input.RetryPrompt,
		})
		return successResult(fmt.Sprintf("Recorded failure %s for Task %s.", failure.ID, failure.TaskID)), failureOutput{Failure: failureViewFrom(failure)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "release_claim",
		Title:       "Release task claim",
		Description: "Release an active claim and return the task to the pending candidate set.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input releaseClaimInput) (*mcp.CallToolResult, releasedOutput, error) {
		err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{
			TaskID:   domain.TaskID(input.TaskID),
			ClaimID:  domain.ClaimID(input.ClaimID),
			Identity: actor,
			Reason:   input.Reason,
		})
		return successResult(fmt.Sprintf("Released Claim %s for Task %s.", input.ClaimID, input.TaskID)), releasedOutput{Released: err == nil}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_blackboard_task",
		Title:       "Create blackboard task",
		Description: "Plan an executable task in an open or empty Blackboard.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createBlackboardTaskInput) (*mcp.CallToolResult, taskOutput, error) {
		if err := requireOperationID(input.OperationID); err != nil {
			return nil, taskOutput{}, err
		}
		task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
			WorkItemID:         domain.WorkItemID(input.WorkItemID),
			Identity:           actor,
			OperationID:        input.OperationID,
			Title:              input.Title,
			Description:        input.Description,
			AcceptanceCriteria: input.AcceptanceCriteria,
			Executor:           domain.ExecutorRequirement(input.Executor),
			AllowedRoles:       input.AllowedRoles,
			Tags:               input.Tags,
		})
		return successResult(fmt.Sprintf("Created Task %s in WorkItem %s.", task.ID, task.WorkItemID)), taskOutput{Task: taskSummaryViewFrom(task)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{Name: "add_blackboard_relation", Title: "Add blackboard relation", Description: "Add a suggested dependency relation between two tasks in an open Blackboard.", Annotations: mutationAnnotations(false)}, func(ctx context.Context, _ *mcp.CallToolRequest, input addBlackboardRelationInput) (*mcp.CallToolResult, relationOutput, error) {
		relation, err := service.AddBlackboardRelation(ctx, application.AddBlackboardRelationCommand{WorkItemID: domain.WorkItemID(input.WorkItemID), FromTaskID: domain.TaskID(input.FromTaskID), ToTaskID: domain.TaskID(input.ToTaskID), Identity: actor})
		return successResult(fmt.Sprintf("Added relation from Task %s to Task %s.", input.FromTaskID, input.ToTaskID)), relationOutput{Relation: relationViewFrom(relation)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{Name: "decompose_blackboard_task", Title: "Decompose blackboard task", Description: "Turn an actively claimed Blackboard task into an aggregate and create its initial child tasks.", Annotations: mutationAnnotations(false)}, func(ctx context.Context, _ *mcp.CallToolRequest, input decomposeBlackboardTaskInput) (*mcp.CallToolResult, decompositionOutput, error) {
		if err := requireOperationID(input.OperationID); err != nil {
			return nil, decompositionOutput{}, err
		}
		children := make([]application.BlackboardTaskSpec, 0, len(input.Children))
		for _, child := range input.Children {
			children = append(children, child.spec())
		}
		result, err := service.DecomposeBlackboardTask(ctx, application.DecomposeBlackboardTaskCommand{TaskID: domain.TaskID(input.TaskID), ClaimID: domain.ClaimID(input.ClaimID), Identity: actor, OperationID: input.OperationID, Children: children})
		return successResult(fmt.Sprintf("Decomposed Task %s into %d children.", input.TaskID, len(result.Children))), decompositionOutput{Parent: taskSummaryViewFrom(result.Parent), Children: taskSummaryViews(result.Children)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{Name: "add_blackboard_child_task", Title: "Add blackboard child task", Description: "Append one child task while a Blackboard aggregate remains open.", Annotations: mutationAnnotations(false)}, func(ctx context.Context, _ *mcp.CallToolRequest, input addBlackboardChildTaskInput) (*mcp.CallToolResult, taskOutput, error) {
		if err := requireOperationID(input.OperationID); err != nil {
			return nil, taskOutput{}, err
		}
		task, err := service.AddBlackboardChildTask(ctx, application.AddBlackboardChildTaskCommand{ParentTaskID: domain.TaskID(input.ParentTaskID), Identity: actor, OperationID: input.OperationID, Task: input.Task.spec()})
		return successResult(fmt.Sprintf("Added child Task %s.", task.ID)), taskOutput{Task: taskSummaryViewFrom(task)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{Name: "skip_blackboard_task", Title: "Skip blackboard task", Description: "Skip an obsolete unclaimed pending Blackboard task with a durable reason.", Annotations: mutationAnnotations(true)}, func(ctx context.Context, _ *mcp.CallToolRequest, input skipBlackboardTaskInput) (*mcp.CallToolResult, taskOutput, error) {
		task, err := service.SkipBlackboardTask(ctx, application.SkipBlackboardTaskCommand{TaskID: domain.TaskID(input.TaskID), Identity: actor, Reason: input.Reason})
		return successResult(fmt.Sprintf("Skipped Task %s.", input.TaskID)), taskOutput{Task: taskSummaryViewFrom(task)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{Name: "submit_blackboard_completion", Title: "Submit blackboard completion", Description: "Submit a converged Blackboard's durable result for acceptance.", Annotations: mutationAnnotations(false)}, func(ctx context.Context, _ *mcp.CallToolRequest, input submitBlackboardCompletionInput) (*mcp.CallToolResult, workItemOutput, error) {
		workItem, err := service.SubmitBlackboardCompletion(ctx, application.SubmitBlackboardCompletionCommand{WorkItemID: domain.WorkItemID(input.WorkItemID), Identity: actor, Result: input.Result})
		return successResult(fmt.Sprintf("Submitted completion for Blackboard WorkItem %s.", input.WorkItemID)), workItemOutput{WorkItem: workItemViewFrom(workItem)}, toolError(err)
	})

	mcp.AddTool(server, &mcp.Tool{Name: "accept_blackboard_completion", Title: "Accept blackboard completion", Description: "Accept a pending Blackboard completion proposal.", Annotations: mutationAnnotations(false)}, func(ctx context.Context, _ *mcp.CallToolRequest, input acceptBlackboardCompletionInput) (*mcp.CallToolResult, workItemOutput, error) {
		workItem, err := service.AcceptBlackboardCompletion(ctx, application.AcceptBlackboardCompletionCommand{WorkItemID: domain.WorkItemID(input.WorkItemID), Identity: actor})
		return successResult(fmt.Sprintf("Accepted completion for Blackboard WorkItem %s.", input.WorkItemID)), workItemOutput{WorkItem: workItemViewFrom(workItem)}, toolError(err)
	})

	return server
}

type findWorkInput struct {
	Tags  []string `json:"tags,omitempty" jsonschema:"Require candidates to contain all of these tags."`
	Limit int      `json:"limit,omitempty" jsonschema:"Maximum number of candidates to return; zero means no limit."`
}

type taskContextInput struct {
	TaskID string `json:"task_id" jsonschema:"Concrete Kairos task ID."`
}

type workItemContextInput struct {
	WorkItemID string `json:"work_item_id" jsonschema:"Concrete Kairos WorkItem ID, including a terminal WorkItem."`
}

type claimTaskInput struct {
	TaskID       string `json:"task_id" jsonschema:"Concrete Kairos task ID."`
	OperationID  string `json:"operation_id" jsonschema:"Stable unique ID for idempotent retries of this mutation."`
	LeaseSeconds int64  `json:"lease_seconds,omitempty" jsonschema:"Requested lease duration in seconds; the server clamps it to policy bounds."`
}

type heartbeatClaimInput struct {
	TaskID       string `json:"task_id" jsonschema:"Concrete Kairos task ID."`
	ClaimID      string `json:"claim_id" jsonschema:"Active claim owned by the current actor."`
	LeaseSeconds int64  `json:"lease_seconds,omitempty" jsonschema:"Requested lease duration in seconds; the server clamps it to policy bounds."`
}

type workflowTransitionInput struct {
	ChoiceGroupID        string   `json:"choice_group_id"`
	SkipOptionalTaskIDs  []string `json:"skip_optional_task_ids,omitempty"`
	ReviewSkippedTaskIDs []string `json:"review_skipped_task_ids,omitempty"`
	Reason               string   `json:"reason,omitempty"`
}

type submitTaskInput struct {
	TaskID        string                   `json:"task_id"`
	ClaimID       string                   `json:"claim_id"`
	Result        string                   `json:"result"`
	ArtifactIDs   []string                 `json:"artifact_ids,omitempty"`
	RequestReview bool                     `json:"request_review,omitempty"`
	Transition    *workflowTransitionInput `json:"transition,omitempty"`
}

type createArtifactInput struct {
	TaskID      string `json:"task_id"`
	ClaimID     string `json:"claim_id"`
	OperationID string `json:"operation_id"`
	Name        string `json:"name"`
	URI         string `json:"uri"`
}

type uploadArtifactInput struct {
	TaskID        string `json:"task_id"`
	ClaimID       string `json:"claim_id"`
	OperationID   string `json:"operation_id"`
	Name          string `json:"name"`
	ContentBase64 string `json:"content_base64" jsonschema:"Standard Base64 file bytes without a data URI prefix."`
}

type failTaskInput struct {
	TaskID      string `json:"task_id"`
	ClaimID     string `json:"claim_id"`
	Action      string `json:"action" jsonschema:"Failure action: reopen or fail_work_item."`
	Reason      string `json:"reason"`
	RetryPrompt string `json:"retry_prompt,omitempty"`
}

type releaseClaimInput struct {
	TaskID  string `json:"task_id" jsonschema:"Concrete Kairos task ID."`
	ClaimID string `json:"claim_id" jsonschema:"Active claim owned by the current actor."`
	Reason  string `json:"reason,omitempty" jsonschema:"Why the claim is being released, when useful for the next executor or an automated release."`
}

type createBlackboardTaskInput struct {
	WorkItemID         string   `json:"work_item_id" jsonschema:"Open blackboard work item ID."`
	OperationID        string   `json:"operation_id" jsonschema:"Stable unique ID for idempotent retries of this mutation."`
	Title              string   `json:"title" jsonschema:"Short executable task title."`
	Description        string   `json:"description,omitempty" jsonschema:"What the executor should do."`
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty" jsonschema:"Observable conditions for successful completion."`
	Executor           string   `json:"executor" jsonschema:"Required executor kind: agent, human, or either."`
	AllowedRoles       []string `json:"allowed_roles,omitempty" jsonschema:"Agent roles allowed to execute the task; empty means unrestricted."`
	Tags               []string `json:"tags,omitempty" jsonschema:"Searchable task tags."`
}

func (input createBlackboardTaskInput) spec() application.BlackboardTaskSpec {
	return application.BlackboardTaskSpec{Title: input.Title, Description: input.Description, AcceptanceCriteria: input.AcceptanceCriteria, Executor: domain.ExecutorRequirement(input.Executor), AllowedRoles: input.AllowedRoles, Tags: input.Tags}
}

type addBlackboardRelationInput struct {
	WorkItemID string `json:"work_item_id"`
	FromTaskID string `json:"from_task_id"`
	ToTaskID   string `json:"to_task_id"`
}
type decomposeBlackboardTaskInput struct {
	TaskID      string                   `json:"task_id"`
	ClaimID     string                   `json:"claim_id"`
	OperationID string                   `json:"operation_id"`
	Children    []addBlackboardChildSpec `json:"children"`
}
type addBlackboardChildTaskInput struct {
	ParentTaskID string                 `json:"parent_task_id"`
	OperationID  string                 `json:"operation_id"`
	Task         addBlackboardChildSpec `json:"task"`
}
type addBlackboardChildSpec struct {
	Title              string   `json:"title" jsonschema:"Short executable task title."`
	Description        string   `json:"description,omitempty"`
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty"`
	Executor           string   `json:"executor"`
	AllowedRoles       []string `json:"allowed_roles,omitempty"`
	Tags               []string `json:"tags,omitempty"`
}

func (input addBlackboardChildSpec) spec() application.BlackboardTaskSpec {
	return application.BlackboardTaskSpec{Title: input.Title, Description: input.Description, AcceptanceCriteria: input.AcceptanceCriteria, Executor: domain.ExecutorRequirement(input.Executor), AllowedRoles: input.AllowedRoles, Tags: input.Tags}
}

type skipBlackboardTaskInput struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}
type submitBlackboardCompletionInput struct {
	WorkItemID string `json:"work_item_id"`
	Result     string `json:"result"`
}

type acceptBlackboardCompletionInput struct {
	WorkItemID string `json:"work_item_id"`
}

func workflowTaskIDs(values []string) []domain.WorkflowTaskID {
	result := make([]domain.WorkflowTaskID, 0, len(values))
	for _, value := range values {
		result = append(result, domain.WorkflowTaskID(value))
	}
	return result
}

func artifactIDs(values []string) []domain.ArtifactID {
	result := make([]domain.ArtifactID, 0, len(values))
	for _, value := range values {
		result = append(result, domain.ArtifactID(value))
	}
	return result
}

func successResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

func toolError(err error) error {
	if errors.Is(err, application.ErrWorkItemCancelled) {
		return fmt.Errorf("%s: %w", workItemCancelledErrorCode, err)
	}
	return err
}

func requireOperationID(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("operation_id is required")
	}
	return nil
}

func readOnlyAnnotations() *mcp.ToolAnnotations {
	closedWorld := false
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld}
}

func mutationAnnotations(destructive bool) *mcp.ToolAnnotations {
	closedWorld := false
	return &mcp.ToolAnnotations{
		DestructiveHint: &destructive,
		IdempotentHint:  true,
		OpenWorldHint:   &closedWorld,
	}
}
