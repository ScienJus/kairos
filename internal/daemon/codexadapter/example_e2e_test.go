//go:build darwin || linux

package codexadapter

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/repository"
)

// Signal the example's entire foreground group, not just a Daemon PID.
// The fake Harness parks in Stop until the test verifies that Core is still up.
func checkExampleGroupShutdown(t *testing.T, root, bins string, sig syscall.Signal) {
	t.Helper()
	dir := t.TempDir()
	// Spaces and shell metacharacters must remain literal through the launch wrapper.
	wrappers := filepath.Join(dir, "bin with spaces;literal")
	scratch := filepath.Join(dir, "tmp")
	for _, path := range []string{wrappers, scratch} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	for _, name := range []string{"kairos-server", "kairos-daemon"} {
		script := "#!/bin/sh\nprintf '%s' \"$$\" > " + quote(filepath.Join(dir, name+".pid")) + "\nexec " + quote(filepath.Join(bins, name)) + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(wrappers, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	fake := "#!/bin/sh\nexport KAIROS_FAKE_CLI=1 KAIROS_FAKE_MODE=delayed_stop GORACE=atexit_sleep_ms=0\nexec " + quote(os.Args[0]) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrappers, "codex"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	launcher := startBinary(t, "/bin/sh", []string{filepath.Join(root, "examples/daemon/run.sh")}, []string{
		"KAIROS_BIN_DIR=" + wrappers, "KAIROS_DAEMON_CODEX_HOME=" + dir, "KAIROS_DAEMON_MODEL=test",
		"KAIROS_DAEMON_EXAMPLE_ADDR=" + address, "TMPDIR=" + scratch, "PATH=" + wrappers + string(os.PathListSeparator) + os.Getenv("PATH"),
	})
	readPID := func(path string) int {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(string(raw))
		if err != nil || pid <= 1 {
			t.Fatalf("invalid fixture PID: %q %v", raw, err)
		}
		return pid
	}
	var runDir string
	awaitBinary(t, 10*time.Second, func() bool {
		matches, _ := filepath.Glob(filepath.Join(scratch, "kairos-daemon-example.*", "workspaces", "*", "run-*", "started"))
		if len(matches) != 1 {
			return false
		}
		runDir = filepath.Dir(matches[0])
		return true
	})
	harnessPID := readPID(filepath.Join(runDir, "started"))
	corePID := readPID(filepath.Join(dir, "kairos-server.pid"))
	daemonPID := readPID(filepath.Join(dir, "kairos-daemon.pid"))
	// Emergency fixture cleanup also handles a failing pre-fix launcher, whose
	// premature exit otherwise leaves the unrenewed Claim and external Harness.
	t.Cleanup(func() {
		for _, pid := range []int{harnessPID, daemonPID, corePID} {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	group, err := syscall.Getpgid(launcher.cmd.Process.Pid)
	if err != nil || group != launcher.cmd.Process.Pid {
		t.Fatal("launcher must own the test foreground group")
	}
	if err = syscall.Kill(-group, sig); err != nil {
		t.Fatal(err)
	}
	awaitBinary(t, 5*time.Second, func() bool { _, e := os.Stat(filepath.Join(runDir, "stopping")); return e == nil })
	// Repeat the terminal signal while cleanup is waiting, before allowing the
	// Harness to end. Neither the Core nor the launcher may skip ordered cleanup.
	if err = syscall.Kill(-group, sig); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://" + address + "/healthz")
	if err != nil {
		t.Fatalf("Core stopped before Daemon could release its Claim: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("Core unavailable during Daemon cleanup")
	}
	coreGroup, err := syscall.Getpgid(corePID)
	if err != nil || coreGroup == group {
		t.Fatal("Core still shares the terminal signal group")
	}
	if err = os.WriteFile(filepath.Join(runDir, "allow-exit"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-launcher.done:
	case <-time.After(8 * time.Second):
		t.Fatal("example did not finish ordered shutdown")
	}
	if code := launcher.cmd.ProcessState.ExitCode(); code != 128+int(sig) {
		t.Fatalf("launcher exit %d, want %d", code, 128+int(sig))
	}
	log, err := os.ReadFile(launcher.logPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(log, []byte("unresolved Dispatches")) || bytes.Contains(log, []byte(`"state":"lost"`)) {
		t.Fatalf("graceful shutdown failed: %s", log)
	}
	if !bytes.Contains(log, []byte(`"state":"finished"`)) {
		t.Fatal("missing confirmed Dispatch finalization")
	}
	if syscall.Kill(corePID, 0) == nil || syscall.Kill(daemonPID, 0) == nil || syscall.Kill(harnessPID, 0) == nil {
		t.Fatal("example left a live process after shutdown")
	}
	demos, err := filepath.Glob(filepath.Join(scratch, "kairos-daemon-example.*"))
	if err != nil || len(demos) != 1 {
		t.Fatal("missing example database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	repo, err := repository.OpenSQLite(ctx, filepath.Join(demos[0], "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	err = repo.View(ctx, func(store application.ReadStore) error {
		works, err := store.ListWorkItems(application.WorkItemFilter{Page: application.PageRequest[application.WorkItemCursor]{Limit: 50}})
		if err != nil {
			return err
		}
		claims := 0
		for _, work := range works {
			if work.Status != domain.WorkItemStatusOpen {
				return fmt.Errorf("stopping changed WorkItem to %s", work.Status)
			}
			taskClaims, err := store.ListClaimsByWorkItem(work.ID)
			if err != nil {
				return err
			}
			for _, claim := range taskClaims {
				claims++
				if claim.Active() || string(claim.EndReason) != "released" {
					return fmt.Errorf("Task Claim was not released: %s", claim.EndReason)
				}
			}
			coordClaims, err := store.ListCoordinationClaims(work.ID)
			if err != nil {
				return err
			}
			for _, claim := range coordClaims {
				claims++
				if claim.Active() || string(claim.EndReason) != "released" {
					return fmt.Errorf("Coordination Claim was not released: %s", claim.EndReason)
				}
			}
		}
		if claims != 1 {
			return fmt.Errorf("got %d Claims; want one completed responsibility", claims)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
