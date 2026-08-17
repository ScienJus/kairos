// Package mcpapi exposes the agent execution surface through MCP.
package mcpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxRequestBodyBytes = 1 << 20

const serverInstructions = "Use find_work to discover eligible work. For an empty_blackboard, create a concrete task and discover again. Read task context before claim_task; execute only after a successful claim. End every claim with submit_task, fail_task, or release_claim. Reuse operation_id only for an identical retry. Use get_work_item_context to inspect open or terminal WorkItems by ID. Identity comes from the MCP transport, never tool arguments."

// Handler serves a stateless Streamable HTTP MCP endpoint. Every request is
// authenticated independently before the SDK dispatches a tool call.
type Handler struct {
	service  *application.Service
	identity identity.Resolver
	handler  http.Handler
}

// New creates an MCP handler backed by the Kairos application service.
func New(service *application.Service, resolver identity.Resolver) (*Handler, error) {
	if service == nil {
		return nil, errors.New("application service is required")
	}
	if resolver == nil {
		return nil, errors.New("identity resolver is required")
	}

	h := &Handler{service: service, identity: resolver}
	schemaCache := mcp.NewSchemaCache()
	streamable := mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		actor, ok := identityFromContext(request)
		if !ok {
			return nil
		}
		return newServer(service, actor, schemaCache)
	}, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		MaxRequestBodyBytes:          maxRequestBodyBytes,
		PropagateRequestCancellation: true,
	})
	h.handler = http.NewCrossOriginProtection().Handler(streamable)
	return h, nil
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

func newServer(service *application.Service, actor identity.Identity, schemaCache *mcp.SchemaCache) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "kairos", Version: "v1"}, &mcp.ServerOptions{
		Instructions: serverInstructions,
		SchemaCache:  schemaCache,
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "find_work",
		Title:       "Find Kairos work",
		Description: "Find pending tasks this actor may execute, plus empty blackboards that need planning.",
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
		Name:        "get_work_item_context",
		Title:       "Get work item context",
		Description: "Get one open or terminal WorkItem with its Definition, task summaries, relations, final result, and status.",
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
		Description: "Get the durable execution context for a task, including instructions, history, and coordination state.",
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
		Description: "Atomically claim a pending task before starting work. Reuse operation_id when retrying the same call.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input claimTaskInput) (*mcp.CallToolResult, claimOutput, error) {
		claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{
			TaskID:      domain.TaskID(input.TaskID),
			Identity:    actor,
			OperationID: input.OperationID,
		})
		return successResult(fmt.Sprintf("Claimed Task %s with Claim %s.", claim.TaskID, claim.ID)), claimOutput{Claim: claimViewFrom(claim)}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_task",
		Title:       "Submit task result",
		Description: "Submit an immutable result from an active claim and optionally request review or choose a workflow transition.",
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
			OperationID:   input.OperationID,
			Result:        input.Result,
			RequestReview: input.RequestReview,
			Transition:    transition,
		})
		return successResult(fmt.Sprintf("Submitted result %s for Task %s.", submission.ID, submission.TaskID)), submissionOutput{Submission: submissionViewFrom(submission)}, err
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
			OperationID: input.OperationID,
			Action:      domain.TaskFailureAction(input.Action),
			Reason:      input.Reason,
			RetryPrompt: input.RetryPrompt,
		})
		return successResult(fmt.Sprintf("Recorded failure %s for Task %s.", failure.ID, failure.TaskID)), failureOutput{Failure: failureViewFrom(failure)}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "release_claim",
		Title:       "Release task claim",
		Description: "Release an active claim and return the task to the pending candidate set.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input releaseClaimInput) (*mcp.CallToolResult, releasedOutput, error) {
		err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{
			TaskID:      domain.TaskID(input.TaskID),
			ClaimID:     domain.ClaimID(input.ClaimID),
			Identity:    actor,
			OperationID: input.OperationID,
			Reason:      input.Reason,
		})
		return successResult(fmt.Sprintf("Released Claim %s for Task %s.", input.ClaimID, input.TaskID)), releasedOutput{Released: err == nil}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_blackboard_task",
		Title:       "Create blackboard task",
		Description: "Plan one executable task in an open blackboard, including an empty blackboard returned by find_work.",
		Annotations: mutationAnnotations(false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input createBlackboardTaskInput) (*mcp.CallToolResult, taskOutput, error) {
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
		return successResult(fmt.Sprintf("Created Task %s in WorkItem %s.", task.ID, task.WorkItemID)), taskOutput{Task: taskSummaryViewFrom(task)}, err
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
	TaskID      string `json:"task_id" jsonschema:"Concrete Kairos task ID."`
	OperationID string `json:"operation_id" jsonschema:"Stable unique ID for idempotent retries of this mutation."`
}

type workflowTransitionInput struct {
	ChoiceGroupID        string   `json:"choice_group_id" jsonschema:"Workflow choice group selected after completing the task."`
	SkipOptionalTaskIDs  []string `json:"skip_optional_task_ids,omitempty" jsonschema:"Optional workflow task definition IDs intentionally skipped."`
	ReviewSkippedTaskIDs []string `json:"review_skipped_task_ids,omitempty" jsonschema:"Subset of skipped IDs that should receive human review."`
	Reason               string   `json:"reason,omitempty" jsonschema:"Reason for the workflow progression choice."`
}

type submitTaskInput struct {
	TaskID        string                   `json:"task_id" jsonschema:"Concrete Kairos task ID."`
	ClaimID       string                   `json:"claim_id" jsonschema:"Active claim owned by the current actor."`
	OperationID   string                   `json:"operation_id" jsonschema:"Stable unique ID for idempotent retries of this mutation."`
	Result        string                   `json:"result" jsonschema:"Durable task result, including useful deliverables and evidence."`
	RequestReview bool                     `json:"request_review,omitempty" jsonschema:"Request human review when the task policy allows it."`
	Transition    *workflowTransitionInput `json:"transition,omitempty" jsonschema:"Required progression choice for a non-terminal workflow task."`
}

type failTaskInput struct {
	TaskID      string `json:"task_id" jsonschema:"Concrete Kairos task ID."`
	ClaimID     string `json:"claim_id" jsonschema:"Active claim owned by the current actor."`
	OperationID string `json:"operation_id" jsonschema:"Stable unique ID for idempotent retries of this mutation."`
	Action      string `json:"action" jsonschema:"Failure action: reopen or fail_work_item."`
	Reason      string `json:"reason" jsonschema:"What prevented successful execution."`
	RetryPrompt string `json:"retry_prompt,omitempty" jsonschema:"Improved instructions for the next executor when reopening."`
}

type releaseClaimInput struct {
	TaskID      string `json:"task_id" jsonschema:"Concrete Kairos task ID."`
	ClaimID     string `json:"claim_id" jsonschema:"Active claim owned by the current actor."`
	OperationID string `json:"operation_id" jsonschema:"Stable unique ID for idempotent retries of this mutation."`
	Reason      string `json:"reason,omitempty" jsonschema:"Why the claim is being released, when useful for the next executor or an automated release."`
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

func workflowTaskIDs(values []string) []domain.WorkflowTaskID {
	result := make([]domain.WorkflowTaskID, 0, len(values))
	for _, value := range values {
		result = append(result, domain.WorkflowTaskID(value))
	}
	return result
}

func successResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}}
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
