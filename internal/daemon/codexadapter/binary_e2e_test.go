//go:build darwin || linux

package codexadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

type binaryProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	logPath string
}

func startBinary(t *testing.T, executable string, args, env []string) *binaryProcess {
	t.Helper()
	cmd := exec.Command(executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "KAIROS_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, env...)
	log, err := os.CreateTemp(t.TempDir(), "process-*.log")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	p := &binaryProcess{cmd: cmd, done: make(chan struct{}), logPath: log.Name()}
	go func() { _ = cmd.Wait(); _ = log.Close(); close(p.done) }()
	t.Cleanup(func() {
		p.stop(t)
		if t.Failed() {
			data, _ := os.ReadFile(p.logPath)
			text := string(data)
			for _, entry := range env {
				key, value, ok := strings.Cut(entry, "=")
				if ok && strings.Contains(key, "TOKEN") {
					text = strings.ReplaceAll(text, value, "[redacted]")
				}
			}
			t.Logf("%s log: %s", filepath.Base(executable), text)
		}
	})
	return p
}

func (p *binaryProcess) stop(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	select {
	case <-p.done:
	case <-time.After(8 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
		t.Error("binary exceeded graceful shutdown budget")
	}
}

func requestJSON(t *testing.T, base, token, method, path string, body []byte, status int, out any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("e2e-%d", time.Now().UnixNano()))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("%s %s: status %d, want %d", method, path, resp.StatusCode, status)
	}
	if out != nil {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err = json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(envelope.Data, out); err != nil {
			t.Fatal(err)
		}
	}
}

func awaitBinary(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("binary E2E condition timed out")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDaemonBinaryE2E(t *testing.T) {
	if os.Getenv("KAIROS_DAEMON_E2E") != "1" {
		t.Skip("set KAIROS_DAEMON_E2E=1; builds/runs isolated binaries, no model calls")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	bins := t.TempDir()
	for _, name := range []string{"kairos-server", "kairos-daemon"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		build := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(bins, name), "./cmd/"+name)
		build.Dir = root
		output, err := build.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("build %s: %v %s", name, err, output)
		}
	}
	for _, scenario := range []string{"complete", "failure-task", "failure-coordination", "cancel-task", "cancel-coordination", "crash-task", "crash-coordination", "example-interrupt", "example-terminate"} {
		t.Run(scenario, func(t *testing.T) {
			if strings.HasPrefix(scenario, "example-") {
				sig := syscall.SIGINT
				if scenario == "example-terminate" {
					sig = syscall.SIGTERM
				}
				checkExampleGroupShutdown(t, root, bins, sig)
				return
			}
			dataDir := t.TempDir()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			listener.Close()
			base := "http://" + address
			admin := strings.Repeat("local-test-admin-", 3) + filepath.Base(dataDir)
			core := startBinary(t, filepath.Join(bins, "kairos-server"), nil, []string{"KAIROS_AUTH_MODE=authenticated", "KAIROS_ADMIN_TOKEN=" + admin, "KAIROS_LISTEN_ADDR=" + address, "KAIROS_SQLITE_PATH=" + filepath.Join(dataDir, "core.db"), "KAIROS_ARTIFACT_DIR=" + filepath.Join(dataDir, "artifacts")})
			health := &http.Client{Timeout: time.Second}
			awaitBinary(t, 5*time.Second, func() bool {
				select {
				case <-core.done:
					t.Fatal("Core exited during startup")
				default:
				}
				resp, e := health.Get(base + "/healthz")
				if e != nil {
					return false
				}
				resp.Body.Close()
				return resp.StatusCode == 200
			})
			issue := func(kind, id, role string) string {
				body, _ := json.Marshal(map[string]string{"kind": kind, "id": id, "role": role})
				var result struct {
					Token string `json:"token"`
				}
				requestJSON(t, base, admin, "POST", "/api/v1/identities", body, 201, &result)
				if result.Token == "" {
					t.Fatal("empty identity credential")
				}
				return result.Token
			}
			operator := issue("human", "operator", "")
			agent := issue("agent", "daemon", "backend")
			modes := []string{"workflow", "blackboard"}
			if strings.HasSuffix(scenario, "task") {
				modes = []string{"workflow"}
			} else if strings.HasSuffix(scenario, "coordination") {
				modes = []string{"blackboard"}
			}
			workIDs := []domain.WorkItemID{}
			for _, mode := range modes {
				definition, err := os.ReadFile(filepath.Join(root, "examples/daemon", mode+".json"))
				if err != nil {
					t.Fatal(err)
				}
				requestJSON(t, base, operator, "POST", "/api/v1/definitions/"+mode+"s/daemon-"+mode+"/versions", definition, 201, nil)
				body, err := os.ReadFile(filepath.Join(root, "examples/daemon", mode+"-work-item.json"))
				if err != nil {
					t.Fatal(err)
				}
				var work domain.WorkItem
				requestJSON(t, base, operator, "POST", "/api/v1/work-items", body, 201, &work)
				workIDs = append(workIDs, work.ID)
			}
			mode := "mcp"
			if strings.HasPrefix(scenario, "failure") {
				mode = "malformed"
			}
			if strings.HasPrefix(scenario, "cancel") || strings.HasPrefix(scenario, "crash") {
				mode = "ignore_interrupt"
			}
			shim := filepath.Join(dataDir, "fake-codex")
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
			// The production Adapter sanitizes the Daemon environment. Only this test
			// executable shim supplies the fake CLI's behavior flags to its own process.
			script := "#!/bin/sh\nexport KAIROS_FAKE_CLI=1 KAIROS_FAKE_MODE=" + quote(mode) + " GORACE=atexit_sleep_ms=0\nexec " + quote(os.Args[0]) + " \"$@\"\n"
			if err = os.WriteFile(shim, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			workspace := filepath.Join(dataDir, "workspaces")
			daemon := startBinary(t, filepath.Join(bins, "kairos-daemon"), []string{"--adapter=codex", "--core-url=" + base, "--codex-executable=" + shim, "--codex-home=" + dataDir, "--codex-model=test", "--workspace-root=" + workspace, "--lease=15s", "--request-timeout=1s", "--stop-timeout=100ms", "--shutdown-timeout=5s", "--poll-interval=20ms", "--discovery-interval=20ms", "--probe-interval=100ms", "--max-attempts=1", "--max-dispatches=1", "--cooldown=100ms"}, []string{"KAIROS_DAEMON_TOKEN=" + agent})
			view := func(id domain.WorkItemID) application.WorkItemExecutionContext {
				var v application.WorkItemExecutionContext
				requestJSON(t, base, operator, "GET", "/api/v1/work-items/"+string(id)+"/context", nil, 200, &v)
				return v
			}
			if scenario == "complete" {
				for _, id := range workIDs {
					awaitBinary(t, 15*time.Second, func() bool { return view(id).WorkItem.Status == domain.WorkItemStatusCompleted })
					v := view(id)
					if len(v.Artifacts) != 1 || len(v.Tasks) != 1 || len(v.ActiveClaims) != 0 || v.ActiveCoordinationClaim != nil {
						t.Fatalf("bad completed context: tasks=%d artifacts=%d active=%d", len(v.Tasks), len(v.Artifacts), len(v.ActiveClaims))
					}
					if v.Tasks[0].Status != domain.TaskStatusCompleted || len(v.Claims) != 1 || v.Artifacts[0].SubmissionID == nil {
						t.Fatal("incomplete Task/Claim/Artifact history")
					}
					if v.WorkItem.AcceptanceMode == domain.WorkItemAcceptanceAgent && len(v.CoordinationClaims) != 3 {
						t.Fatal("missing planning/completion/acceptance Claim history")
					}
					resp, err := http.NewRequest("GET", base+"/api/v1/artifacts/"+string(v.Artifacts[0].ID)+"/content", nil)
					if err != nil {
						t.Fatal(err)
					}
					resp.Header.Set("Authorization", "Bearer "+operator)
					response, err := health.Do(resp)
					if err != nil {
						t.Fatal(err)
					}
					content, err := io.ReadAll(response.Body)
					response.Body.Close()
					if err != nil || response.StatusCode != 200 || string(content) != "managed result" {
						t.Fatal("committed content mismatch")
					}
				}
				daemon.stop(t)
				return
			}
			id := workIDs[0]
			claimHistory := func() ([]domain.Claim, []domain.CoordinationClaim) {
				v := view(id)
				return v.Claims, v.CoordinationClaims
			}
			if strings.HasPrefix(scenario, "failure") {
				awaitBinary(t, 8*time.Second, func() bool {
					tasks, coords := claimHistory()
					return (len(tasks) == 1 && !tasks[0].Active()) || (len(coords) == 1 && !coords[0].Active())
				})
				// Several discovery and health cycles must not reacquire this generation.
				time.Sleep(500 * time.Millisecond)
				tasks, coords := claimHistory()
				if len(tasks)+len(coords) != 1 {
					t.Fatal("failure suppression allowed another Claim")
				}
				reason := ""
				if len(tasks) == 1 {
					reason = string(tasks[0].EndReason)
				} else {
					reason = string(coords[0].EndReason)
				}
				if reason != "released" {
					t.Fatalf("failure ended Claim as %s", reason)
				}
				v := view(id)
				if v.WorkItem.Status != domain.WorkItemStatusOpen {
					t.Fatal("infrastructure failure terminated WorkItem")
				}
				for _, task := range v.Tasks {
					if task.Status != domain.TaskStatusPending || len(task.Failures) != 0 {
						t.Fatal("runtime error became a business failure")
					}
				}
				daemon.stop(t)
				return
			}
			var harness *os.Process
			awaitBinary(t, 5*time.Second, func() bool {
				files, _ := filepath.Glob(filepath.Join(workspace, "*", "run-*", "started"))
				if len(files) != 1 {
					return false
				}
				raw, e := os.ReadFile(files[0])
				if e != nil {
					return false
				}
				pid, e := strconv.Atoi(string(raw))
				if e != nil {
					t.Fatal(e)
				}
				harness, e = os.FindProcess(pid)
				if e != nil {
					t.Fatal(e)
				}
				return true
			})
			// A crashed Daemon cannot clean its Harness. Test teardown owns this fixture.
			t.Cleanup(func() { _ = harness.Kill() })
			if strings.HasPrefix(scenario, "cancel") {
				requestJSON(t, base, operator, "POST", "/api/v1/work-items/"+string(id)+"/cancellation", []byte(`{"reason":"E2E cancellation"}`), 200, nil)
			} else {
				_ = daemon.cmd.Process.Kill()
				<-daemon.done
			}
			awaitBinary(t, 35*time.Second, func() bool {
				tasks, coords := claimHistory()
				return (len(tasks) == 1 && !tasks[0].Active()) || (len(coords) == 1 && !coords[0].Active())
			})
			tasks, coords := claimHistory()
			reason := ""
			if len(tasks) == 1 {
				reason = string(tasks[0].EndReason)
			} else if len(coords) == 1 {
				reason = string(coords[0].EndReason)
			}
			expected := "expired"
			if strings.HasPrefix(scenario, "cancel") {
				expected = "work_item_cancelled"
			}
			if reason != expected {
				t.Fatalf("Claim ended %q, want %q", reason, expected)
			}
			v := view(id)
			for _, task := range v.Tasks {
				if len(task.Failures) != 0 {
					t.Fatal("stop/reaper fabricated business failure")
				}
			}
			if expected == "expired" {
				if v.WorkItem.Status != domain.WorkItemStatusOpen {
					t.Fatal("reaper terminated WorkItem")
				}
				var rows []application.WorkCandidate
				requestJSON(t, base, agent, "GET", "/api/v1/work", nil, 200, &rows)
				if len(rows) != 1 || rows[0].WorkItem.ID != id {
					t.Fatal("reaped candidate is not rediscoverable")
				}
				// Core reclaimed responsibility, not the still-live external Harness.
				if err := harness.Signal(syscall.Signal(0)); err != nil {
					t.Fatal("test fixture ended before proving crash boundary")
				}
			} else {
				if v.WorkItem.Status != domain.WorkItemStatusCancelled {
					t.Fatal("cancellation not persisted")
				}
				awaitBinary(t, 5*time.Second, func() bool {
					log, _ := os.ReadFile(daemon.logPath)
					return bytes.Contains(log, []byte(`"state":"lost"`)) && bytes.Contains(log, []byte(`"active":0`))
				})
				awaitBinary(t, 5*time.Second, func() bool { return harness.Signal(syscall.Signal(0)) != nil })
				daemon.stop(t)
			}
		})
	}
}
