package domain

import (
	"strings"
	"testing"
)

func TestActorIDReservedPathSegments(t *testing.T) {
	for _, kind := range []ActorKind{ActorHuman, ActorAgent} {
		for _, id := range []ActorID{".", ".."} {
			err := (ActorRef{Kind: kind, ID: id}).Validate()
			if err == nil || !strings.Contains(err.Error(), "executor.id: must not be a reserved URL path segment") {
				t.Fatalf("%s %q: expected reserved path segment validation, got %v", kind, id, err)
			}
		}
		for _, id := range []ActorID{"alice", "admin-0123", "agent_1.2", "...", " 人 / ID ", " . ", "%2e"} {
			if err := (ActorRef{Kind: kind, ID: id}).Validate(); err != nil {
				t.Fatalf("%s %q: %v", kind, id, err)
			}
		}
		for _, id := range []ActorID{"", " ", "\t", "\u3000"} {
			err := (ActorRef{Kind: kind, ID: id}).Validate()
			if err == nil || !strings.Contains(err.Error(), "is required") {
				t.Fatalf("%q: expected required validation, got %v", id, err)
			}
		}
	}
}
