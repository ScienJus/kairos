package domain

import (
	"strings"
	"time"
)

// WorkflowActivationStatus is the lifecycle state of a Workflow node activation.
type WorkflowActivationStatus string

const (
	WorkflowActivationWaiting  WorkflowActivationStatus = "waiting"
	WorkflowActivationResolved WorkflowActivationStatus = "resolved"
)

// Valid reports whether the activation status is recognized.
func (s WorkflowActivationStatus) Valid() bool {
	return s == WorkflowActivationWaiting || s == WorkflowActivationResolved
}

// WorkflowActivationOutcome records how a resolved activation materialized its Task.
type WorkflowActivationOutcome string

const (
	WorkflowActivationCreated WorkflowActivationOutcome = "created"
	WorkflowActivationSkipped WorkflowActivationOutcome = "skipped"
	WorkflowActivationReview  WorkflowActivationOutcome = "review"
)

// Valid reports whether the activation outcome is recognized.
func (o WorkflowActivationOutcome) Valid() bool {
	return o == WorkflowActivationCreated || o == WorkflowActivationSkipped || o == WorkflowActivationReview
}

// WorkflowActivationInputOutcome records one predecessor's applied decision.
type WorkflowActivationInputOutcome string

const (
	WorkflowActivationInputTriggered WorkflowActivationInputOutcome = "triggered"
	WorkflowActivationInputSkipped   WorkflowActivationInputOutcome = "skipped"
)

// Valid reports whether the input outcome is recognized.
func (o WorkflowActivationInputOutcome) Valid() bool {
	return o == WorkflowActivationInputTriggered || o == WorkflowActivationInputSkipped
}

// WorkflowActivationInput reserves one concrete incoming Workflow relation.
// Decision fields remain empty until that predecessor's decision is applied.
type WorkflowActivationInput struct {
	RelationID WorkflowRelationID

	SourceTaskID    *TaskID
	DecisionID      *TransitionDecisionID
	Outcome         WorkflowActivationInputOutcome
	ReviewRequested bool
	ResolvedAt      *time.Time
}

// Resolved reports whether an applied decision has filled this input.
func (i WorkflowActivationInput) Resolved() bool {
	return i.DecisionID != nil
}

// Validate checks one activation input.
func (i WorkflowActivationInput) Validate() error {
	if strings.TrimSpace(string(i.RelationID)) == "" {
		return invalid("workflow_activation.inputs.relation_id", "is required")
	}
	if !i.Resolved() {
		if i.SourceTaskID != nil || i.Outcome != "" || i.ReviewRequested || i.ResolvedAt != nil {
			return invalid("workflow_activation.inputs", "unresolved input %q must not contain decision fields", i.RelationID)
		}
		return nil
	}
	if strings.TrimSpace(string(*i.DecisionID)) == "" {
		return invalid("workflow_activation.inputs.decision_id", "must not be empty")
	}
	if i.SourceTaskID == nil || strings.TrimSpace(string(*i.SourceTaskID)) == "" {
		return invalid("workflow_activation.inputs.source_task_id", "is required after resolution")
	}
	if !i.Outcome.Valid() {
		return invalid("workflow_activation.inputs.outcome", "unsupported value %q", i.Outcome)
	}
	if i.ReviewRequested && i.Outcome != WorkflowActivationInputSkipped {
		return invalid("workflow_activation.inputs.review_requested", "is supported only for skipped inputs")
	}
	if i.ResolvedAt == nil || i.ResolvedAt.IsZero() {
		return invalid("workflow_activation.inputs.resolved_at", "is required after resolution")
	}
	return nil
}

// WorkflowTaskActivation correlates the concrete inputs that may produce one Task.
// It is internal Workflow runtime state and is never claimable by an executor.
type WorkflowTaskActivation struct {
	ID             WorkflowTaskActivationID
	WorkItemID     WorkItemID
	WorkflowTaskID WorkflowTaskID
	CorrelationID  WorkflowCorrelationID

	// ParentCorrelationIDs is the outer correlation stack. Entering a nested
	// cycle pushes the current correlation; exiting it restores the last parent.
	ParentCorrelationIDs []WorkflowCorrelationID

	Inputs []WorkflowActivationInput

	Status  WorkflowActivationStatus
	Outcome WorkflowActivationOutcome

	CreatedAt  time.Time
	UpdatedAt  time.Time
	ResolvedAt *time.Time
}

// Validate checks the WorkflowTaskActivation invariants.
func (a WorkflowTaskActivation) Validate() error {
	if strings.TrimSpace(string(a.ID)) == "" {
		return invalid("workflow_activation.id", "is required")
	}
	if strings.TrimSpace(string(a.WorkItemID)) == "" {
		return invalid("workflow_activation.work_item_id", "is required")
	}
	if strings.TrimSpace(string(a.WorkflowTaskID)) == "" {
		return invalid("workflow_activation.workflow_task_id", "is required")
	}
	if strings.TrimSpace(string(a.CorrelationID)) == "" {
		return invalid("workflow_activation.correlation_id", "is required")
	}
	seenCorrelations := map[WorkflowCorrelationID]struct{}{a.CorrelationID: {}}
	for _, correlationID := range a.ParentCorrelationIDs {
		if strings.TrimSpace(string(correlationID)) == "" {
			return invalid("workflow_activation.parent_correlation_ids", "must not contain empty values")
		}
		if _, exists := seenCorrelations[correlationID]; exists {
			return invalid("workflow_activation.parent_correlation_ids", "contains duplicate correlation %q", correlationID)
		}
		seenCorrelations[correlationID] = struct{}{}
	}
	if !a.Status.Valid() {
		return invalid("workflow_activation.status", "unsupported value %q", a.Status)
	}
	if err := validateTimestamps(a.CreatedAt, a.UpdatedAt); err != nil {
		return err
	}

	seenRelations := make(map[WorkflowRelationID]struct{}, len(a.Inputs))
	allInputsResolved := true
	for _, input := range a.Inputs {
		if err := input.Validate(); err != nil {
			return err
		}
		if _, exists := seenRelations[input.RelationID]; exists {
			return invalid("workflow_activation.inputs", "contains duplicate relation %q", input.RelationID)
		}
		seenRelations[input.RelationID] = struct{}{}
		allInputsResolved = allInputsResolved && input.Resolved()
	}

	if a.Status == WorkflowActivationWaiting {
		if len(a.Inputs) == 0 {
			return invalid("workflow_activation.inputs", "waiting activation requires at least one input")
		}
		if allInputsResolved {
			return invalid("workflow_activation.status", "must be resolved after all inputs resolve")
		}
		if a.Outcome != "" || a.ResolvedAt != nil {
			return invalid("workflow_activation.resolution", "must be empty while waiting")
		}
		return nil
	}

	if !allInputsResolved {
		return invalid("workflow_activation.inputs", "all inputs must resolve before the activation")
	}
	if !a.Outcome.Valid() {
		return invalid("workflow_activation.outcome", "a valid outcome is required after resolution")
	}
	if a.ResolvedAt == nil || a.ResolvedAt.IsZero() {
		return invalid("workflow_activation.resolved_at", "is required after resolution")
	}
	if a.ResolvedAt.Before(a.CreatedAt) {
		return invalid("workflow_activation.resolved_at", "must not be before created_at")
	}
	return nil
}
