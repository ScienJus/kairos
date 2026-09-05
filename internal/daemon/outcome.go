package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

type OutcomeKind string

const (
	Completed        OutcomeKind = "completed"
	Decomposed       OutcomeKind = "decomposed"
	RetryableFailure OutcomeKind = "retryable_failure"
	TerminalFailure  OutcomeKind = "terminal_failure"
	Abandoned        OutcomeKind = "abandoned"
	CreateTask       OutcomeKind = "create_task"
	SubmitCompletion OutcomeKind = "submit_completion"
	AcceptCompletion OutcomeKind = "accept_completion"
)

type TaskSpec struct {
	Title              string                     `json:"title"`
	Description        string                     `json:"description,omitempty"`
	AcceptanceCriteria string                     `json:"acceptance_criteria,omitempty"`
	Executor           domain.ExecutorRequirement `json:"executor"`
	AllowedRoles       []string                   `json:"allowed_roles"`
	Tags               []string                   `json:"tags"`
}

type Transition struct {
	ChoiceGroupID        domain.WorkflowChoiceGroupID `json:"choice_group_id"`
	SkipOptionalTaskIDs  []domain.WorkflowTaskID      `json:"skip_optional_task_ids"`
	ReviewSkippedTaskIDs []domain.WorkflowTaskID      `json:"review_skipped_task_ids"`
	Reason               string                       `json:"reason,omitempty"`
}

type TaskOutcome struct {
	Kind          OutcomeKind         `json:"kind"`
	Result        string              `json:"result,omitempty"`
	ArtifactIDs   []domain.ArtifactID `json:"artifact_ids"`
	RequestReview bool                `json:"request_review,omitempty"`
	Transition    *Transition         `json:"transition,omitempty"`
	Children      []TaskSpec          `json:"children"`
	Reason        string              `json:"reason,omitempty"`
	RetryPrompt   string              `json:"retry_prompt,omitempty"`
}

type CoordinationDecision struct {
	Kind   OutcomeKind `json:"kind"`
	Task   *TaskSpec   `json:"task,omitempty"`
	Result string      `json:"result,omitempty"`
}

// HarnessOutcome is a tagged union; exactly one member must match the Dispatch.
type HarnessOutcome struct {
	Task         *TaskOutcome          `json:"task,omitempty"`
	Coordination *CoordinationDecision `json:"coordination,omitempty"`
}

// DecodeOutcome is the strict JSON boundary available to concrete Adapters.
// Unknown fields and multiple JSON values are protocol errors, not ignored hints.
func DecodeOutcome(data []byte, candidate Candidate) (HarnessOutcome, error) {
	var outcome HarnessOutcome
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&outcome); err != nil {
		return HarnessOutcome{}, errors.New("invalid outcome JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return HarnessOutcome{}, errors.New("outcome must contain exactly one JSON value")
	}
	if err := outcome.Validate(candidate); err != nil {
		return HarnessOutcome{}, err
	}
	return outcome, nil
}

func (o HarnessOutcome) Kind() OutcomeKind {
	if o.Task != nil {
		return o.Task.Kind
	}
	if o.Coordination != nil {
		return o.Coordination.Kind
	}
	return ""
}

func (o HarnessOutcome) Validate(c Candidate) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Kind == TaskCandidate {
		if o.Task == nil || o.Coordination != nil {
			return errors.New("Task Dispatch requires TaskOutcome")
		}
		t := o.Task
		switch t.Kind {
		case Completed:
			if strings.TrimSpace(t.Result) == "" || len(t.Children) != 0 || t.Reason != "" || t.RetryPrompt != "" {
				return errors.New("invalid completed outcome")
			}
			if t.Transition != nil {
				if c.Mode != domain.CoordinationModeWorkflow {
					return errors.New("transition requires a Workflow Task")
				}
				if err := t.Transition.validate(); err != nil {
					return err
				}
			}
			seen := map[domain.ArtifactID]bool{}
			for _, id := range t.ArtifactIDs {
				if strings.TrimSpace(string(id)) == "" || seen[id] {
					return errors.New("invalid artifact ids")
				}
				seen[id] = true
			}
		case Decomposed:
			if c.Mode != domain.CoordinationModeBlackboard || len(t.Children) == 0 || t.Result != "" || len(t.ArtifactIDs) != 0 || t.RequestReview || t.Transition != nil || t.Reason != "" || t.RetryPrompt != "" {
				return errors.New("invalid decomposed outcome")
			}
			for _, child := range t.Children {
				if err := child.validate(); err != nil {
					return err
				}
			}
		case RetryableFailure, TerminalFailure, Abandoned:
			if t.Result != "" || len(t.ArtifactIDs) != 0 || t.RequestReview || t.Transition != nil || len(t.Children) != 0 {
				return errors.New("invalid failure or abandoned fields")
			}
			if t.Kind != Abandoned && strings.TrimSpace(t.Reason) == "" {
				return errors.New("business failure requires a reason")
			}
			if t.Kind != RetryableFailure && t.RetryPrompt != "" {
				return errors.New("retry prompt requires retryable_failure")
			}
		default:
			return errors.New("unsupported Task outcome")
		}
		reason := t.Reason
		if t.Kind == Abandoned {
			reason = strings.TrimSpace(reason)
		}
		return validateOutcomeText(strings.TrimSpace(t.Result), reason, t.RetryPrompt)
	}
	if o.Coordination == nil || o.Task != nil {
		return errors.New("Coordination Dispatch requires CoordinationDecision")
	}
	d := o.Coordination
	switch d.Kind {
	case CreateTask:
		if d.Task == nil || d.Result != "" {
			return errors.New("create_task requires only a Task spec")
		}
		return d.Task.validate()
	case SubmitCompletion:
		if c.Kind == WorkItemAcceptance || d.Task != nil || strings.TrimSpace(d.Result) == "" {
			return errors.New("invalid submit_completion decision")
		}
		return validateOutcomeText(strings.TrimSpace(d.Result))
	case AcceptCompletion:
		if c.Kind != WorkItemAcceptance || d.Task != nil || d.Result != "" {
			return errors.New("invalid accept_completion decision")
		}
	case Abandoned:
		if d.Task != nil || d.Result != "" {
			return errors.New("abandoned decision has no payload")
		}
	default:
		return errors.New("unsupported Coordination decision")
	}
	return nil
}

func (t TaskSpec) validate() error {
	if strings.TrimSpace(t.Title) == "" || !t.Executor.Valid() {
		return errors.New("Task spec requires title and executor")
	}
	if err := validateTaskSpecSet("allowed_roles", t.AllowedRoles); err != nil {
		return err
	}
	if err := validateTaskSpecSet("tags", t.Tags); err != nil {
		return err
	}
	if t.Executor == domain.ExecutorHuman && len(t.AllowedRoles) != 0 {
		return errors.New("human Task spec must not declare allowed_roles")
	}
	return nil
}

// Role/Tag values must already be canonical because discovery and authorization
// match the stored strings exactly. Reject malformed output before persistence.
func validateTaskSpecSet(field string, values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value != strings.TrimSpace(value) {
			return fmt.Errorf("%s values must not have surrounding whitespace", field)
		}
		if value == "" || seen[value] {
			return fmt.Errorf("%s must not contain empty or duplicate values", field)
		}
		seen[value] = true
	}
	return nil
}

func (t Transition) validate() error {
	if strings.TrimSpace(string(t.ChoiceGroupID)) == "" {
		return errors.New("transition requires a choice group")
	}
	intent := domain.WorkflowSkipIntent{SkipTaskIDs: t.SkipOptionalTaskIDs, ReviewRequestedTaskIDs: t.ReviewSkippedTaskIDs}
	if err := intent.Validate(); err != nil {
		return errors.New("invalid transition skip/review sets")
	}
	return validateOutcomeText(strings.TrimSpace(t.Reason))
}

func validateOutcomeText(values ...string) error {
	for _, value := range values {
		if len(value) > domain.MaxHistoryTextBytes {
			return errors.New("outcome history text exceeds the Core byte limit")
		}
	}
	return nil
}

// freezeOutcome prevents an Adapter from changing the selected intent after
// returning it, including slices and pointers inside the payload.
func freezeOutcome(o HarnessOutcome) (HarnessOutcome, error) {
	data, err := json.Marshal(o)
	if err != nil {
		return HarnessOutcome{}, fmt.Errorf("encode outcome: %w", err)
	}
	var copy HarnessOutcome
	err = json.Unmarshal(data, &copy)
	return copy, err
}
