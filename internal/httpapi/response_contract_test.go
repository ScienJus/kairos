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
	workItemBody := rawTrustedResponse(t, server.Client(), http.MethodGet, server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/context", http.StatusOK)
	var workItemEnvelope map[string]any
	if err := json.Unmarshal(workItemBody, &workItemEnvelope); err != nil {
		t.Fatalf("decode WorkItem context: %v", err)
	}
	workItemData := workItemEnvelope["data"].(map[string]any)["work_item"].(map[string]any)
	assertJSONKeys(t, workItemData,
		[]string{"cancelled_at", "cancelled_by", "cancellation_reason"},
		[]string{"cancelledat", "cancelledby", "cancellationreason"},
	)
	if workItemData["cancelled_at"] != nil || workItemData["cancelled_by"] != nil || workItemData["cancellation_reason"] != "" {
		t.Fatalf("open WorkItem cancellation metadata = %#v, want null/null/empty", workItemData)
	}
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

	cancelledBody := rawTrustedJSONResponse(t, server.Client(), http.MethodPost,
		server.URL+"/api/v1/work-items/"+string(workItem.ID)+"/cancellation",
		map[string]any{"reason": "No longer required"}, "cancel-contract-work-item", http.StatusOK,
		trustedTestIdentity{ID: "contract-reviewer", Kind: domain.ActorHuman},
	)
	var cancelledEnvelope map[string]any
	if err := json.Unmarshal(cancelledBody, &cancelledEnvelope); err != nil {
		t.Fatalf("decode cancelled WorkItem: %v", err)
	}
	cancelledData := cancelledEnvelope["data"].(map[string]any)
	assertJSONKeys(t, cancelledData,
		[]string{"cancelled_at", "cancelled_by", "cancellation_reason"},
		[]string{"cancelledat", "cancelledby", "cancellationreason"},
	)
	cancelledBy, actorOK := cancelledData["cancelled_by"].(map[string]any)
	if cancelledAt, timeOK := cancelledData["cancelled_at"].(string); !timeOK || cancelledAt == "" ||
		!actorOK || cancelledBy["kind"] != "human" || cancelledBy["id"] != "contract-reviewer" ||
		cancelledData["cancellation_reason"] != "No longer required" {
		t.Fatalf("cancelled WorkItem metadata = %#v", cancelledData)
	}
}

func rawTrustedResponse(t *testing.T, client *http.Client, method, url string, wantStatus int) []byte {
	t.Helper()
	return rawTrustedJSONResponse(t, client, method, url, nil, "", wantStatus, trustedTestIdentity{ID: "contract-reviewer", Kind: domain.ActorHuman})
}

func rawTrustedJSONResponse(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	requestBody any,
	operationID string,
	wantStatus int,
	actor trustedTestIdentity,
) []byte {
	t.Helper()
	request := newTrustedRequest(t, method, url, requestBody, operationID, actor)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	defer response.Body.Close()
	var responseBody json.RawMessage
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&responseBody); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("response status = %d, want %d: %s", response.StatusCode, wantStatus, responseBody)
	}
	return append(responseBody, '\n')
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
