package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

type timedDiscoveryTransport struct {
	responses map[string][]byte
	calls     int
}

func (r *timedDiscoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-time.After(40 * time.Millisecond):
	}
	data, ok := r.responses[req.URL.Path]
	if !ok || req.Method != http.MethodGet {
		return nil, fmt.Errorf("unexpected discovery route: %s", req.URL.Path)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), Request: req}, nil
}

type timedDiscoveryCore struct {
	*schedulerTestCore
	discovery *HTTPClient
	resolved  int
}

func (c *timedDiscoveryCore) Discover(ctx context.Context, tags []string, limit int, timeout time.Duration) ([]DiscoveredCandidate, error) {
	rows, err := c.discovery.Discover(ctx, tags, limit, timeout)
	c.resolved = len(rows)
	return rows, err
}

func TestSchedulerDiscoveryHasIndependentRequestBudgets(t *testing.T) {
	// Seed transport responses from a valid real SQL/HTTP execution context,
	// then isolate timeout assertions from wall-clock and filesystem overhead.
	f := newHTTPFixture(t, domain.CoordinationModeBlackboard, TaskCandidate)
	view, err := f.client.context(context.Background(), f.candidate)
	if err != nil {
		t.Fatal(err)
	}
	responses := make(map[string][]byte)
	rows := make([]map[string]any, 0, 3)
	for i := range 3 {
		view.WorkItem.ID = domain.WorkItemID(fmt.Sprintf("discovery-work-%d", i))
		view.Tasks[0].ID = domain.TaskID(fmt.Sprintf("discovery-task-%d", i))
		view.Tasks[0].WorkItemID = view.WorkItem.ID
		data, err := json.Marshal(map[string]any{"data": view})
		if err != nil {
			t.Fatal(err)
		}
		responses["/api/v1/work-items/"+string(view.WorkItem.ID)+"/context"] = data
		rows = append(rows, map[string]any{"kind": TaskCandidate, "work_item": view.WorkItem, "task": view.Tasks[0]})
	}
	responses["/api/v1/work"], err = json.Marshal(map[string]any{"data": rows})
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		transport := &timedDiscoveryTransport{responses: responses}
		client, err := NewHTTPClient("http://core.test", NewSecret("test"), &http.Client{Transport: transport})
		if err != nil {
			t.Fatal(err)
		}
		core, adapter, options := schedulerFixture(t)
		options.Dispatch.RequestTimeout = 120 * time.Millisecond
		combined := &timedDiscoveryCore{schedulerTestCore: core, discovery: client}
		s, err := NewScheduler(combined, adapter, options)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		s.admit(context.Background(), func(*scheduledRun) {})
		if elapsed := time.Since(started); elapsed != 160*time.Millisecond {
			t.Fatalf("expected four independent 40ms requests, got %s", elapsed)
		}
		if transport.calls != 4 || combined.resolved != 3 || s.Stats().Claims != 1 {
			t.Fatalf("discovery stalled: calls=%d resolved=%d stats=%+v", transport.calls, combined.resolved, s.Stats())
		}
	})
}
