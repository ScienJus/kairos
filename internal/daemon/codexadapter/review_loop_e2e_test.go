//go:build darwin || linux

package codexadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/daemon"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Exercise real MCP, scoped uploads and the production Dispatch finalizer.
// This deterministic Harness deliberately asks for one Human recovery and one
// draft revision; the live-model example does not force a Checker verdict.
func helperReviewLoop(prompt []byte, output string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	meta := map[string]string{}
	for _, word := range strings.Fields(string(prompt)) {
		if key, value, ok := strings.Cut(word, "="); ok {
			meta[key] = strings.TrimSuffix(value, ".")
		}
	}
	endpoint := ""
	for _, arg := range os.Args {
		if value, ok := strings.CutPrefix(arg, "mcp_servers.kairos.url="); ok {
			endpoint, _ = strconv.Unquote(value)
		}
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: authTransport{os.Getenv(executorEnv)}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "review-loop-fixture", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	call := func(name string, args, out any) error {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			return err
		}
		if result.IsError {
			return fmt.Errorf("fixture tool %s failed", name)
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, out)
	}
	var view struct {
		Task struct {
			WorkflowTaskID string                              `json:"workflow_task_id"`
			Description    string                              `json:"description"`
			Reviews        []struct{ Status, Feedback string } `json:"reviews"`
		} `json:"task"`
		InputArtifacts []application.ArtifactSubmission `json:"input_artifacts"`
		Workflow       struct {
			ChoiceGroups []struct {
				ID string `json:"id"`
			} `json:"choice_groups"`
		} `json:"workflow"`
	}
	if err := call("get_task_context", map[string]string{"task_id": meta["task_id"]}, &view); err != nil {
		return err
	}
	download := func(id domain.ArtifactID) ([]byte, error) {
		response, err := client.Get(strings.TrimSuffix(endpoint, "/mcp") + "/api/v1/artifacts/" + url.PathEscape(string(id)) + "/content")
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, errors.New("fixture artifact unavailable")
		}
		return io.ReadAll(io.LimitReader(response.Body, 1<<20))
	}
	input := func() (domain.Artifact, []byte, error) {
		if len(view.InputArtifacts) != 1 || len(view.InputArtifacts[0].Artifacts) != 1 {
			return domain.Artifact{}, nil, errors.New("expected one exact input")
		}
		artifact := view.InputArtifacts[0].Artifacts[0]
		body, err := download(artifact.ID)
		return artifact, body, err
	}
	outcome := daemon.TaskOutcome{Kind: daemon.Completed, ArtifactIDs: []domain.ArtifactID{}, Children: []daemon.TaskSpec{}}
	name, content, choice := "guide", "", ""
	switch view.Task.WorkflowTaskID {
	case "draft":
		content = "# Guide v1\nDraft without restore verification.\n"
		if len(view.InputArtifacts) > 0 {
			_, body, err := input()
			if err != nil {
				return err
			}
			var review struct {
				GuideID domain.ArtifactID `json:"guide_artifact_id"`
				Verdict string            `json:"verdict"`
			}
			if err := json.Unmarshal(body, &review); err != nil {
				return err
			}
			previous, err := download(review.GuideID)
			if err != nil {
				return err
			}
			if review.Verdict != "revise" || !strings.Contains(string(previous), "Guide v1") {
				return errors.New("revision lost reviewed version")
			}
			content = "# Guide v2\nStop Core, back up SQLite and Artifact storage together, restore in isolation and verify content.\n"
		}
		choice = "continue:draft-check"
	case "check":
		guide, body, err := input()
		if err != nil {
			return err
		}
		if !strings.Contains(view.Task.Description, "Retry of Task") && strings.Contains(string(body), "Guide v1") {
			outcome.Kind = daemon.HumanInterventionRequired
			outcome.Reason = "Fixture requires an operator decision before checking the initial guide."
			break
		}
		verdict, feedback := "ready", []string{}
		choice = "exit:check"
		if strings.Contains(string(body), "Guide v1") {
			verdict, feedback, choice = "revise", []string{"Include a restore verification procedure."}, "continue:check-draft"
		}
		encoded, err := json.Marshal(map[string]any{"guide_artifact_id": guide.ID, "verdict": verdict, "feedback": feedback})
		if err != nil {
			return err
		}
		name, content = "review", string(encoded)
	case "publish":
		_, body, err := input()
		if err != nil {
			return err
		}
		var review struct {
			GuideID domain.ArtifactID `json:"guide_artifact_id"`
			Verdict string            `json:"verdict"`
		}
		if err := json.Unmarshal(body, &review); err != nil {
			return err
		}
		guide, err := download(review.GuideID)
		if err != nil {
			return err
		}
		if review.Verdict != "ready" || !strings.Contains(string(guide), "Guide v2") {
			return errors.New("publication lost checked version")
		}
		content = string(guide)
		for _, rejected := range view.Task.Reviews {
			if rejected.Status == "rejected" {
				content += "\nHuman correction: " + rejected.Feedback + "\n"
			}
		}
	default:
		return errors.New("unknown review-loop node")
	}
	if outcome.Kind == daemon.Completed {
		if choice != "" {
			found := false
			for _, group := range view.Workflow.ChoiceGroups {
				found = found || group.ID == choice
			}
			if !found {
				return errors.New("fixture selected an unavailable transition")
			}
			outcome.Transition = &daemon.Transition{ChoiceGroupID: domain.WorkflowChoiceGroupID(choice), SkipOptionalTaskIDs: []domain.WorkflowTaskID{}, ReviewSkippedTaskIDs: []domain.WorkflowTaskID{}}
		}
		var created struct {
			Artifact domain.Artifact `json:"artifact"`
		}
		if err := call("upload_artifact", map[string]string{"task_id": meta["task_id"], "claim_id": meta["claim_id"], "operation_id": "loop-upload-" + meta["claim_id"], "name": name, "content_base64": base64.StdEncoding.EncodeToString([]byte(content))}, &created); err != nil {
			return err
		}
		outcome.Result = "Delivered " + name + "; the fixture verified its exact input provenance."
		outcome.ArtifactIDs = []domain.ArtifactID{created.Artifact.ID}
	}
	encoded, err := json.Marshal(daemon.HarnessOutcome{Task: &outcome})
	if err != nil {
		return err
	}
	return os.WriteFile(output, encoded, 0600)
}

func checkReviewLoopBinary(t *testing.T, root, bins string, crash bool) {
	t.Helper()
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	base := "http://" + address
	admin := strings.Repeat("review-loop-test-", 3)
	startBinary(t, filepath.Join(bins, "kairos-server"), nil, []string{"KAIROS_AUTH_MODE=authenticated", "KAIROS_ADMIN_TOKEN=" + admin, "KAIROS_LISTEN_ADDR=" + address, "KAIROS_SQLITE_PATH=" + filepath.Join(dir, "core.db"), "KAIROS_ARTIFACT_DIR=" + filepath.Join(dir, "artifacts")})
	health := &http.Client{Timeout: time.Second}
	awaitBinary(t, 5*time.Second, func() bool {
		response, err := health.Get(base + "/healthz")
		if err != nil {
			return false
		}
		response.Body.Close()
		return response.StatusCode == 200
	})
	issue := func(kind, id, role string) string {
		body, _ := json.Marshal(map[string]string{"kind": kind, "id": id, "role": role})
		var issued struct {
			Token string `json:"token"`
		}
		requestJSON(t, base, admin, "POST", "/api/v1/identities", body, 201, &issued)
		return issued.Token
	}
	operator := issue("human", "loop-operator", "")
	var loopWork domain.WorkItem
	for _, fixture := range []struct{ file, endpoint string }{{"workflow.json", "/api/v1/definitions/workflows/review-loop/versions"}, {"work-item.json", "/api/v1/work-items"}} {
		body, err := os.ReadFile(filepath.Join(root, "examples/review-loop", fixture.file))
		if err != nil {
			t.Fatal(err)
		}
		if fixture.file == "work-item.json" {
			requestJSON(t, base, operator, "POST", fixture.endpoint, body, 201, &loopWork)
		} else {
			requestJSON(t, base, operator, "POST", fixture.endpoint, body, 201, nil)
		}
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	launch := func(role, mode string) *binaryProcess {
		t.Helper()
		shim := filepath.Join(dir, role+"-"+mode)
		if err := os.WriteFile(shim, []byte("#!/bin/sh\nexport KAIROS_FAKE_CLI=1 KAIROS_FAKE_MODE="+quote(mode)+" GORACE=atexit_sleep_ms=0\nexec "+quote(os.Args[0])+" \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		token := issue("agent", "loop-"+role+"-"+mode, role)
		return startBinary(t, filepath.Join(bins, "kairos-daemon"), []string{"--adapter=codex", "--core-url=" + base, "--codex-executable=" + shim, "--codex-home=" + dir, "--codex-model=test", "--workspace-root=" + filepath.Join(dir, role+"-workspaces"), "--slots=1", "--lease=15s", "--request-timeout=1s", "--poll-interval=30ms", "--discovery-interval=30ms", "--probe-interval=100ms", "--max-attempts=1", "--max-dispatches=1"}, []string{"KAIROS_DAEMON_TOKEN=" + token})
	}
	read := func() application.WorkItemExecutionContext {
		t.Helper()
		var view application.WorkItemExecutionContext
		requestJSON(t, base, operator, "GET", "/api/v1/work-items/"+string(loopWork.ID)+"/context", nil, 200, &view)
		return view
	}
	writer := launch("writer", "review_loop")
	var checker *binaryProcess
	if crash {
		crashed := launch("checker", "ignore_interrupt")
		var pid int
		awaitBinary(t, 8*time.Second, func() bool {
			markers, _ := filepath.Glob(filepath.Join(dir, "checker-workspaces", "*", "run-*", "started"))
			if len(markers) != 1 {
				return false
			}
			value, err := os.ReadFile(markers[0])
			if err != nil {
				return false
			}
			pid, err = strconv.Atoi(string(value))
			if err != nil || pid <= 1 {
				t.Fatal("invalid fixture PID")
			}
			return true
		})
		harness, err := os.FindProcess(pid)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = harness.Kill() })
		_ = crashed.cmd.Process.Kill()
		<-crashed.done
		awaitBinary(t, 35*time.Second, func() bool {
			view := read()
			for _, claim := range view.Claims {
				if claim.EndReason == domain.ClaimEndExpired {
					return true
				}
			}
			return false
		})
		for _, task := range read().Tasks {
			if len(task.Failures) != 0 {
				t.Fatal("crash fabricated business failure")
			}
		}
		// The fixture operator explicitly cleans its known orphan before starting
		// another checker. Core's lease reaper does not terminate Harnesses.
		_ = harness.Kill()
		awaitBinary(t, 5*time.Second, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH })
	}
	checker = launch("checker", "review_loop")
	var failed domain.Task
	awaitBinary(t, 10*time.Second, func() bool {
		for _, task := range read().Tasks {
			if task.Status == domain.TaskStatusFailed {
				failed = task
				return true
			}
		}
		return false
	})
	if failed.WorkflowTaskID == nil || *failed.WorkflowTaskID != "check" || len(failed.Failures) != 1 || failed.Failures[0].Action != domain.TaskFailureAwaitHuman || !strings.Contains(failed.Failures[0].Reason, "operator decision") {
		t.Fatalf("wrong failure guard: %+v", failed)
	}
	var originalGuide domain.Artifact
	for _, artifact := range read().Artifacts {
		if artifact.Name == "guide" {
			originalGuide = artifact
		}
	}
	if originalGuide.ID == "" {
		t.Fatal("initial guide was lost")
	}
	body, _ := json.Marshal(map[string]any{"version": read().WorkItem.Version, "max_task_instances_per_node": 10, "instructions": "Fixture operator permits checking; preserve the initial guide and request a restore-verification revision."})
	requestJSON(t, base, operator, "POST", "/api/v1/work-items/"+string(loopWork.ID)+"/continue", body, 200, nil)
	var publication domain.Task
	awaitBinary(t, 15*time.Second, func() bool {
		for _, task := range read().Tasks {
			if task.WorkflowTaskID != nil && *task.WorkflowTaskID == "publish" && task.Status == domain.TaskStatusInReview {
				publication = task
				return true
			}
		}
		return false
	})
	view := read()
	var replacement domain.Task
	counts := map[domain.WorkflowTaskID]int{}
	for _, task := range view.Tasks {
		counts[*task.WorkflowTaskID]++
		if task.RetryOfTaskID != nil && *task.RetryOfTaskID == failed.ID {
			replacement = task
		}
	}
	if replacement.ID == "" || counts["draft"] != 2 || counts["check"] != 3 || counts["publish"] != 1 {
		t.Fatalf("missing replacement or real loop: %+v", counts)
	}
	readArtifact := func(id domain.ArtifactID) []byte {
		t.Helper()
		request, err := http.NewRequest("GET", base+"/api/v1/artifacts/"+string(id)+"/content", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+operator)
		response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("submitted artifact content unavailable")
		}
		content, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	for _, artifact := range view.Artifacts {
		if artifact.TaskID == replacement.ID {
			var report struct {
				GuideID domain.ArtifactID `json:"guide_artifact_id"`
			}
			if err := json.Unmarshal(readArtifact(artifact.ID), &report); err != nil || report.GuideID != originalGuide.ID {
				t.Fatal("Human Continue changed the original input version")
			}
		}
	}
	if !strings.Contains(string(readArtifact(originalGuide.ID)), "Guide v1") {
		t.Fatal("initial history overwritten")
	}
	reviewID := publication.Reviews[len(publication.Reviews)-1].ID
	requestJSON(t, base, operator, "POST", "/api/v1/tasks/"+string(publication.ID)+"/reviews/"+string(reviewID)+"/decision", []byte(`{"decision":"rejected","feedback":"Include rollback steps."}`), 200, nil)
	awaitBinary(t, 10*time.Second, func() bool {
		for _, task := range read().Tasks {
			if task.ID == publication.ID && task.Status == domain.TaskStatusInReview && len(task.Submissions) == 2 {
				publication = task
				return true
			}
		}
		return false
	})
	if len(publication.Reviews) != 2 || publication.Reviews[0].Status != domain.ReviewStatusRejected {
		t.Fatal("Review rejection history lost")
	}
	var finalGuide domain.Artifact
	for _, artifact := range read().Artifacts {
		if artifact.SubmissionID != nil && *artifact.SubmissionID == publication.Submissions[1].ID {
			finalGuide = artifact
		}
	}
	if finalGuide.ID == "" || !strings.Contains(string(readArtifact(finalGuide.ID)), "Human correction: Include rollback steps.") {
		t.Fatal("next Claim did not receive Human feedback")
	}
	reviewID = publication.Reviews[1].ID
	requestJSON(t, base, operator, "POST", "/api/v1/tasks/"+string(publication.ID)+"/reviews/"+string(reviewID)+"/decision", []byte(`{"decision":"approved","feedback":"Fixture accepted."}`), 200, nil)
	completed := read()
	if completed.WorkItem.Status != domain.WorkItemStatusCompleted || completed.WorkItem.Result != "" || len(completed.ActiveClaims) != 0 || len(completed.Artifacts) != 6 {
		t.Fatalf("incomplete durable delivery: %+v", completed.WorkItem)
	}
	writer.stop(t)
	checker.stop(t)

}
