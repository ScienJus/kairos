// kairos-daemon runs the continuous scheduler with a configured Harness adapter.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/ScienJus/kairos/internal/daemon"
	"github.com/ScienJus/kairos/internal/daemon/codexadapter"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out, stderr io.Writer) error {
	options := daemon.DefaultSchedulerOptions()
	flags := flag.NewFlagSet("kairos-daemon", flag.ContinueOnError)
	flags.SetOutput(stderr)
	coreURL := flags.String("core-url", "http://localhost:8080", "Kairos Core base URL")
	mcpURL := flags.String("mcp-url", "", "MCP endpoint (defaults to Core URL + /mcp)")
	adapter := flags.String("adapter", "unavailable", "unavailable, fake-abandon (diagnostics), or codex")
	codexExecutable := flags.String("codex-executable", "codex", "Codex CLI executable (0.146.x)")
	codexHome := flags.String("codex-home", "", "dedicated authenticated Codex home (required with --adapter=codex)")
	codexModel := flags.String("codex-model", "", "explicit model for Codex (required with --adapter=codex)")
	tags := flags.String("tags", "", "comma-separated discovery tags")
	flags.StringVar(&options.WorkspaceRoot, "workspace-root", options.WorkspaceRoot, "private per-Dispatch workspace root (retained after exit)")
	flags.IntVar(&options.Slots, "slots", options.Slots, "maximum concurrent Dispatches")
	flags.IntVar(&options.DiscoveryLimit, "discovery-limit", options.DiscoveryLimit, "Core per-kind candidate limit (1..50)")
	flags.IntVar(&options.MaxDispatches, "max-dispatches", options.MaxDispatches, "unsuccessful Claims per candidate generation before quarantine")
	flags.IntVar(&options.Dispatch.MaxAttempts, "max-attempts", options.Dispatch.MaxAttempts, "Harness attempts within one Claim")
	flags.DurationVar(&options.Dispatch.Lease, "lease", options.Dispatch.Lease, "Claim lease; heartbeat timing is derived")
	flags.DurationVar(&options.Dispatch.PollInterval, "poll-interval", options.Dispatch.PollInterval, "Harness observation interval")
	flags.DurationVar(&options.Dispatch.RequestTimeout, "request-timeout", options.Dispatch.RequestTimeout, "Core/Adapter call timeout")
	flags.DurationVar(&options.Dispatch.StopTimeout, "stop-timeout", options.Dispatch.StopTimeout, "Harness stop confirmation window")
	flags.DurationVar(&options.DiscoveryInterval, "discovery-interval", options.DiscoveryInterval, "candidate discovery interval")
	flags.DurationVar(&options.ProbeInterval, "probe-interval", options.ProbeInterval, "health probe interval while idle or suppressed")
	flags.DurationVar(&options.Cooldown, "cooldown", options.Cooldown, "candidate retry cooldown")
	flags.DurationVar(&options.ShutdownTimeout, "shutdown-timeout", options.ShutdownTimeout, "graceful reconcile window before process exit")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: kairos-daemon [flags]\nCredential: KAIROS_DAEMON_TOKEN environment variable; never pass it as a flag.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid command arguments")
	}
	if flags.NArg() != 0 {
		return errors.New("positional arguments are not supported")
	}
	if *adapter != "unavailable" && *adapter != "fake-abandon" && *adapter != "codex" {
		return errors.New("unsupported adapter")
	}
	core, err := daemon.NewHTTPClient(*coreURL, daemon.NewSecret(getenv("KAIROS_DAEMON_TOKEN")), nil)
	if err != nil {
		return err
	}
	options.Dispatch.MCPURL = *mcpURL
	options.Dispatch.CoreURL = *coreURL
	if *mcpURL == "" {
		options.Dispatch.MCPURL = strings.TrimRight(*coreURL, "/") + "/mcp"
	}
	if *tags != "" {
		options.Tags = strings.Split(*tags, ",")
	}
	options.Logger = slog.New(slog.NewJSONHandler(out, nil))
	var harness daemon.Adapter = &diagnosticAdapter{enabled: *adapter == "fake-abandon", runs: make(map[string]daemon.Candidate)}
	if *adapter == "codex" {
		harness, err = codexadapter.New(codexadapter.Options{Executable: *codexExecutable, Home: *codexHome, Model: *codexModel})
		if err != nil {
			return err
		}
	}
	scheduler, err := daemon.NewScheduler(core, harness, options)
	if err != nil {
		return err
	}
	options.Logger.Info("daemon_started", "adapter", *adapter, "slots", options.Slots)
	return scheduler.Run(ctx)
}

// The opt-in fake Adapter releases and quarantines work; it never calls a model.
type diagnosticAdapter struct {
	enabled bool
	mu      sync.Mutex
	runs    map[string]daemon.Candidate
}

func (a *diagnosticAdapter) Probe(context.Context) error {
	if !a.enabled {
		return errors.New("no Harness adapter configured")
	}
	return nil
}
func (a *diagnosticAdapter) Start(ctx context.Context, r daemon.StartRequest) (daemon.RunRef, error) {
	if err := ctx.Err(); err != nil {
		return daemon.RunRef{}, err
	}
	if !a.enabled {
		return daemon.RunRef{}, errors.New("no Harness adapter configured")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runs[r.ClaimID] = r.Candidate
	return daemon.RunRef{ID: r.ClaimID}, nil
}
func (a *diagnosticAdapter) Observe(_ context.Context, r daemon.RunRef) (daemon.RunObservation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	candidate, ok := a.runs[r.ID]
	if !ok {
		return daemon.RunObservation{State: daemon.RunStopped}, nil
	}
	outcome := &daemon.HarnessOutcome{}
	if candidate.Kind == daemon.TaskCandidate {
		outcome.Task = &daemon.TaskOutcome{Kind: daemon.Abandoned}
	} else {
		outcome.Coordination = &daemon.CoordinationDecision{Kind: daemon.Abandoned}
	}
	delete(a.runs, r.ID)
	return daemon.RunObservation{State: daemon.OutcomeReady, Outcome: outcome}, nil
}
func (a *diagnosticAdapter) Stop(_ context.Context, r daemon.RunRef, _ daemon.StopReason) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.runs, r.ID)
	return nil
}
