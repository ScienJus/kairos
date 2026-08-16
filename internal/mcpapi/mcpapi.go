// Package mcpapi exposes the agent execution surface through MCP.
package mcpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxRequestBodyBytes = 1 << 20

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
	streamable := mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		actor, ok := identityFromContext(request)
		if !ok {
			return nil
		}
		return newServer(service, actor)
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

func newServer(service *application.Service, actor identity.Identity) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "kairos", Version: "v1"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "find_work",
		Title:       "Find Kairos work",
		Description: "Find pending tasks this actor may execute, plus empty blackboards that need planning.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input findWorkInput) (*mcp.CallToolResult, workCandidatesOutput, error) {
		candidates, err := service.FindWork(ctx, application.FindWorkQuery{
			Identity: actor,
			Tags:     input.Tags,
			Limit:    input.Limit,
		})
		return nil, workCandidatesOutput{Data: candidates}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task_context",
		Title:       "Get task context",
		Description: "Get the durable execution context for a task, including instructions, history, and coordination state.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input taskContextInput) (*mcp.CallToolResult, taskContextOutput, error) {
		result, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{
			TaskID:   domain.TaskID(input.TaskID),
			Identity: actor,
		})
		return nil, taskContextOutput{Data: result}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "claim_task",
		Title:       "Claim task",
		Description: "Atomically claim a pending task before starting work. Reuse operation_id when retrying the same call.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input claimTaskInput) (*mcp.CallToolResult, claimOutput, error) {
		claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{
			TaskID:      domain.TaskID(input.TaskID),
			Identity:    actor,
			OperationID: input.OperationID,
		})
		return nil, claimOutput{Data: claim}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_task",
		Title:       "Submit task result",
		Description: "Submit an immutable result from an active claim and optionally request review or choose a workflow transition.",
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
		return nil, submissionOutput{Data: submission}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "fail_task",
		Title:       "Report task failure",
		Description: "End an active claim by reopening the task for retry or failing the whole work item.",
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
		return nil, failureOutput{Data: failure}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "release_claim",
		Title:       "Release task claim",
		Description: "Release an active claim and return the task to the pending candidate set.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input releaseClaimInput) (*mcp.CallToolResult, releasedOutput, error) {
		err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{
			TaskID:      domain.TaskID(input.TaskID),
			ClaimID:     domain.ClaimID(input.ClaimID),
			Identity:    actor,
			OperationID: input.OperationID,
			Reason:      input.Reason,
		})
		return nil, releasedOutput{Released: err == nil}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_blackboard_task",
		Title:       "Create blackboard task",
		Description: "Plan one executable task in an open blackboard, including an empty blackboard returned by find_work.",
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
		return nil, taskOutput{Data: task}, err
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
	Reason      string `json:"reason" jsonschema:"Why the actor is giving up responsibility."`
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

type workCandidatesOutput struct {
	Data []application.WorkCandidate `json:"data"`
}

type taskContextOutput struct {
	Data application.TaskExecutionContext `json:"data"`
}

type claimOutput struct {
	Data domain.Claim `json:"data"`
}

type submissionOutput struct {
	Data domain.TaskSubmission `json:"data"`
}

type failureOutput struct {
	Data domain.TaskFailure `json:"data"`
}

type taskOutput struct {
	Data domain.Task `json:"data"`
}

type releasedOutput struct {
	Released bool `json:"released"`
}

func workflowTaskIDs(values []string) []domain.WorkflowTaskID {
	result := make([]domain.WorkflowTaskID, 0, len(values))
	for _, value := range values {
		result = append(result, domain.WorkflowTaskID(value))
	}
	return result
}
