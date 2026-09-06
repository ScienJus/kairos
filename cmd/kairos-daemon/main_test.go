package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/daemon"
)

func TestCommandConfiguration(t *testing.T) {
	for _, test := range []struct {
		name  string
		args  []string
		token string
		want  string
	}{
		{"help", []string{"--help"}, "", ""},
		{"missing token", nil, "", "Identity Token is required"},
		{"unknown adapter", []string{"--adapter=real"}, "secret", "unsupported adapter"},
		{"unconfigured codex", []string{"--adapter=codex", "--codex-executable=" + os.Args[0]}, "secret", "authentication home and model are required"},
		{"invalid slots", []string{"--slots=0"}, "secret", "invalid scheduler"},
		{"invalid tags", []string{"--tags= backend "}, "secret", "whitespace"},
		{"positional", []string{"extra"}, "secret", "positional"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			err := run(context.Background(), test.args, func(string) string { return test.token }, &out, &stderr)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error: %v", err)
			}
			if strings.Contains(out.String(), "secret") {
				t.Fatal("credential logged")
			}
		})
	}
}

func TestDiagnosticAdapterExplicitOptIn(t *testing.T) {
	a := &diagnosticAdapter{runs: make(map[string]daemon.Candidate)}
	if a.Probe(context.Background()) == nil {
		t.Fatal("default adapter must block Claims")
	}
	a.enabled = true
	for _, kind := range []daemon.CandidateKind{daemon.TaskCandidate, daemon.EmptyBlackboard, daemon.BlackboardCompletion, daemon.WorkItemAcceptance} {
		candidate := daemon.Candidate{Kind: kind}
		ref, err := a.Start(context.Background(), daemon.StartRequest{Candidate: candidate, ClaimID: "test"})
		if err != nil {
			t.Fatal(err)
		}
		observation, err := a.Observe(context.Background(), ref)
		if err != nil || observation.State != daemon.OutcomeReady || observation.Outcome.Kind() != daemon.Abandoned {
			t.Fatalf("observation: %+v %v", observation, err)
		}
	}
}
