package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

type DiscoveredCandidate struct {
	Candidate  Candidate
	Generation string
}

type DiscoveryCore interface {
	Core
	// Timeout applies to each request, not the complete discovery batch.
	// Resolved candidates may accompany a later error; cancellation and
	// authorization failures must prevent admission of that partial result.
	Discover(context.Context, []string, int, time.Duration) ([]DiscoveredCandidate, error)
}

// Discover uses Core's bounded per-kind snapshot, not a global fair queue.
func (c *HTTPClient) Discover(ctx context.Context, tags []string, limit int, timeout time.Duration) ([]DiscoveredCandidate, error) {
	if timeout <= 0 {
		return nil, errors.New("discovery request timeout must be positive")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	for _, tag := range tags {
		query.Add("tag", tag)
	}
	var rows []struct {
		Kind     CandidateKind   `json:"kind"`
		WorkItem domain.WorkItem `json:"work_item"`
		Task     *domain.Task    `json:"task"`
	}
	call, cancel := context.WithTimeout(ctx, timeout)
	err := c.request(call, http.MethodGet, "/work?"+query.Encode(), "", nil, &rows)
	cancel()
	if err != nil {
		return nil, err
	}
	result := make([]DiscoveredCandidate, 0, len(rows))
	contexts := make(map[domain.WorkItemID]workContext)
	for _, row := range rows {
		candidate := Candidate{Kind: row.Kind, WorkItemID: row.WorkItem.ID, Mode: row.WorkItem.CoordinationMode()}
		if row.Task != nil {
			candidate.TaskID = row.Task.ID
		}
		if err := candidate.Validate(); err != nil {
			return nil, err
		}
		view, ok := contexts[candidate.WorkItemID]
		if !ok {
			var err error
			call, cancel := context.WithTimeout(ctx, timeout)
			view, err = c.context(call, candidate)
			cancel()
			if statusIs(err, 404, 409) {
				continue
			}
			if err != nil {
				return result, err
			}
			contexts[candidate.WorkItemID] = view
		}
		generation, err := candidateGeneration(candidate, view)
		if err != nil {
			return nil, err
		}
		result = append(result, DiscoveredCandidate{candidate, generation})
	}
	return result, nil
}

// Business history is retained; operational Claim churn is intentionally erased.
// Shared WorkItem context changes invalidate all affected local candidate records.
func candidateGeneration(candidate Candidate, view workContext) (string, error) {
	view.WorkItem.Version = 0
	view.WorkItem.UpdatedAt = time.Time{}
	view.Claims = nil
	view.CoordinationClaims = nil
	view.Tasks = append([]domain.Task{}, view.Tasks...)
	view.Relations = append([]domain.TaskRelation{}, view.Relations...)
	view.Artifacts = append([]domain.Artifact{}, view.Artifacts...)
	for i := range view.Tasks {
		task := &view.Tasks[i]
		task.Version = 0
		task.UpdatedAt = time.Time{}
		task.ActiveClaimID = nil
		if task.Status == domain.TaskStatusWorking {
			task.Status = domain.TaskStatusPending
		}
	}
	sort.Slice(view.Tasks, func(i, j int) bool { return view.Tasks[i].ID < view.Tasks[j].ID })
	sort.Slice(view.Artifacts, func(i, j int) bool { return view.Artifacts[i].ID < view.Artifacts[j].ID })
	sort.Slice(view.Relations, func(i, j int) bool {
		a, b := view.Relations[i], view.Relations[j]
		if a.FromTaskID != b.FromTaskID {
			return a.FromTaskID < b.FromTaskID
		}
		return a.ToTaskID < b.ToTaskID
	})
	data, err := json.Marshal(struct {
		Candidate Candidate
		Context   workContext
	}{candidate, view})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
