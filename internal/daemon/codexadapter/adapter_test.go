//go:build darwin || linux

package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/daemon"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestMain(m *testing.M) {
	if os.Getenv("KAIROS_FAKE_CLI") == "1" {
		helperCLI()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Execute real processes without involving a model or provider.
func helperCLI() {
	mode := os.Getenv("KAIROS_FAKE_MODE")
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		if mode == "bad_version" {
			fmt.Println("codex-cli 0.1.0")
		} else {
			fmt.Println("codex-cli 0.146.0")
		}
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "login" {
		if mode == "unauthenticated" {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "child" {
		for {
			time.Sleep(time.Hour)
		}
	}
	output := ""
	for i, arg := range os.Args {
		if arg == "--output-last-message" && i+1 < len(os.Args) {
			output = os.Args[i+1]
		}
	}
	if output == "" {
		os.Exit(3)
	}
	prompt, _ := io.ReadAll(os.Stdin)
	if mode == "mcp" {
		if err := helperMCP(prompt, output); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(6)
		}
		return
	}
	report := map[string]bool{
		"identity_seen":     os.Getenv("KAIROS_DAEMON_TOKEN") != "",
		"other_secret_seen": os.Getenv("UNRELATED_SECRET") != "" || os.Getenv("OPENAI_API_KEY") != "",
		"executor_seen":     os.Getenv(executorEnv) != "",
		"secret_in_args":    strings.Contains(strings.Join(os.Args, " "), os.Getenv(executorEnv)),
		"secret_in_prompt":  strings.Contains(string(prompt), os.Getenv(executorEnv)),
		"execution_prompt":  strings.Contains(string(prompt), "outcome.schema.json") && strings.Contains(string(prompt), "MCP server's credential-specific instructions"),
	}
	data, _ := json.Marshal(report)
	_ = os.WriteFile("report.json", data, 0600)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	_ = os.WriteFile("started", []byte(fmt.Sprint(os.Getpid())), 0600)
	if mode == "tree" {
		child := exec.Command(os.Args[0], "child")
		child.Env = os.Environ()
		if child.Start() != nil {
			os.Exit(4)
		}
		_ = os.WriteFile("child.pid", []byte(fmt.Sprint(child.Process.Pid)), 0600)
		<-interrupt
		_ = child.Process.Kill()
		_ = child.Wait()
		return
	}
	if mode == "sleep" {
		<-interrupt
		return
	}
	if mode == "ignore_interrupt" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode == "failure" {
		os.Exit(2)
	}
	if mode == "missing" {
		return
	}
	text := os.Getenv("KAIROS_FAKE_OUTCOME")
	if text == "" {
		text = `{"task":{"kind":"completed","result":"done"},"runtime_failure":null}`
	}
	if mode == "malformed" {
		text = "not JSON"
	}
	if mode == "oversized" {
		text = strings.Repeat("x", maxOutcomeBytes+1)
	}
	if os.WriteFile(output, []byte(text), 0600) != nil {
		os.Exit(5)
	}
}

func fixture(t *testing.T, mode string) (*Adapter, daemon.StartRequest) {
	t.Helper()
	a, err := New(Options{Executable: os.Args[0], Home: t.TempDir(), Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	a.environment = append(a.environment, "KAIROS_FAKE_CLI=1", "KAIROS_FAKE_MODE="+mode, "GORACE=atexit_sleep_ms=0")
	r := daemon.StartRequest{Candidate: daemon.Candidate{Kind: daemon.TaskCandidate, WorkItemID: "work", TaskID: "task", Mode: domain.CoordinationModeBlackboard}, ClaimID: "claim", Attempt: 1, Workspace: t.TempDir(), CoreURL: "http://localhost:8080", MCPURL: "http://localhost:8080/mcp", ExecutorToken: daemon.NewSecret("krs_claim_" + strings.Repeat("A", 43))}
	return a, r
}

func observeEnd(t *testing.T, a *Adapter, ref daemon.RunRef) daemon.RunObservation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	for {
		observation, err := a.Observe(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		if observation.State != daemon.RunRunning {
			return observation
		}
		select {
		case <-ctx.Done():
			_ = a.Stop(context.Background(), ref, daemon.StopRequested)
			t.Fatal("fake CLI did not end")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestRealProcessOutcomesAndEnvironment(t *testing.T) {
	t.Setenv("KAIROS_DAEMON_TOKEN", "must-not-inherit")
	t.Setenv("UNRELATED_SECRET", "must-not-inherit")
	t.Setenv("OPENAI_API_KEY", "must-not-inherit")
	for _, mode := range []string{"success", "malformed", "oversized", "missing", "failure"} {
		t.Run(mode, func(t *testing.T) {
			a, r := fixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if err := a.Probe(ctx); err != nil {
				t.Fatal(err)
			}
			ref, err := a.Start(ctx, r)
			if err != nil || ref.ID == "" {
				t.Fatalf("Start: %+v %v", ref, err)
			}
			observation := observeEnd(t, a, ref)
			want := daemon.RuntimeFailed
			if mode == "success" {
				want = daemon.OutcomeReady
			}
			if observation.State != want {
				t.Fatalf("outcome: %+v", observation)
			}
			if mode == "success" && (observation.Outcome == nil || observation.Outcome.Kind() != daemon.Completed) {
				t.Fatal("missing result")
			}
			data, err := os.ReadFile(filepath.Join(ref.ID, "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report map[string]bool
			if json.Unmarshal(data, &report) != nil {
				t.Fatal("invalid report")
			}
			if report["identity_seen"] || report["other_secret_seen"] || report["secret_in_args"] || report["secret_in_prompt"] || !report["executor_seen"] || !report["execution_prompt"] {
				t.Fatalf("unsafe environment: %s", data)
			}
			if _, err := os.Stat(filepath.Join(ref.ID, ".agents")); !os.IsNotExist(err) {
				t.Fatal("Adapter must not install a separate skill")
			}
			for _, name := range []string{"outcome.json", "outcome.schema.json"} {
				info, err := os.Stat(filepath.Join(ref.ID, name))
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0600 {
					t.Fatalf("file permissions: %s", name)
				}
			}
			if err = a.Stop(context.Background(), ref, daemon.StopRequested); err != nil {
				t.Fatal(err)
			}
			a.Forget(ref)
			if len(a.runs) != 0 {
				t.Fatal("terminal metadata leaked")
			}
			if _, err = os.Stat(ref.ID); err != nil {
				t.Fatal("Forget deleted workspace")
			}
		})
	}
}

func TestProbeAndStartFailures(t *testing.T) {
	for _, mode := range []string{"bad_version", "unauthenticated"} {
		a, _ := fixture(t, mode)
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		if a.Probe(ctx) == nil {
			t.Fatalf("Probe accepted %s", mode)
		}
		cancel()
		if len(a.runs) != 0 {
			t.Fatal("Probe created a run")
		}
	}
	a, r := fixture(t, "success")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ref, err := a.Start(ctx, r); err == nil || ref.ID != "" {
		t.Fatalf("cancelled Start: %+v %v", ref, err)
	}
	r.ExecutorToken = daemon.NewSecret("identity-token")
	if ref, err := a.Start(context.Background(), r); err == nil || ref.ID != "" {
		t.Fatalf("invalid token Start: %+v %v", ref, err)
	}
	if len(a.runs) != 0 {
		t.Fatal("failed Start created a run")
	}
}

func TestStopConfirmsExitAndPreservesIsolation(t *testing.T) {
	for _, mode := range []string{"sleep", "tree", "ignore_interrupt"} {
		t.Run(mode, func(t *testing.T) {
			a, r := fixture(t, mode)
			ref, err := a.Start(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			marker := "started"
			if mode == "tree" {
				marker = "child.pid"
			}
			deadline := time.Now().Add(4 * time.Second)
			for {
				if _, err = os.Stat(filepath.Join(ref.ID, marker)); err == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = a.Stop(context.Background(), ref, daemon.StopRequested)
					t.Fatal("fake did not start")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err = a.Stop(context.Background(), ref, daemon.StopRequested); err != nil {
				t.Fatal(err)
			}
			observation := observeEnd(t, a, ref)
			if observation.State != daemon.RunStopped && observation.State != daemon.RunLost {
				t.Fatalf("Stop became a business result: %+v", observation)
			}
			if mode == "sleep" && observation.State != daemon.RunStopped {
				t.Fatal("root process not confirmed stopped")
			}
			if mode == "ignore_interrupt" && observation.State != daemon.RunLost {
				t.Fatal("forced shutdown must not confirm shell cleanup")
			}
			if err = a.Stop(context.Background(), ref, daemon.StopRequested); err != nil {
				t.Fatal(err)
			}
			r.Attempt++
			other, err := a.Start(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if other.ID == ref.ID {
				t.Fatal("retry reused workspace")
			}
			_ = a.Stop(context.Background(), other, daemon.StopRequested)
			_ = observeEnd(t, a, other)
		})
	}
}

func TestRuntimeFailureAndCoordination(t *testing.T) {
	for _, system := range []bool{false, true} {
		a, r := fixture(t, "success")
		a.environment = append(a.environment, fmt.Sprintf(`KAIROS_FAKE_OUTCOME={"task":null,"runtime_failure":{"system":%t}}`, system))
		ref, err := a.Start(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		observation := observeEnd(t, a, ref)
		if observation.State != daemon.RuntimeFailed || observation.SystemFailure != system || observation.Outcome != nil {
			t.Fatalf("runtime envelope: %+v", observation)
		}
	}
	for _, kind := range []daemon.CandidateKind{daemon.EmptyBlackboard, daemon.BlackboardCompletion, daemon.WorkItemAcceptance} {
		a, r := fixture(t, "success")
		r.Candidate.Kind = kind
		r.Candidate.TaskID = ""
		a.environment = append(a.environment, `KAIROS_FAKE_OUTCOME={"coordination":{"kind":"abandoned"},"runtime_failure":null}`)
		ref, err := a.Start(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if observation := observeEnd(t, a, ref); observation.State != daemon.OutcomeReady {
			t.Fatalf("kind %s: %+v", kind, observation)
		}
	}
}
