package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestWorkItemProvenance(t *testing.T) {
	forEachSQLRepository(t, func(t *testing.T, repo *SQLRepository, _ func(*testing.T) *SQLRepository) {
		ctx := context.Background()
		definition := attemptDefinition()
		otherVersion, otherDefinition := definition, definition
		otherVersion.Version++
		otherDefinition.ID = "other-definition"
		for _, value := range []domain.WorkflowDefinition{definition, otherVersion, otherDefinition} {
			if err := repo.CreateWorkflowDefinition(ctx, value); err != nil {
				t.Fatal(err)
			}
		}
		blackboard := repositoryBlackboardDefinition()
		if err := repo.CreateBlackboardDefinition(ctx, blackboard); err != nil {
			t.Fatal(err)
		}
		original := domain.WorkItem{ID: "original", Definition: definition.Binding(), Status: domain.WorkItemStatusOpen, Title: "Original", Goal: "Keep provenance", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime}
		other := original
		other.ID = "other"
		board := original
		board.ID, board.Definition = "board", blackboard.Binding()
		copy := original
		copy.ID, copy.StartedOverFromWorkItemID = "copy", &original.ID
		// Creation must accept an Open source: Start over ends it later in this transaction.
		if err := repo.Update(ctx, func(store application.WriteStore) error {
			for _, value := range []domain.WorkItem{original, other, board, copy} {
				if err := store.CreateWorkItem(value); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		for _, scenario := range []struct {
			name   string
			mutate func(*domain.WorkItem)
			want   error
			reason string
		}{
			{"blackboard-source", func(w *domain.WorkItem) { w.StartedOverFromWorkItemID = &board.ID }, application.ErrConflict, "same Workflow Definition binding"},
			{"different-version", func(w *domain.WorkItem) { w.Definition = otherVersion.Binding() }, application.ErrConflict, "same Workflow Definition binding"},
			{"different-definition", func(w *domain.WorkItem) { w.Definition = otherDefinition.Binding() }, application.ErrConflict, "same Workflow Definition binding"},
			{"missing-source", func(w *domain.WorkItem) { id := domain.WorkItemID("missing"); w.StartedOverFromWorkItemID = &id }, application.ErrNotFound, ""},
			{"self-source", func(w *domain.WorkItem) { w.StartedOverFromWorkItemID = &w.ID }, domain.ErrInvalidModel, "must reference another Workflow WorkItem"},
			{"blackboard-target", func(w *domain.WorkItem) { w.Definition = blackboard.Binding() }, domain.ErrInvalidModel, "must reference another Workflow WorkItem"},
		} {
			t.Run("create/"+scenario.name, func(t *testing.T) {
				value := copy
				value.ID = domain.WorkItemID(scenario.name)
				scenario.mutate(&value)
				if scenario.want != domain.ErrInvalidModel {
					if err := value.Validate(); err != nil {
						t.Fatalf("invalid fixture: %v", err)
					}
				}
				err := repo.Update(ctx, func(store application.WriteStore) error { return store.CreateWorkItem(value) })
				if !errors.Is(err, scenario.want) || (scenario.reason != "" && !strings.Contains(err.Error(), scenario.reason)) {
					t.Fatalf("create: %v", err)
				}
				var count int
				if err := repo.db.QueryRowContext(ctx, rebind(repo.dialect, "SELECT COUNT(*) FROM work_items WHERE id = ?"), value.ID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rejected creation persisted: count=%d, err=%v", count, err)
				}
			})
		}

		read := func(id domain.WorkItemID) (sql.NullString, domain.DefinitionBinding, string) {
			t.Helper()
			var source sql.NullString
			var binding domain.DefinitionBinding
			var payload string
			if err := repo.db.QueryRowContext(ctx, rebind(repo.dialect, "SELECT started_over_from_work_item_id, definition_id, definition_version, mode, payload FROM work_items WHERE id = ?"), id).Scan(&source, &binding.ID, &binding.Version, &binding.Mode, &payload); err != nil {
				t.Fatal(err)
			}
			return source, binding, payload
		}
		for _, scenario := range []struct {
			name   string
			base   domain.WorkItem
			mutate func(*domain.WorkItem)
			reason string
		}{
			{"add-source", original, func(w *domain.WorkItem) { w.StartedOverFromWorkItemID = &other.ID }, "start-over source is immutable"},
			{"remove-source", copy, func(w *domain.WorkItem) { w.StartedOverFromWorkItemID = nil }, "start-over source is immutable"},
			{"replace-source", copy, func(w *domain.WorkItem) { w.StartedOverFromWorkItemID = &other.ID }, "start-over source is immutable"},
			{"change-definition", copy, func(w *domain.WorkItem) { w.Definition = otherDefinition.Binding() }, "definition binding is immutable"},
			{"change-version", copy, func(w *domain.WorkItem) { w.Definition = otherVersion.Binding() }, "definition binding is immutable"},
			{"change-mode", original, func(w *domain.WorkItem) { w.Definition = blackboard.Binding() }, "definition binding is immutable"},
		} {
			t.Run("save/"+scenario.name, func(t *testing.T) {
				beforeSource, beforeBinding, beforePayload := read(scenario.base.ID)
				value := scenario.base
				value.Version++
				value.Title = "Changed"
				scenario.mutate(&value)
				if err := value.Validate(); err != nil {
					t.Fatalf("invalid fixture: %v", err)
				}
				err := repo.Update(ctx, func(store application.WriteStore) error { return store.SaveWorkItem(value) })
				if !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), scenario.reason) {
					t.Fatalf("save: %v", err)
				}
				afterSource, afterBinding, afterPayload := read(value.ID)
				if afterSource != beforeSource || afterBinding != beforeBinding || afterPayload != beforePayload {
					t.Fatal("rejected save changed stored columns or payload")
				}
			})
		}
		for _, value := range []domain.WorkItem{original, copy} {
			value.Version++
			value.Title = "Updated with unchanged provenance"
			if err := repo.Update(ctx, func(store application.WriteStore) error { return store.SaveWorkItem(value) }); err != nil {
				t.Fatalf("save unchanged provenance: %v", err)
			}
			source, binding, payload := read(value.ID)
			stored, err := decodeJSON[domain.WorkItem](payload)
			if err != nil {
				t.Fatal(err)
			}
			if binding != value.Definition || stored.Title != value.Title || stored.Version != value.Version || source.Valid != (stored.StartedOverFromWorkItemID != nil) || (source.Valid && source.String != string(*stored.StartedOverFromWorkItemID)) {
				t.Fatal("save did not preserve consistent provenance")
			}
		}
	})
}
