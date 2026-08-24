package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/httpapi"
	"github.com/ScienJus/kairos/internal/identity"
	"github.com/ScienJus/kairos/internal/repository"
)

func TestHTTPResponsesUseSnakeCaseAndPreserveEmptyValues(t *testing.T) {
	ctx := context.Background()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(t.TempDir(), "kairos.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	service, err := application.NewService(repo, endToEndClock{}, &endToEndIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := httpapi.New(service, identity.TrustedResolver{})
	if err != nil {
		t.Fatalf("new HTTP API: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	empty := rawTrustedResponse(t, server.Client(), http.MethodGet, server.URL+"/api/v1/work-items", http.StatusOK)
	if string(empty) != "{\"data\":[]}\n" {
		t.Fatalf("empty collection response = %s", empty)
	}

	requestData[domain.BlackboardDefinition](t, server.Client(), http.MethodPost, server.URL+"/api/v1/definitions/blackboards", map[string]any{
		"id": "contract", "version": 1, "name": "Contract", "status": "published", "suggested_tags": []string{},
	}, "create-contract-definition", http.StatusCreated)
	workItem := requestData[domain.WorkItem](t, server.Client(), http.MethodPost, server.URL+"/api/v1/work-items", map[string]any{
		"definition_id": "contract", "mode": "blackboard", "title": "Check contract", "goal": "Keep the API stable", "tags": []string{},
	}, "create-contract-work-item", http.StatusCreated)
	task := requestData[domain.Task](t, server.Client(), http.MethodPost, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/tasks", map[string]any{
		"title": "Inspect response", "executor": "human", "allowed_roles": []string{}, "tags": []string{},
	}, "create-contract-task", http.StatusCreated)

	body := rawTrustedResponse(t, server.Client(), http.MethodGet, server.URL+"/api/v1/tasks/"+string(task.ID), http.StatusOK)
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode Task Detail: %v", err)
	}
	assertSnakeCaseJSONKeys(t, envelope, "response")
	data := envelope["data"].(map[string]any)
	taskData := data["task"].(map[string]any)
	assertJSONKeys(t, taskData,
		[]string{"work_item_id", "workflow_task_id", "active_claim_id", "transition_decisions"},
		[]string{"workitemid", "workflowtaskid", "activeclaimid", "transitiondecisions"},
	)
	assertJSONKeys(t, data,
		[]string{"current_review"},
		[]string{"currentreview"},
	)
	assertJSONKeys(t, data["history"].(map[string]any),
		[]string{"transition_decisions"},
		[]string{"transitiondecisions"},
	)
	assertJSONKeys(t, data["capabilities"].(map[string]any),
		[]string{"can_claim"},
		[]string{"canclaim"},
	)
	for _, field := range []string{"reviews", "submissions", "failures", "transition_decisions"} {
		if values, ok := taskData[field].([]any); !ok || len(values) != 0 {
			t.Fatalf("task.%s = %#v, want []", field, taskData[field])
		}
	}
	if data["current_review"] != nil {
		t.Fatalf("current_review = %#v, want null", data["current_review"])
	}
	if values, ok := data["artifacts"].([]any); !ok || len(values) != 0 {
		t.Fatalf("artifacts = %#v, want []", data["artifacts"])
	}
}

func rawTrustedResponse(t *testing.T, client *http.Client, method, url string, wantStatus int) []byte {
	t.Helper()
	request := newTrustedRequest(t, method, url, nil, "", trustedTestIdentity{ID: "contract-reviewer", Kind: domain.ActorHuman})
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	defer response.Body.Close()
	var body json.RawMessage
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&body); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("response status = %d, want %d: %s", response.StatusCode, wantStatus, body)
	}
	return append(body, '\n')
}

var snakeCaseJSONKey = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

func assertSnakeCaseJSONKeys(t *testing.T, value any, path string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if !snakeCaseJSONKey.MatchString(key) {
				t.Errorf("%s contains non-snake-case key %q", path, key)
			}
			assertSnakeCaseJSONKeys(t, child, path+"."+key)
		}
	case []any:
		for _, child := range typed {
			assertSnakeCaseJSONKeys(t, child, path+"[]")
		}
	}
}

func assertJSONKeys(t *testing.T, object map[string]any, present, absent []string) {
	t.Helper()
	for _, key := range present {
		if _, ok := object[key]; !ok {
			t.Errorf("JSON object is missing key %q", key)
		}
	}
	for _, key := range absent {
		if _, ok := object[key]; ok {
			t.Errorf("JSON object contains concatenated key %q", key)
		}
	}
}
