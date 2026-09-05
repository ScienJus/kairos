package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

const maxCoreResponseBytes = 64 << 20

// HTTPClient holds only the Daemon's ordinary Identity credential. Executor
// credentials are passed to Claim explicitly and never used for lifecycle calls.
type HTTPClient struct {
	base  string
	token Secret
	http  *http.Client
}

func NewHTTPClient(base string, token Secret, client *http.Client) (*HTTPClient, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("Core URL must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if token.Reveal() == "" || strings.ContainsAny(token.Reveal(), " \t\r\n") {
		return nil, errors.New("Identity Token is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	copy := *client
	// A redirect must never turn a lifecycle POST into a GET or forward secrets.
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPClient{base: strings.TrimRight(base, "/"), token: token, http: &copy}, nil
}

func (c *HTTPClient) request(ctx context.Context, method, path, operation string, body, result any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return errors.New("cannot encode Core request")
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, reader)
	if err != nil {
		return errors.New("cannot construct Core request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
	req.Header.Set("Content-Type", "application/json")
	if operation != "" {
		req.Header.Set("Idempotency-Key", operation)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Core transport failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoreResponseBytes+1))
	if err != nil {
		return errors.New("cannot read Core response")
	}
	if len(data) > maxCoreResponseBytes {
		return errors.New("Core response exceeds size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &envelope)
		return &APIError{Status: resp.StatusCode, Code: envelope.Error.Code}
	}
	if resp.StatusCode == http.StatusNoContent && result == nil {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("invalid Core response envelope")
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Data, result); err != nil {
			return errors.New("invalid Core response data")
		}
	}
	return nil
}

func taskPath(c Candidate) string { return "/tasks/" + url.PathEscape(string(c.TaskID)) }
func workPath(c Candidate) string { return "/work-items/" + url.PathEscape(string(c.WorkItemID)) }
func claimPath(c Candidate, id string) string {
	if c.Kind == TaskCandidate {
		return taskPath(c) + "/claims/" + url.PathEscape(id)
	}
	return workPath(c) + "/coordination-claims/" + url.PathEscape(id)
}

type workContext struct {
	WorkItem           domain.WorkItem            `json:"work_item"`
	Tasks              []domain.Task              `json:"tasks"`
	Claims             []domain.Claim             `json:"claims"`
	CoordinationClaims []domain.CoordinationClaim `json:"coordination_claims"`
	Artifacts          []domain.Artifact          `json:"artifacts"`
}

func (c *HTTPClient) context(ctx context.Context, candidate Candidate) (workContext, error) {
	var view workContext
	if err := c.request(ctx, http.MethodGet, workPath(candidate)+"/context", "", nil, &view); err != nil {
		return view, err
	}
	if view.WorkItem.ID != candidate.WorkItemID || view.WorkItem.CoordinationMode() != candidate.Mode {
		return view, &APIError{Status: 400, Code: "candidate_scope_mismatch"}
	}
	if candidate.Kind == TaskCandidate {
		for _, task := range view.Tasks {
			if task.ID == candidate.TaskID && task.WorkItemID == candidate.WorkItemID {
				return view, nil
			}
		}
		return view, &APIError{Status: 404, Code: "task_not_in_work_item"}
	}
	return view, nil
}

func (c *HTTPClient) Claim(ctx context.Context, candidate Candidate, operation string, token Secret, lease int64) (Claim, error) {
	if err := candidate.Validate(); err != nil {
		return Claim{}, &ClaimAttemptError{State: ClaimNotSent, Err: err}
	}
	if _, err := c.context(ctx, candidate); err != nil {
		return Claim{}, &ClaimAttemptError{State: ClaimNotSent, Err: err}
	}
	body := map[string]any{"executor_token": token.Reveal(), "lease_seconds": lease}
	if candidate.Kind == TaskCandidate {
		var claim domain.Claim
		err := c.request(ctx, http.MethodPost, taskPath(candidate)+"/claims", operation, body, &claim)
		if err != nil {
			return Claim{}, claimMutationError(err)
		}
		if claim.TaskID != candidate.TaskID {
			return Claim{}, errors.New("Core Task Claim scope mismatch")
		}
		return taskClaim(claim), nil
	}
	body["kind"] = candidate.Kind
	var claim domain.CoordinationClaim
	err := c.request(ctx, http.MethodPost, workPath(candidate)+"/coordination-claims", operation, body, &claim)
	if err != nil {
		return Claim{}, claimMutationError(err)
	}
	if claim.WorkItemID != candidate.WorkItemID || string(claim.Kind) != string(candidate.Kind) {
		return Claim{}, errors.New("Core Coordination Claim scope mismatch")
	}
	return coordinationClaim(claim), nil
}

func claimMutationError(err error) error {
	var api *APIError
	// Core evaluates Claim eligibility after serializing the idempotency key;
	// an earlier successful request would have replayed instead of conflicting.
	// Authentication errors, gateway responses and transport failures do not
	// establish this fact and must preserve any prior uncertainty.
	if errors.As(err, &api) && api.Status == http.StatusConflict && api.Code == "conflict" {
		return &ClaimAttemptError{State: ClaimRejected, Err: err}
	}
	return err
}

func taskClaim(c domain.Claim) Claim {
	return Claim{ID: string(c.ID), Executor: c.Executor, LeaseSeconds: c.LeaseSeconds, Active: c.Active(), EndReason: string(c.EndReason), EndedAt: c.EndedAt}
}
func coordinationClaim(c domain.CoordinationClaim) Claim {
	return Claim{ID: string(c.ID), Executor: c.Executor, LeaseSeconds: c.LeaseSeconds, Active: c.Active(), EndReason: string(c.EndReason), EndedAt: c.EndedAt}
}

func (c *HTTPClient) Heartbeat(ctx context.Context, candidate Candidate, id string, lease int64) (Claim, error) {
	body := map[string]any{"lease_seconds": lease}
	if candidate.Kind == TaskCandidate {
		var claim domain.Claim
		if err := c.request(ctx, http.MethodPost, claimPath(candidate, id)+"/heartbeat", "", body, &claim); err != nil {
			return Claim{}, err
		}
		if claim.TaskID != candidate.TaskID {
			return Claim{}, errors.New("Core heartbeat scope mismatch")
		}
		return taskClaim(claim), nil
	}
	var claim domain.CoordinationClaim
	if err := c.request(ctx, http.MethodPost, claimPath(candidate, id)+"/heartbeat", "", body, &claim); err != nil {
		return Claim{}, err
	}
	if claim.WorkItemID != candidate.WorkItemID || string(claim.Kind) != string(candidate.Kind) {
		return Claim{}, errors.New("Core heartbeat scope mismatch")
	}
	return coordinationClaim(claim), nil
}

func (c *HTTPClient) Inspect(ctx context.Context, candidate Candidate, id string) (ClaimStatus, error) {
	view, err := c.context(ctx, candidate)
	if err != nil {
		return ClaimStatus{}, err
	}
	if candidate.Kind == TaskCandidate {
		for _, claim := range view.Claims {
			if string(claim.ID) != id || claim.TaskID != candidate.TaskID {
				continue
			}
			for _, task := range view.Tasks {
				if task.ID == candidate.TaskID {
					return ClaimStatus{Claim: taskClaim(claim), Task: &task, Artifacts: view.Artifacts, Tasks: view.Tasks, WorkItemResult: view.WorkItem.Result}, nil
				}
			}
		}
	} else {
		for _, claim := range view.CoordinationClaims {
			if string(claim.ID) == id && claim.WorkItemID == candidate.WorkItemID && string(claim.Kind) == string(candidate.Kind) {
				return ClaimStatus{Claim: coordinationClaim(claim), Tasks: view.Tasks, WorkItemResult: view.WorkItem.Result}, nil
			}
		}
	}
	return ClaimStatus{}, errors.New("bound Claim absent from Core history")
}

func (c *HTTPClient) Release(ctx context.Context, candidate Candidate, id, reason string) error {
	var body any
	if candidate.Kind == TaskCandidate {
		body = map[string]any{"reason": reason}
	}
	return c.request(ctx, http.MethodDelete, claimPath(candidate, id), "", body, nil)
}

func (c *HTTPClient) Apply(ctx context.Context, candidate Candidate, id, operation string, outcome HarnessOutcome) error {
	if err := outcome.Validate(candidate); err != nil {
		return fmt.Errorf("invalid Harness outcome: %w", err)
	}
	body := map[string]any{}
	path := ""
	if t := outcome.Task; t != nil {
		body["claim_id"] = id
		switch t.Kind {
		case Completed:
			path = taskPath(candidate) + "/submissions"
			body["result"], body["artifact_ids"], body["request_review"], body["transition"] = t.Result, requestArray(t.ArtifactIDs), t.RequestReview, transitionRequest(t.Transition)
		case Decomposed:
			path = taskPath(candidate) + "/decomposition"
			children := make([]TaskSpec, len(t.Children))
			for i, child := range t.Children {
				children[i] = taskSpecRequest(child)
			}
			body["children"] = children
		case RetryableFailure, TerminalFailure:
			path = taskPath(candidate) + "/failures"
			body["action"] = failureAction(t.Kind)
			body["reason"], body["retry_prompt"] = t.Reason, t.RetryPrompt
		case Abandoned:
			return c.Release(ctx, candidate, id, t.Reason)
		}
	} else {
		d := outcome.Coordination
		body["coordination_claim_id"] = id
		switch d.Kind {
		case CreateTask:
			path = workPath(candidate) + "/tasks"
			t := taskSpecRequest(*d.Task)
			body["title"], body["description"], body["acceptance_criteria"] = t.Title, t.Description, t.AcceptanceCriteria
			body["executor"], body["allowed_roles"], body["tags"] = t.Executor, t.AllowedRoles, t.Tags
		case SubmitCompletion:
			path = workPath(candidate) + "/completion"
			body["result"] = d.Result
		case AcceptCompletion:
			path = workPath(candidate) + "/acceptance"
		case Abandoned:
			return c.Release(ctx, candidate, id, "")
		}
	}
	return c.request(ctx, http.MethodPost, path, operation, body, nil)
}

// Normalize only the outbound representation, preserving the frozen intent.
func requestArray[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func taskSpecRequest(spec TaskSpec) TaskSpec {
	spec.AllowedRoles, spec.Tags = requestArray(spec.AllowedRoles), requestArray(spec.Tags)
	return spec
}

func transitionRequest(transition *Transition) *Transition {
	if transition == nil {
		return nil
	}
	copy := *transition
	copy.SkipOptionalTaskIDs, copy.ReviewSkippedTaskIDs = requestArray(copy.SkipOptionalTaskIDs), requestArray(copy.ReviewSkippedTaskIDs)
	return &copy
}

func failureAction(kind OutcomeKind) domain.TaskFailureAction {
	if kind == RetryableFailure {
		return domain.TaskFailureReopen
	}
	return domain.TaskFailureFailWorkItem
}
