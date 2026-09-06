//go:build darwin || linux

package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/daemon"
)

// Opt-in installed CLI regression, without real authentication or model calls.
// The scripted loopback Provider asks the actual CLI to execute an ordinary shell
// command. A fake CLI alone cannot verify Codex's separate shell process groups.
func TestRealCodexShellLifecycle(t *testing.T) {
	cli := os.Getenv("KAIROS_TEST_CODEX_EXECUTABLE")
	if cli == "" {
		t.Skip("set KAIROS_TEST_CODEX_EXECUTABLE for the local simulated-Provider regression")
	}
	cli, err := exec.LookPath(cli)
	if err != nil {
		t.Fatal(err)
	}
	cli, err = filepath.Abs(cli)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"interrupt", "complete", "killed"} {
		t.Run(action, func(t *testing.T) {
			var calls atomic.Int32
			finish := make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				emit := func(event any) {
					data, _ := json.Marshal(event)
					fmt.Fprintf(w, "data: %s\n\n", data)
				}
				if calls.Add(1) == 1 {
					args, _ := json.Marshal(map[string]any{"cmd": `/bin/sh -c 'echo $$ > shell.pid; exec /bin/sleep 45'`, "yield_time_ms": 1000, "max_output_tokens": 1000})
					emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "first"}})
					emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"type": "function_call", "id": "fc", "call_id": "call", "name": "exec_command", "arguments": string(args)}})
					emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": "first", "status": "completed", "output": []any{}}})
					return
				}
				select {
				case <-r.Context().Done():
					return
				case <-finish:
				}
				result := `{"task":{"kind":"completed","result":"done","artifact_ids":[],"request_review":false,"transition":null,"children":[],"reason":"","retry_prompt":""},"runtime_failure":null}`
				item := map[string]any{"type": "message", "id": "msg", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": result, "annotations": []any{}}}}
				emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "last"}})
				emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
				emit(map[string]any{"type": "response.output_text.delta", "item_id": "msg", "output_index": 0, "content_index": 0, "delta": result})
				emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
				emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": "last", "status": "completed", "output": []any{item}}})
			}))
			defer provider.Close()
			shim := filepath.Join(t.TempDir(), "codex-shim")
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
			// Override only the Provider and unrelated MCP initialization. Start,
			// sandbox, environment, and Stop/Observe are the production paths.
			script := "#!/bin/sh\nexec " + quote(cli) + " \"$@\""
			for _, config := range []string{`model_provider="local_test"`, `model_providers.local_test.name="local_test"`, `model_providers.local_test.base_url="` + provider.URL + `"`, `model_providers.local_test.wire_api="responses"`, `model_providers.local_test.requires_openai_auth=false`, `mcp_servers.kairos.enabled=false`, `mcp_servers.kairos.required=false`} {
				script += " -c " + quote(config)
			}
			if err := os.WriteFile(shim, []byte(script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			a, request := fixture(t, "success")
			a.options.Executable = shim
			clean := []string{}
			for _, entry := range a.environment {
				if !strings.HasPrefix(entry, "KAIROS_FAKE_") {
					clean = append(clean, entry)
				}
			}
			a.environment = clean
			ref, err := a.Start(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			var child int
			defer func() {
				_ = a.Stop(context.Background(), ref, daemon.StopRequested)
				if child > 0 {
					_ = syscall.Kill(child, syscall.SIGKILL)
				}
			}()
			deadline := time.Now().Add(15 * time.Second)
			for child == 0 && time.Now().Before(deadline) {
				data, err := os.ReadFile(filepath.Join(ref.ID, "shell.pid"))
				if err == nil {
					child, err = strconv.Atoi(strings.TrimSpace(string(data)))
					if err != nil {
						t.Fatal(err)
					}
					break
				}
				obs, err := a.Observe(context.Background(), ref)
				if err != nil || obs.State != daemon.RunRunning {
					t.Fatalf("CLI ended before shell: %+v %v", obs, err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if child <= 0 {
				t.Fatal("real CLI did not start the ordinary shell command")
			}
			a.mu.Lock()
			process := a.runs[ref.ID].process
			a.mu.Unlock()
			group, err := syscall.Getpgid(child)
			if err != nil || group == process.Pid {
				t.Fatalf("regression must exercise a separate shell PGID: %d, root %d, %v", group, process.Pid, err)
			}
			switch action {
			case "interrupt":
				if err := a.Stop(context.Background(), ref, daemon.StopRequested); err != nil {
					t.Fatal(err)
				}
				// A repeated stop must not become a second interrupt during cleanup.
				if err := a.Stop(context.Background(), ref, daemon.StopRequested); err != nil {
					t.Fatal(err)
				}
			case "complete":
				close(finish)
			case "killed":
				if err := process.Kill(); err != nil {
					t.Fatal(err)
				}
			}
			obs := observeEnd(t, a, ref)
			want := daemon.RunStopped
			if action == "complete" {
				want = daemon.OutcomeReady
			}
			if action == "killed" {
				want = daemon.RunLost
			}
			if obs.State != want {
				t.Fatalf("%s observation = %+v, want %s", action, obs, want)
			}
			state, _ := exec.Command("/bin/ps", "-p", strconv.Itoa(child), "-o", "stat=").Output()
			if action != "killed" && len(strings.TrimSpace(string(state))) > 0 && !strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
				t.Fatalf("reported %s while ordinary shell is still live: %s", obs.State, state)
			}
		})
	}
}
