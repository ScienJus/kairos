package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

var repositoryTestTime = time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
var repositoryTestIDSequence atomic.Int64

func TestSQLRepositoryContract(t *testing.T) {
	forEachSQLRepository(t, func(
		t *testing.T,
		repository *SQLRepository,
		openPeer func(*testing.T) *SQLRepository,
	) {
		ctx := context.Background()
		workflow := repositoryWorkflowDefinition()
		blackboard := repositoryBlackboardDefinition()
		if err := repository.CreateWorkflowDefinition(ctx, workflow); err != nil {
			t.Fatalf("create workflow definition: %v", err)
		}
		if err := repository.CreateBlackboardDefinition(ctx, blackboard); err != nil {
			t.Fatalf("create blackboard definition: %v", err)
		}

		t.Run("workflow round trip", func(t *testing.T) {
			testWorkflowRoundTrip(t, repository, workflow)
		})
		t.Run("definition metadata batch", func(t *testing.T) {
			testDefinitionMetadataBatch(t, repository, workflow, blackboard)
		})
		t.Run("queryable domain columns", func(t *testing.T) {
			testQueryableDomainColumns(t, repository, workflow, blackboard)
		})
		t.Run("mutable record timestamps", func(t *testing.T) {
			testMutableRecordTimestamps(t, repository, blackboard)
		})
		t.Run("completed Artifact operation retention", func(t *testing.T) {
			testCompletedArtifactOperationRetention(t, repository)
		})
		t.Run("blackboard lifecycle candidates", func(t *testing.T) {
			testBlackboardLifecycleCandidates(t, repository, blackboard)
		})
		t.Run("rollback", func(t *testing.T) {
			testTransactionRollback(t, repository, blackboard)
		})
		t.Run("concurrent claim", func(t *testing.T) {
			testConcurrentClaim(t, repository, openPeer(t), blackboard)
		})
		t.Run("concurrent Definition versions", func(t *testing.T) {
			testConcurrentDefinitionVersions(t, repository, openPeer(t), workflow)
		})
		t.Run("claim lease columns", func(t *testing.T) {
			testClaimLeaseColumns(t, repository, blackboard)
		})
		t.Run("artifact garbage collection", func(t *testing.T) {
			testArtifactGarbageCollection(t, repository, blackboard)
		})
		t.Run("artifact blob conflict", func(t *testing.T) {
			testArtifactBlobConflict(t, repository)
		})
		t.Run("concurrent blackboard appends", func(t *testing.T) {
			testConcurrentBlackboardPlanning(t, repository, openPeer(t), blackboard)
		})
		t.Run("concurrent idempotent planning", func(t *testing.T) {
			testConcurrentIdempotentPlanning(t, repository, openPeer(t), blackboard)
		})
		t.Run("hierarchy closure race", func(t *testing.T) {
			testHierarchyClosureRace(t, repository, openPeer(t), blackboard)
		})
		t.Run("concurrent child appends", func(t *testing.T) {
			testConcurrentChildAppends(t, repository, openPeer(t), blackboard)
		})
		t.Run("concurrent reciprocal relations", func(t *testing.T) {
			testConcurrentReciprocalRelations(t, repository, openPeer(t), blackboard)
		})
		if repository.dialect == dialectPostgres {
			t.Run("independent work items do not block", func(t *testing.T) {
				testIndependentWorkItemsDoNotBlock(t, repository, openPeer(t), blackboard)
			})
		}
		t.Run("cursor pagination", func(t *testing.T) {
			testCursorPagination(t, repository, workflow, blackboard)
		})
	})
}

func testCompletedArtifactOperationRetention(t *testing.T, repository *SQLRepository) {
	t.Helper()
	ctx := context.Background()
	actor := domain.ActorRef{Kind: domain.ActorAgent, ID: "upload-retention-agent"}
	cutoff := repositoryTestTime.Add(-time.Hour)
	records := []application.IdempotencyRecord{
		{Actor: actor, OperationID: "expired-upload", Operation: application.ArtifactUploadOperation, Status: application.IdempotencyCompleted, RequestHash: "expired", Response: `{}`, CreatedAt: cutoff.Add(-time.Minute)},
		{Actor: actor, OperationID: "expired-external", Operation: application.CreateArtifactOperation, Status: application.IdempotencyCompleted, RequestHash: "external", Response: `{}`, CreatedAt: cutoff.Add(-time.Minute)},
		{Actor: actor, OperationID: "recently-completed-upload", Operation: application.ArtifactUploadOperation, Status: application.IdempotencyCompleted, RequestHash: "recent", Response: `{}`, CreatedAt: cutoff.Add(-time.Minute)},
		{Actor: actor, OperationID: "durable-task", Operation: "create_blackboard_task", Status: application.IdempotencyCompleted, RequestHash: "task", Response: `{}`, CreatedAt: cutoff.Add(-time.Minute)},
	}
	if err := repository.Update(ctx, func(store application.WriteStore) error {
		for _, record := range records {
			if err := store.CreateIdempotencyRecord(record); err != nil {
				return err
			}
		}
		if err := store.SaveIdempotencyRecord(records[2], cutoff.Add(time.Minute)); err != nil {
			return err
		}
		deleted, err := store.DeleteCompletedArtifactOperationRecords(cutoff)
		if err != nil {
			return err
		}
		if deleted != 2 {
			return fmt.Errorf("deleted %d completed Artifact operation records, want 2", deleted)
		}
		return nil
	}); err != nil {
		t.Fatalf("apply completed upload retention: %v", err)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		if _, err := store.GetIdempotencyRecord(actor, "expired-upload"); !errors.Is(err, application.ErrNotFound) {
			return fmt.Errorf("expired upload lookup = %v, want not found", err)
		}
		if _, err := store.GetIdempotencyRecord(actor, "expired-external"); !errors.Is(err, application.ErrNotFound) {
			return fmt.Errorf("expired external Artifact lookup = %v, want not found", err)
		}
		for _, operationID := range []string{"recently-completed-upload", "durable-task"} {
			if _, err := store.GetIdempotencyRecord(actor, operationID); err != nil {
				return fmt.Errorf("retained operation %q: %w", operationID, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func testConcurrentDefinitionVersions(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.WorkflowDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	actors := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "definition-author-a"}, Role: "architect"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "definition-author-b"}, Role: "architect"},
	}
	definitionID := domain.DefinitionID("concurrent-definition-versions")
	base, err := services[0].CreateWorkflowDefinition(ctx, application.CreateWorkflowDefinitionCommand{
		Identity: actors[0],
		Metadata: application.DefinitionMetadataCommand{ID: definitionID, Name: "Concurrent Definition v1"},
		Graph:    definition.Graph,
	})
	if err != nil || base.Version != 1 {
		t.Fatalf("create initial concurrent Definition: %#v, err=%v", base, err)
	}

	start := make(chan struct{})
	baseVersion := base.Version
	versions := make([]int64, len(services))
	errorsByAuthor := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			created, err := service.CreateWorkflowDefinition(ctx, application.CreateWorkflowDefinitionCommand{
				Identity: actors[index], BaseVersion: &baseVersion,
				Metadata: application.DefinitionMetadataCommand{
					ID: definitionID, Name: fmt.Sprintf("Concurrent Definition by author %d", index+1),
				},
				Graph: definition.Graph,
			})
			errorsByAuthor[index] = err
			versions[index] = created.Version
		}()
	}
	close(start)
	wait.Wait()
	successes := make([]int64, 0, 1)
	staleAuthor := -1
	for index, err := range errorsByAuthor {
		if err == nil {
			successes = append(successes, versions[index])
			continue
		}
		if !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "advanced from version 1 to 2") {
			t.Fatalf("concurrent Definition append error = %v, want stale-base conflict", err)
		}
		staleAuthor = index
	}
	if !slices.Equal(successes, []int64{2}) || staleAuthor < 0 {
		t.Fatalf("concurrent Definition results = versions %v errors %v", versions, errorsByAuthor)
	}
	retryBase := int64(2)
	retried, err := services[staleAuthor].CreateWorkflowDefinition(ctx, application.CreateWorkflowDefinitionCommand{
		Identity: actors[staleAuthor], BaseVersion: &retryBase,
		Metadata: application.DefinitionMetadataCommand{ID: definitionID, Name: "Rebased concurrent Definition"},
		Graph:    definition.Graph,
	})
	if err != nil || retried.Version != 3 {
		t.Fatalf("retry concurrent Definition append: %#v, err=%v", retried, err)
	}
}

func testCursorPagination(
	t *testing.T,
	repository *SQLRepository,
	workflow domain.WorkflowDefinition,
	blackboard domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	secondWorkflow := workflow
	secondWorkflow.ID = "pagination-workflow"
	secondWorkflow.Name = "Pagination Workflow"
	if err := repository.CreateWorkflowDefinition(ctx, secondWorkflow); err != nil {
		t.Fatalf("create pagination Workflow: %v", err)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		first, err := store.ListWorkflowDefinitionCatalog(application.DefinitionCatalogFilter{Page: application.PageRequest[application.DefinitionCatalogCursor]{Limit: 1}})
		if err != nil {
			return err
		}
		if len(first) != 2 || first[0].ID == first[1].ID {
			return fmt.Errorf("first Definition catalog repository page = %#v", first)
		}
		cursor := application.DefinitionCatalogCursor{ID: first[0].ID}
		second, err := store.ListWorkflowDefinitionCatalog(application.DefinitionCatalogFilter{Page: application.PageRequest[application.DefinitionCatalogCursor]{Limit: 1, After: &cursor}})
		if err != nil {
			return err
		}
		if len(second) == 0 || second[0].ID != first[1].ID {
			return fmt.Errorf("second Definition catalog repository page = %#v after %#v", second, first)
		}
		return nil
	}); err != nil {
		t.Fatalf("paginate Definition catalog: %v", err)
	}
	latestWorkflow := secondWorkflow
	latestWorkflow.Version = 2
	latestWorkflow.Name = "Pagination Workflow v2"
	if err := repository.CreateWorkflowDefinition(ctx, latestWorkflow); err != nil {
		t.Fatalf("create latest Workflow version: %v", err)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		latest, err := store.GetLatestWorkflowDefinition(secondWorkflow.ID)
		if err != nil {
			return err
		}
		if latest.Version != 2 {
			return fmt.Errorf("latest stored Definition = v%d, want v2", latest.Version)
		}
		catalog, err := store.ListWorkflowDefinitionCatalog(application.DefinitionCatalogFilter{
			Page: application.PageRequest[application.DefinitionCatalogCursor]{Limit: 50},
		})
		if err != nil {
			return err
		}
		var catalogVersion int64
		for _, definition := range catalog {
			if definition.ID == secondWorkflow.ID {
				catalogVersion = definition.Version
			}
		}
		if catalogVersion != 2 {
			return fmt.Errorf("Definition catalog version = v%d, want v2", catalogVersion)
		}
		versions, err := store.ListWorkflowDefinitionVersions(application.DefinitionVersionFilter{
			ID: secondWorkflow.ID, Page: application.PageRequest[application.DefinitionVersionCursor]{Limit: 1},
		})
		if err != nil {
			return err
		}
		if len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 {
			return fmt.Errorf("Definition version repository page = %#v", versions)
		}
		return nil
	}); err != nil {
		t.Fatalf("query Definition catalog and versions: %v", err)
	}

	service := repositoryTestService(t, repository)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "pagination-agent"}, Role: "generalist"}
	workItems := make([]domain.WorkItem, 0, 2)
	for _, title := range []string{"Pagination one", "Pagination two"} {
		workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
			Definition: blackboard.Binding(), Identity: agent, Title: title, Goal: "Verify keyset pagination", Tags: []string{"pagination-contract"},
		})
		if err != nil {
			t.Fatalf("create pagination WorkItem: %v", err)
		}
		workItems = append(workItems, workItem)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		first, err := store.ListWorkItems(application.WorkItemFilter{
			Tags: []string{"pagination-contract"}, Page: application.PageRequest[application.WorkItemCursor]{Limit: 1},
		})
		if err != nil {
			return err
		}
		if len(first) != 2 || first[0].ID == first[1].ID {
			return fmt.Errorf("first WorkItem repository page = %#v", first)
		}
		cursor := application.WorkItemCursor{UpdatedAt: first[0].UpdatedAt, ID: first[0].ID}
		second, err := store.ListWorkItems(application.WorkItemFilter{
			Tags: []string{"pagination-contract"}, Page: application.PageRequest[application.WorkItemCursor]{Limit: 1, After: &cursor},
		})
		if err != nil {
			return err
		}
		if len(second) != 1 || second[0].ID != first[1].ID {
			return fmt.Errorf("second WorkItem repository page = %#v after %#v", second, first)
		}
		return nil
	}); err != nil {
		t.Fatalf("paginate WorkItems: %v", err)
	}

	task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItems[0].ID, Identity: agent, Title: "Submit pagination artifacts", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create pagination Task: %v", err)
	}
	claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim pagination Task: %v", err)
	}
	artifactIDs := make([]domain.ArtifactID, 0, 2)
	for index, name := range []string{"first", "second"} {
		artifact, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{
			TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Name: name, URI: fmt.Sprintf("https://example.test/pagination/%d", index),
		})
		if err != nil {
			t.Fatalf("create pagination Artifact: %v", err)
		}
		artifactIDs = append(artifactIDs, artifact.ID)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "done", ArtifactIDs: artifactIDs,
	}); err != nil {
		t.Fatalf("submit pagination Artifacts: %v", err)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		first, err := store.ListArtifacts(application.ArtifactFilter{
			WorkItemID: workItems[0].ID, SubmittedOnly: true, Page: application.PageRequest[application.ArtifactCursor]{Limit: 1},
		})
		if err != nil {
			return err
		}
		if len(first) != 2 || first[0].ID == first[1].ID {
			return fmt.Errorf("first Artifact repository page = %#v", first)
		}
		cursor := application.ArtifactCursor{CreatedAt: first[0].CreatedAt, ID: first[0].ID}
		second, err := store.ListArtifacts(application.ArtifactFilter{
			WorkItemID: workItems[0].ID, SubmittedOnly: true,
			Page: application.PageRequest[application.ArtifactCursor]{Limit: 1, After: &cursor},
		})
		if err != nil {
			return err
		}
		if len(second) != 1 || second[0].ID != first[1].ID {
			return fmt.Errorf("second Artifact repository page = %#v after %#v", second, first)
		}
		return nil
	}); err != nil {
		t.Fatalf("paginate Artifacts: %v", err)
	}

	otherTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItems[0].ID, Identity: agent, Title: "Submit another Task artifact", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create other Artifact Task: %v", err)
	}
	otherClaim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: otherTask.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim other Artifact Task: %v", err)
	}
	otherArtifact, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{
		TaskID: otherTask.ID, ClaimID: otherClaim.ID, Identity: agent, Name: "other", URI: "https://example.test/pagination/other",
	})
	if err != nil {
		t.Fatalf("create other Task Artifact: %v", err)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: otherTask.ID, ClaimID: otherClaim.ID, Identity: agent, Result: "done", ArtifactIDs: []domain.ArtifactID{otherArtifact.ID},
	}); err != nil {
		t.Fatalf("submit other Task Artifact: %v", err)
	}
	separateTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItems[1].ID, Identity: agent, Title: "Separate WorkItem Claim", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create separate WorkItem Task: %v", err)
	}
	separateClaim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: separateTask.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim separate WorkItem Task: %v", err)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		claims, err := store.ListClaimsByWorkItem(workItems[0].ID)
		if err != nil {
			return err
		}
		if len(claims) != 2 || claims[0].ID != claim.ID || claims[1].ID != otherClaim.ID {
			return fmt.Errorf("WorkItem Claims = %#v, want %q then %q", claims, claim.ID, otherClaim.ID)
		}
		if slices.ContainsFunc(claims, func(item domain.Claim) bool { return item.ID == separateClaim.ID }) {
			return fmt.Errorf("WorkItem Claims contain Claim %q from another WorkItem", separateClaim.ID)
		}
		return nil
	}); err != nil {
		t.Fatalf("list Claims by WorkItem: %v", err)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		artifacts, err := store.ListArtifacts(application.ArtifactFilter{
			WorkItemID: workItems[0].ID, TaskID: task.ID, SubmittedOnly: true,
		})
		if err != nil {
			return err
		}
		if len(artifacts) != len(artifactIDs) {
			return fmt.Errorf("Task-filtered Artifacts = %#v, want %d", artifacts, len(artifactIDs))
		}
		for _, artifact := range artifacts {
			if artifact.TaskID != task.ID {
				return fmt.Errorf("Task-filtered Artifact belongs to %q, want %q", artifact.TaskID, task.ID)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("filter Artifacts by Task: %v", err)
	}

	humanTasks := make([]domain.Task, 0, len(workItems))
	for index, workItem := range workItems {
		humanTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
			WorkItemID: workItem.ID, Identity: agent, Title: fmt.Sprintf("Human attention %d", index+1), Executor: domain.ExecutorHuman,
		})
		if err != nil {
			t.Fatalf("create Human Attention Task: %v", err)
		}
		humanTasks = append(humanTasks, humanTask)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		all, err := store.ListHumanAttention(application.PageRequest[application.HumanAttentionCursor]{})
		if err != nil {
			return err
		}
		for _, task := range humanTasks {
			if !slices.ContainsFunc(all, func(item application.HumanAttentionItem) bool {
				return item.Task != nil && item.Task.ID == task.ID
			}) {
				return fmt.Errorf("Human Attention collection does not contain Task %q: %#v", task.ID, all)
			}
		}
		if len(all) < 2 {
			return fmt.Errorf("Human Attention collection = %#v, want at least two items", all)
		}
		first, err := store.ListHumanAttention(application.PageRequest[application.HumanAttentionCursor]{Limit: 1})
		if err != nil {
			return err
		}
		if len(first) != 2 || first[0].Cursor() != all[0].Cursor() || first[1].Cursor() != all[1].Cursor() {
			return fmt.Errorf("first Human Attention repository page = %#v, all = %#v", first, all)
		}
		cursor := first[0].Cursor()
		second, err := store.ListHumanAttention(application.PageRequest[application.HumanAttentionCursor]{Limit: 1, After: &cursor})
		if err != nil {
			return err
		}
		if len(second) == 0 || second[0].Cursor() != all[1].Cursor() || second[0].Cursor() == first[0].Cursor() {
			return fmt.Errorf("second Human Attention repository page = %#v after %#v", second, first)
		}
		return nil
	}); err != nil {
		t.Fatalf("paginate Human Attention: %v", err)
	}
}

func testMutableRecordTimestamps(t *testing.T, repository *SQLRepository, definition domain.BlackboardDefinition) {
	t.Helper()
	ctx := context.Background()
	createdAt := repositoryTestTime.Add(10*time.Minute + 123456789*time.Nanosecond)
	updatedAt := repositoryTestTime.Add(20*time.Minute + 987654321*time.Nanosecond)
	creator, err := application.NewService(repository, repositoryTestClockAt{now: createdAt}, &repositoryTestIDs{})
	if err != nil {
		t.Fatalf("new timestamp creator: %v", err)
	}
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "timestamp-agent"}, Role: "generalist"}
	workItem, err := creator.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Mutable timestamps", Goal: "Verify persisted updates",
	})
	if err != nil {
		t.Fatalf("create timestamp WorkItem: %v", err)
	}
	task, err := creator.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Submit timestamped Artifact", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create timestamp Task: %v", err)
	}
	claim, err := creator.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("create timestamp Claim: %v", err)
	}
	artifact, err := creator.CreateArtifact(ctx, application.CreateArtifactCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Name: "timestamp", URI: "https://example.test/timestamp",
	})
	if err != nil {
		t.Fatalf("create timestamp Artifact: %v", err)
	}
	updater, err := application.NewService(repository, repositoryTestClockAt{now: updatedAt}, &repositoryTestIDs{})
	if err != nil {
		t.Fatalf("new timestamp updater: %v", err)
	}
	if _, err := updater.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "done", ArtifactIDs: []domain.ArtifactID{artifact.ID},
	}); err != nil {
		t.Fatalf("submit timestamp Task: %v", err)
	}

	pending := application.IdempotencyRecord{
		Actor: agent.Actor, OperationID: "timestamp-pending", Operation: "upload_artifact",
		Status: application.IdempotencyPending, RequestHash: "timestamp-hash", Response: `{}`, CreatedAt: createdAt,
	}
	if err := repository.Update(ctx, func(store application.WriteStore) error {
		if err := store.CreateIdempotencyRecord(pending); err != nil {
			return err
		}
		pending.Response = `{"digest":"sha256:timestamp"}`
		return store.SaveIdempotencyRecord(pending, updatedAt)
	}); err != nil {
		t.Fatalf("update timestamp Idempotency Record: %v", err)
	}

	wantCreated := createdAt.UTC().Truncate(time.Microsecond)
	wantUpdated := updatedAt.UTC().Truncate(time.Microsecond)
	if !workItem.CreatedAt.Equal(wantCreated) || !task.CreatedAt.Equal(wantCreated) || !claim.ClaimedAt.Equal(wantCreated) || !artifact.CreatedAt.Equal(wantCreated) {
		t.Fatalf("application timestamps were not normalized to %s: work=%s task=%s claim=%s artifact=%s", wantCreated, workItem.CreatedAt, task.CreatedAt, claim.ClaimedAt, artifact.CreatedAt)
	}
	var claimUpdated, artifactCreated, artifactUpdated, recordCreated, recordUpdated scannedTime
	query := rebind(repository.dialect, "SELECT updated_at FROM claims WHERE id = ?")
	if err := repository.db.QueryRowContext(ctx, query, claim.ID).Scan(&claimUpdated); err != nil {
		t.Fatalf("query Claim updated_at: %v", err)
	}
	query = rebind(repository.dialect, "SELECT created_at, updated_at FROM artifacts WHERE id = ?")
	if err := repository.db.QueryRowContext(ctx, query, artifact.ID).Scan(&artifactCreated, &artifactUpdated); err != nil {
		t.Fatalf("query Artifact timestamps: %v", err)
	}
	query = rebind(repository.dialect, "SELECT created_at, updated_at FROM idempotency_records WHERE actor_kind = ? AND actor_id = ? AND operation_id = ?")
	if err := repository.db.QueryRowContext(ctx, query, agent.Actor.Kind, agent.Actor.ID, pending.OperationID).Scan(&recordCreated, &recordUpdated); err != nil {
		t.Fatalf("query Idempotency timestamps: %v", err)
	}
	if !claimUpdated.Time.Equal(wantUpdated) {
		t.Fatalf("Claim updated_at = %s, want %s", claimUpdated.Time, wantUpdated)
	}
	if !artifactCreated.Time.Equal(wantCreated) || !artifactUpdated.Time.Equal(wantUpdated) {
		t.Fatalf("Artifact timestamps = %s/%s, want %s/%s", artifactCreated.Time, artifactUpdated.Time, wantCreated, wantUpdated)
	}
	if !recordCreated.Time.Equal(wantCreated) || !recordUpdated.Time.Equal(wantUpdated) {
		t.Fatalf("Idempotency timestamps = %s/%s, want %s/%s", recordCreated.Time, recordUpdated.Time, wantCreated, wantUpdated)
	}
}

func testQueryableDomainColumns(
	t *testing.T,
	repository *SQLRepository,
	workflow domain.WorkflowDefinition,
	blackboard domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	olderTime := repositoryTestTime.Add(time.Minute + 123456789*time.Nanosecond)
	newerTime := repositoryTestTime.Add(2*time.Minute + 987654321*time.Nanosecond)
	older := domain.WorkItem{
		ID: "queryable-older", Definition: blackboard.Binding(), Status: domain.WorkItemStatusOpen,
		AcceptanceMode: domain.WorkItemAcceptanceNone, Title: "Older", Goal: "Verify query columns",
		Tags: []string{"osr05", "auth"}, CreatedAt: olderTime, UpdatedAt: olderTime,
	}
	newer := domain.WorkItem{
		ID: "queryable-newer", Definition: blackboard.Binding(), Status: domain.WorkItemStatusOpen,
		AcceptanceMode: domain.WorkItemAcceptanceNone, Title: "Newer", Goal: "Verify query ordering",
		Tags: []string{"osr05"}, CreatedAt: newerTime, UpdatedAt: newerTime,
	}
	workflowItem := domain.WorkItem{
		ID: "queryable-workflow", Definition: workflow.Binding(), Status: domain.WorkItemStatusOpen,
		AcceptanceMode: domain.WorkItemAcceptanceNone, Title: "Workflow", Goal: "Verify mode filtering",
		Tags: []string{"osr05", "workflow"}, CreatedAt: newerTime, UpdatedAt: newerTime,
	}
	workflowV2 := workflow
	workflowV2.Version++
	workflowV2.UpdatedAt = newerTime
	blackboardV2 := blackboard
	blackboardV2.Version++
	blackboardV2.UpdatedAt = newerTime
	workflowTaskID := workflow.Graph.Tasks[0].ID
	workflowActivationID := domain.WorkflowTaskActivationID("queryable-workflow-activation")
	workflowExecution := workflow.Graph.Tasks[0].Execution
	workflowReview := workflow.Graph.Tasks[0].ReviewPolicy
	relation := domain.TaskRelation{
		WorkItemID: older.ID, FromTaskID: "queryable-agent-match", ToTaskID: "queryable-unrestricted", CreatedAt: olderTime,
	}
	workflowRelation := domain.TaskRelation{
		WorkItemID: workflowItem.ID, FromTaskID: "queryable-workflow-blocker", ToTaskID: "queryable-workflow-blocked", CreatedAt: newerTime,
	}
	tasks := []domain.Task{
		{ID: "queryable-agent-match", WorkItemID: older.ID, Status: domain.TaskStatusPending, Title: "Agent match", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Tags: []string{"osr05-task", "auth"}, CreatedAt: olderTime, UpdatedAt: olderTime},
		{ID: "queryable-agent-other-role", WorkItemID: older.ID, Status: domain.TaskStatusPending, Title: "Agent other role", Executor: domain.ExecutorAgent, AllowedRoles: []string{"frontend"}, Tags: []string{"osr05-task", "auth"}, Position: 1, CreatedAt: olderTime, UpdatedAt: olderTime},
		{ID: "queryable-human", WorkItemID: older.ID, Status: domain.TaskStatusPending, Title: "Human", Executor: domain.ExecutorHuman, Tags: []string{"osr05-task", "auth"}, Position: 2, CreatedAt: olderTime, UpdatedAt: olderTime},
		{ID: "queryable-unrestricted", WorkItemID: older.ID, Status: domain.TaskStatusPending, Title: "Unrestricted", Executor: domain.ExecutorAgent, Tags: []string{"osr05-task", "other"}, Position: 3, CreatedAt: olderTime, UpdatedAt: olderTime},
		{ID: "queryable-workflow-blocked", WorkItemID: workflowItem.ID, WorkflowTaskID: &workflowTaskID, WorkflowActivationID: &workflowActivationID, Status: domain.TaskStatusPending, Title: "Blocked Workflow task", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Position: 0, Execution: &workflowExecution, ReviewPolicy: &workflowReview, CreatedAt: newerTime, UpdatedAt: newerTime},
		{ID: "queryable-workflow-blocker", WorkItemID: workflowItem.ID, WorkflowTaskID: &workflowTaskID, WorkflowActivationID: &workflowActivationID, Status: domain.TaskStatusPending, Title: "Unfinished Human predecessor", Executor: domain.ExecutorHuman, Position: 1, Execution: &workflowExecution, ReviewPolicy: &workflowReview, CreatedAt: newerTime, UpdatedAt: newerTime},
		{ID: "queryable-workflow-task", WorkItemID: workflowItem.ID, WorkflowTaskID: &workflowTaskID, WorkflowActivationID: &workflowActivationID, Status: domain.TaskStatusPending, Title: "Workflow tags are descriptive", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Tags: []string{"workflow-label"}, Position: 2, Execution: &workflowExecution, ReviewPolicy: &workflowReview, CreatedAt: newerTime, UpdatedAt: newerTime},
	}
	if err := repository.Update(ctx, func(store application.WriteStore) error {
		if err := store.CreateWorkflowDefinition(workflowV2); err != nil {
			return err
		}
		if err := store.CreateBlackboardDefinition(blackboardV2); err != nil {
			return err
		}
		for _, workItem := range []domain.WorkItem{older, newer, workflowItem} {
			if err := store.CreateWorkItem(workItem); err != nil {
				return err
			}
		}
		for _, task := range tasks {
			if err := store.CreateTask(task); err != nil {
				return err
			}
		}
		if err := store.CreateTaskRelation(relation); err != nil {
			return err
		}
		if err := store.CreateTaskRelation(workflowRelation); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("create queryable records: %v", err)
	}

	if err := repository.View(ctx, func(store application.ReadStore) error {
		persistedOlder, err := store.GetWorkItem(older.ID)
		if err != nil {
			return err
		}
		if !persistedOlder.CreatedAt.Equal(olderTime.Truncate(time.Microsecond)) || !persistedOlder.UpdatedAt.Equal(olderTime.Truncate(time.Microsecond)) {
			return fmt.Errorf("payload timestamps = %s/%s, want microsecond precision", persistedOlder.CreatedAt, persistedOlder.UpdatedAt)
		}
		latestWorkflow, err := store.GetLatestWorkflowDefinition(workflow.ID)
		if err != nil || latestWorkflow.Version != workflowV2.Version {
			return fmt.Errorf("latest Workflow = version %d, err %v; want %d", latestWorkflow.Version, err, workflowV2.Version)
		}
		latestBlackboard, err := store.GetLatestBlackboardDefinition(blackboard.ID)
		if err != nil || latestBlackboard.Version != blackboardV2.Version {
			return fmt.Errorf("latest Blackboard = version %d, err %v; want %d", latestBlackboard.Version, err, blackboardV2.Version)
		}
		workItems, err := store.ListWorkItems(application.WorkItemFilter{
			Statuses: []domain.WorkItemStatus{domain.WorkItemStatusOpen},
			Modes:    []domain.CoordinationMode{domain.CoordinationModeBlackboard},
			Tags:     []string{"osr05"},
		})
		if err != nil {
			return err
		}
		if len(workItems) != 2 || workItems[0].ID != newer.ID || workItems[1].ID != older.ID {
			return fmt.Errorf("filtered work items = %v, want %q then %q", workItemIDs(workItems), newer.ID, older.ID)
		}
		matching, err := store.ListWorkItems(application.WorkItemFilter{Tags: []string{"osr05", "auth"}})
		if err != nil {
			return err
		}
		if len(matching) != 1 || matching[0].ID != older.ID {
			return fmt.Errorf("contains-all work items = %v, want only %q", workItemIDs(matching), older.ID)
		}

		agentCandidates, err := store.ListOpenTasks(application.OpenTaskFilter{ActorKind: domain.ActorAgent, Role: "backend", Tags: []string{"osr05-task", "auth"}, Limit: 50})
		if err != nil {
			return err
		}
		if !candidateContains(agentCandidates, "queryable-agent-match") || candidateContains(agentCandidates, "queryable-agent-other-role") || candidateContains(agentCandidates, "queryable-human") {
			return fmt.Errorf("backend candidates = %v", candidateTaskIDs(agentCandidates))
		}
		humanCandidates, err := store.ListOpenTasks(application.OpenTaskFilter{ActorKind: domain.ActorHuman, Tags: []string{"osr05-task", "auth"}, Limit: 50})
		if err != nil {
			return err
		}
		if !candidateContains(humanCandidates, "queryable-human") || candidateContains(humanCandidates, "queryable-agent-match") {
			return fmt.Errorf("human candidates = %v", candidateTaskIDs(humanCandidates))
		}
		unrestricted, err := store.ListOpenTasks(application.OpenTaskFilter{ActorKind: domain.ActorAgent, Role: "any-role", Tags: []string{"osr05-task", "other"}, Limit: 50})
		if err != nil {
			return err
		}
		if !candidateContains(unrestricted, "queryable-unrestricted") {
			return fmt.Errorf("unrestricted candidates = %v", candidateTaskIDs(unrestricted))
		}
		workflowCandidates, err := store.ListOpenTasks(application.OpenTaskFilter{ActorKind: domain.ActorAgent, Role: "backend", Tags: []string{"not-a-workflow-label"}, Limit: 50})
		if err != nil {
			return err
		}
		if !candidateContains(workflowCandidates, "queryable-workflow-task") || candidateContains(workflowCandidates, "queryable-workflow-blocked") {
			return fmt.Errorf("Workflow candidates filtered by descriptive tags: %v", candidateTaskIDs(workflowCandidates))
		}
		limitedWorkflowCandidates, err := store.ListOpenTasks(application.OpenTaskFilter{
			ActorKind: domain.ActorAgent, Role: "backend", Tags: []string{"not-a-workflow-label"}, Limit: 1,
		})
		if err != nil {
			return err
		}
		if len(limitedWorkflowCandidates) != 1 || limitedWorkflowCandidates[0].Task.ID != "queryable-workflow-task" {
			return fmt.Errorf("limited Workflow candidates = %v, want eligible task after blocked task", candidateTaskIDs(limitedWorkflowCandidates))
		}
		limitedCandidates, err := store.ListOpenTasks(application.OpenTaskFilter{ActorKind: domain.ActorAgent, Role: "backend", Limit: 1})
		if err != nil {
			return err
		}
		if len(limitedCandidates) != 1 {
			return fmt.Errorf("limited Task candidates = %v, want one", candidateTaskIDs(limitedCandidates))
		}
		emptyBlackboards, err := store.ListEmptyBlackboards(nil, 1)
		if err != nil {
			return err
		}
		if len(emptyBlackboards) != 1 {
			return fmt.Errorf("limited empty Blackboards = %d, want one", len(emptyBlackboards))
		}
		return nil
	}); err != nil {
		t.Fatalf("query dedicated columns: %v", err)
	}

	arrayExpression := "tags"
	rolesExpression := "allowed_roles"
	if repository.dialect == dialectPostgres {
		arrayExpression = "to_json(tags)::text"
		rolesExpression = "to_json(allowed_roles)::text"
	}
	var tagsJSON, rolesJSON, executor string
	var createdAt, updatedAt scannedTime
	query := rebind(repository.dialect, "SELECT "+arrayExpression+", created_at, updated_at FROM work_items WHERE id = ?")
	if err := repository.db.QueryRowContext(ctx, query, older.ID).Scan(&tagsJSON, &createdAt, &updatedAt); err != nil {
		t.Fatalf("query WorkItem columns: %v", err)
	}
	var storedTags []string
	if err := json.Unmarshal([]byte(tagsJSON), &storedTags); err != nil || !slices.Equal(storedTags, older.Tags) {
		t.Fatalf("stored WorkItem tags = %q (%v), want %v", tagsJSON, err, older.Tags)
	}
	if !createdAt.Time.Equal(olderTime.Truncate(time.Microsecond)) || !updatedAt.Time.Equal(olderTime.Truncate(time.Microsecond)) {
		t.Fatalf("stored WorkItem timestamps = %s/%s, want microsecond-normalized %s", createdAt.Time, updatedAt.Time, olderTime)
	}
	query = rebind(repository.dialect, "SELECT executor, "+rolesExpression+" FROM tasks WHERE id = ?")
	if err := repository.db.QueryRowContext(ctx, query, tasks[0].ID).Scan(&executor, &rolesJSON); err != nil {
		t.Fatalf("query Task columns: %v", err)
	}
	var storedRoles []string
	if err := json.Unmarshal([]byte(rolesJSON), &storedRoles); err != nil || executor != string(domain.ExecutorAgent) || !slices.Equal(storedRoles, tasks[0].AllowedRoles) {
		t.Fatalf("stored Task executor/roles = %q/%q (%v)", executor, rolesJSON, err)
	}
	query = rebind(repository.dialect, "SELECT "+rolesExpression+" FROM tasks WHERE id = ?")
	if err := repository.db.QueryRowContext(ctx, query, tasks[3].ID).Scan(&rolesJSON); err != nil {
		t.Fatalf("query empty roles: %v", err)
	}
	if rolesJSON != "[]" {
		t.Fatalf("stored empty roles = %q, want []", rolesJSON)
	}
	var relationCreatedAt scannedTime
	query = rebind(repository.dialect, "SELECT created_at FROM task_relations WHERE work_item_id = ? AND from_task_id = ? AND to_task_id = ?")
	if err := repository.db.QueryRowContext(ctx, query, relation.WorkItemID, relation.FromTaskID, relation.ToTaskID).Scan(&relationCreatedAt); err != nil {
		t.Fatalf("query TaskRelation created_at: %v", err)
	}
	if !relationCreatedAt.Time.Equal(olderTime.Truncate(time.Microsecond)) {
		t.Fatalf("TaskRelation created_at = %s, want %s", relationCreatedAt.Time, olderTime.Truncate(time.Microsecond))
	}
}

func workItemIDs(values []domain.WorkItem) []domain.WorkItemID {
	result := make([]domain.WorkItemID, len(values))
	for index := range values {
		result[index] = values[index].ID
	}
	return result
}

func candidateContains(values []application.WorkCandidate, id domain.TaskID) bool {
	return slices.Contains(candidateTaskIDs(values), id)
}

func candidateTaskIDs(values []application.WorkCandidate) []domain.TaskID {
	result := make([]domain.TaskID, 0, len(values))
	for _, value := range values {
		if value.Task != nil {
			result = append(result, value.Task.ID)
		}
	}
	return result
}

func testArtifactBlobConflict(t *testing.T, repository *SQLRepository) {
	t.Helper()
	ctx := context.Background()
	original := domain.ArtifactBlob{
		URI:    "kairos://blobs/uploads/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Digest: "sha256:first", Size: 5, CreatedAt: repositoryTestTime,
	}
	if err := repository.Update(ctx, func(store application.WriteStore) error {
		return store.CreateArtifactBlob(original)
	}); err != nil {
		t.Fatalf("create Artifact Blob: %v", err)
	}
	if err := repository.Update(ctx, func(store application.WriteStore) error {
		return store.CreateArtifactBlob(original)
	}); err != nil {
		t.Fatalf("repeat identical Artifact Blob: %v", err)
	}
	for _, changed := range []domain.ArtifactBlob{
		{URI: original.URI, Digest: "sha256:changed", Size: original.Size, CreatedAt: original.CreatedAt},
		{URI: original.URI, Digest: original.Digest, Size: original.Size + 1, CreatedAt: original.CreatedAt},
	} {
		if err := repository.Update(ctx, func(store application.WriteStore) error {
			return store.CreateArtifactBlob(changed)
		}); !errors.Is(err, application.ErrConflict) {
			t.Fatalf("create conflicting Artifact Blob %#v: %v", changed, err)
		}
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		persisted, err := store.GetArtifactBlob(original.URI)
		if err != nil {
			return err
		}
		if persisted.Digest != original.Digest || persisted.Size != original.Size {
			return fmt.Errorf("Artifact Blob = %#v, want %#v", persisted, original)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify Artifact Blob: %v", err)
	}
}

func testArtifactGarbageCollection(t *testing.T, repository *SQLRepository, definition domain.BlackboardDefinition) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "artifact-gc-agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Artifact GC", Goal: "Collect an abandoned Artifact",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Upload", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	artifact, err := service.CreateArtifact(ctx, application.CreateArtifactCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Name: "branch", URI: "https://example.test/abandoned",
	})
	if err != nil {
		t.Fatalf("create Artifact: %v", err)
	}
	if err := service.ReleaseClaim(ctx, application.ReleaseClaimCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: agent}); err != nil {
		t.Fatalf("release claim: %v", err)
	}
	collector, err := application.NewService(repository, repositoryTestClockAt{now: repositoryTestTime.Add(2 * time.Hour)}, &repositoryTestIDs{})
	if err != nil {
		t.Fatalf("new collector: %v", err)
	}
	result, err := collector.GarbageCollectArtifacts(ctx, time.Hour)
	if err != nil {
		t.Fatalf("collect Artifact garbage: %v", err)
	}
	if result.ArtifactsDeleted != 1 {
		t.Fatalf("GC result = %#v", result)
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		_, err := store.GetArtifact(artifact.ID)
		return err
	}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("get collected Artifact: %v", err)
	}
}

func testDefinitionMetadataBatch(t *testing.T, repository *SQLRepository, workflow domain.WorkflowDefinition, blackboard domain.BlackboardDefinition) {
	t.Helper()
	ctx := context.Background()
	bindings := []domain.DefinitionBinding{workflow.Binding(), blackboard.Binding()}
	var metadata map[domain.DefinitionBinding]domain.DefinitionMetadata
	if err := repository.View(ctx, func(store application.ReadStore) error {
		var err error
		metadata, err = store.GetDefinitionMetadata(bindings)
		return err
	}); err != nil {
		t.Fatalf("get definition metadata: %v", err)
	}
	if len(metadata) != 2 || metadata[workflow.Binding()].Name != workflow.Name || metadata[blackboard.Binding()].Name != blackboard.Name {
		t.Fatalf("definition metadata = %#v", metadata)
	}

	if err := repository.View(ctx, func(store application.ReadStore) error {
		var err error
		metadata, err = store.GetDefinitionMetadata(nil)
		return err
	}); err != nil {
		t.Fatalf("get empty definition metadata: %v", err)
	}
	if metadata == nil || len(metadata) != 0 {
		t.Fatalf("empty definition metadata = %#v, want non-nil empty map", metadata)
	}
}

func testBlackboardLifecycleCandidates(t *testing.T, repository *SQLRepository, definition domain.BlackboardDefinition) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "lifecycle-agent"}, Role: "generalist"}

	createWorkItem := func(title string, acceptanceMode domain.WorkItemAcceptanceMode) domain.WorkItem {
		t.Helper()
		workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
			Definition: definition.Binding(), Identity: agent, Title: title, Goal: "Exercise lifecycle discovery", AcceptanceMode: acceptanceMode,
		})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		return workItem
	}

	empty := createWorkItem("Empty lifecycle board", domain.WorkItemAcceptanceNone)
	pending := createWorkItem("Pending lifecycle board", domain.WorkItemAcceptanceNone)
	if _, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: pending.ID, Identity: agent, Title: "Pending task", Executor: domain.ExecutorAgent,
	}); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	converged := createWorkItem("Converged lifecycle board", domain.WorkItemAcceptanceNone)
	task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: converged.ID, Identity: agent, Title: "Completed task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create completed task: %v", err)
	}
	claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim completed task: %v", err)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "done",
	}); err != nil {
		t.Fatalf("submit completed task: %v", err)
	}
	skipped := createWorkItem("Skipped lifecycle board", domain.WorkItemAcceptanceNone)
	skippedTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: skipped.ID, Identity: agent, Title: "Obsolete task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create skipped task: %v", err)
	}
	if _, err := service.SkipBlackboardTask(ctx, application.SkipBlackboardTaskCommand{
		TaskID: skippedTask.ID, Identity: agent, Reason: "no longer needed",
	}); err != nil {
		t.Fatalf("skip task: %v", err)
	}

	agentAcceptance := createWorkItem("Agent acceptance lifecycle board", domain.WorkItemAcceptanceAgent)
	if _, err := service.SubmitBlackboardCompletion(ctx, application.SubmitBlackboardCompletionCommand{
		WorkItemID: agentAcceptance.ID, Identity: agent, Result: "ready for agent acceptance",
	}); err != nil {
		t.Fatalf("submit agent completion: %v", err)
	}
	humanAcceptance := createWorkItem("Human acceptance lifecycle board", domain.WorkItemAcceptanceHuman)
	if _, err := service.SubmitBlackboardCompletion(ctx, application.SubmitBlackboardCompletionCommand{
		WorkItemID: humanAcceptance.ID, Identity: agent, Result: "ready for human acceptance",
	}); err != nil {
		t.Fatalf("submit human completion: %v", err)
	}

	var completionCandidates, acceptanceCandidates []domain.WorkItem
	if err := repository.View(ctx, func(store application.ReadStore) error {
		var err error
		completionCandidates, err = store.ListBlackboardsAwaitingCompletion(nil, 50)
		if err != nil {
			return err
		}
		acceptanceCandidates, err = store.ListBlackboardsAwaitingAgentAcceptance(nil, 50)
		return err
	}); err != nil {
		t.Fatalf("list lifecycle candidates: %v", err)
	}
	found := make(map[domain.WorkItemID]bool, len(completionCandidates)+len(acceptanceCandidates))
	for _, candidate := range append(completionCandidates, acceptanceCandidates...) {
		found[candidate.ID] = true
	}
	if !found[converged.ID] || !found[skipped.ID] || !found[agentAcceptance.ID] {
		t.Fatalf("lifecycle candidates = %+v/%+v, want completed %q, skipped %q, and agent acceptance %q", completionCandidates, acceptanceCandidates, converged.ID, skipped.ID, agentAcceptance.ID)
	}
	for _, excluded := range []domain.WorkItemID{empty.ID, pending.ID, humanAcceptance.ID} {
		if found[excluded] {
			t.Fatalf("lifecycle candidates unexpectedly include %q: %+v/%+v", excluded, completionCandidates, acceptanceCandidates)
		}
	}
	if err := repository.View(ctx, func(store application.ReadStore) error {
		limitedCompletion, err := store.ListBlackboardsAwaitingCompletion(nil, 1)
		if err != nil {
			return err
		}
		limitedAcceptance, err := store.ListBlackboardsAwaitingAgentAcceptance(nil, 1)
		if err != nil {
			return err
		}
		if len(limitedCompletion) != 1 || len(limitedAcceptance) != 1 {
			return fmt.Errorf("limited lifecycle groups = %d completion/%d acceptance, want 1/1", len(limitedCompletion), len(limitedAcceptance))
		}
		return nil
	}); err != nil {
		t.Fatalf("limit lifecycle candidates: %v", err)
	}
}

func testClaimLeaseColumns(t *testing.T, repository *SQLRepository, definition domain.BlackboardDefinition) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "lease-agent"}, Role: "generalist"}
	human := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "lease-human"}}
	workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{Definition: definition.Binding(), Identity: agent, Title: "Lease columns", Goal: "Persist claim ownership"})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	agentTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: agent, Title: "Agent", Executor: domain.ExecutorAgent})
	if err != nil {
		t.Fatalf("create agent task: %v", err)
	}
	humanTask, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: agent, Title: "Human", Executor: domain.ExecutorHuman})
	if err != nil {
		t.Fatalf("create human task: %v", err)
	}
	agentClaim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: agentTask.ID, Identity: agent, LeaseSeconds: 300})
	if err != nil {
		t.Fatalf("claim agent task: %v", err)
	}
	humanClaim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: humanTask.ID, Identity: human})
	if err != nil {
		t.Fatalf("claim human task: %v", err)
	}

	assertColumns := func(id domain.ClaimID, wantKind, wantID string, wantLease bool) {
		t.Helper()
		var kind, executorID string
		var heartbeat, until nullableScannedTime
		var seconds sql.NullInt64
		query := rebind(repository.dialect, "SELECT executor_kind, executor_id, last_heartbeat_at, lease_until, lease_seconds FROM claims WHERE id = ?")
		if err := repository.db.QueryRowContext(ctx, query, id).Scan(&kind, &executorID, &heartbeat, &until, &seconds); err != nil {
			t.Fatalf("query claim columns: %v", err)
		}
		if kind != wantKind || executorID != wantID {
			t.Fatalf("executor columns = %s/%s, want %s/%s", kind, executorID, wantKind, wantID)
		}
		if heartbeat.Valid != wantLease || until.Valid != wantLease || seconds.Valid != wantLease {
			t.Fatalf("lease nullability = %v/%v/%v, want %v", heartbeat.Valid, until.Valid, seconds.Valid, wantLease)
		}
	}
	assertColumns(agentClaim.ID, "agent", "lease-agent", true)
	assertColumns(humanClaim.ID, "human", "lease-human", false)

	listReapable := func(now time.Time) []domain.TaskID {
		t.Helper()
		var result []domain.TaskID
		if err := repository.View(ctx, func(store application.ReadStore) error {
			var err error
			result, err = store.ListReapableAgentClaimTasks(now)
			return err
		}); err != nil {
			t.Fatalf("list reapable claims: %v", err)
		}
		return result
	}
	if got := listReapable(agentClaim.LeaseUntil.Add(-time.Nanosecond)); slices.Contains(got, agentTask.ID) {
		t.Fatalf("reapable before deadline = %v, unexpectedly contains %q", got, agentTask.ID)
	}
	if got := listReapable(agentClaim.LeaseUntil); !slices.Contains(got, agentTask.ID) || slices.Contains(got, humanTask.ID) {
		t.Fatalf("reapable at deadline = %v, want agent task %q without human task %q", got, agentTask.ID, humanTask.ID)
	}
}

func forEachSQLRepository(
	t *testing.T,
	test func(*testing.T, *SQLRepository, func(*testing.T) *SQLRepository),
) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "kairos.db")
		open := func(t *testing.T) *SQLRepository {
			t.Helper()
			repository, err := OpenSQLite(context.Background(), path)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			return repository
		}
		test(t, open(t), open)
	})

	dsn := os.Getenv("KAIROS_TEST_POSTGRES_DSN")
	if dsn == "" {
		return
	}
	t.Run("postgres", func(t *testing.T) {
		open := func(t *testing.T) *SQLRepository {
			t.Helper()
			repository, err := OpenPostgres(context.Background(), dsn)
			if err != nil {
				t.Fatalf("open postgres: %v", err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			return repository
		}
		repository := open(t)
		if _, err := repository.db.ExecContext(context.Background(), `
			TRUNCATE TABLE
				idempotency_records,
				work_item_events,
				claims,
				task_relations,
				tasks,
				workflow_activations,
				work_items,
				definitions
			CASCADE`); err != nil {
			t.Fatalf("reset postgres: %v", err)
		}
		test(t, repository, open)
	})
}

func TestSQLiteRestrictsExistingFilesystemPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kairos.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("create sqlite file: %v", err)
	}
	repository, err := OpenSQLite(context.Background(), path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer repository.Close()

	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(candidate)
		if err != nil {
			t.Fatalf("stat %q: %v", candidate, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("permissions for %q = %04o, want 0600", candidate, got)
		}
	}
}

func testWorkflowRoundTrip(
	t *testing.T,
	repository *SQLRepository,
	definition domain.WorkflowDefinition,
) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	agent := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "workflow-agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Persist workflow", Goal: "Verify SQL persistence",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	start := repositoryTaskByDefinition(t, repository, workItem.ID, "implement")
	claim, err := service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim implement: %v", err)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &application.WorkflowTransitionCommand{
			ChoiceGroupID: "exit:implement", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs"},
		},
	}); err != nil {
		t.Fatalf("submit implement: %v", err)
	}

	docs := repositoryTaskByDefinition(t, repository, workItem.ID, "docs")
	if docs.Status != domain.TaskStatusSkipped || len(docs.TransitionDecisions) != 1 {
		t.Fatalf("persisted skipped docs: %#v", docs)
	}
	testTask := repositoryTaskByDefinition(t, repository, workItem.ID, "test")
	if testTask.Status != domain.TaskStatusPending {
		t.Fatalf("test task status: got %s", testTask.Status)
	}
	contextView, err := service.GetTaskExecutionContext(ctx, application.GetTaskExecutionContextQuery{
		TaskID: testTask.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get persisted execution context: %v", err)
	}
	if len(contextView.Workflow.UpstreamTasks) != 2 {
		t.Fatalf("upstream tasks: %#v", contextView.Workflow.UpstreamTasks)
	}

	claim, err = service.ClaimTask(ctx, application.ClaimTaskCommand{TaskID: testTask.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim test: %v", err)
	}
	if _, err := service.SubmitTask(ctx, application.SubmitTaskCommand{
		TaskID: testTask.ID, ClaimID: claim.ID, Identity: agent, Result: "Tests passed",
	}); err != nil {
		t.Fatalf("submit test: %v", err)
	}

	err = repository.View(ctx, func(store application.ReadStore) error {
		persisted, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		if persisted.Status != domain.WorkItemStatusCompleted || persisted.CompletedAt == nil {
			return fmt.Errorf("persisted work item is %#v", persisted)
		}
		sequence, err := store.LastWorkItemEventSequence(workItem.ID)
		if err != nil {
			return err
		}
		if sequence < 10 {
			return fmt.Errorf("event sequence %d is unexpectedly short", sequence)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify completed workflow: %v", err)
	}
}

func testTransactionRollback(
	t *testing.T,
	repository *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	workItem := domain.WorkItem{
		ID: "rolled-back-work", Definition: definition.Binding(), Status: domain.WorkItemStatusOpen,
		Title: "Rollback", Goal: "Must not persist", CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime,
	}
	sentinel := errors.New("rollback sentinel")
	err := repository.Update(ctx, func(store application.WriteStore) error {
		if err := store.CreateWorkItem(workItem); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("rollback error: got %v", err)
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		_, err := store.GetWorkItem(workItem.ID)
		return err
	})
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("rolled back work item: got %v", err)
	}
}

func testConcurrentClaim(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	creator := application.Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "creator"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: creator, Title: "Concurrent claim", Goal: "Choose one owner",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	task, err := service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   creator, Title: "Claim once", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	identities := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-a"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-b"}, Role: "generalist"},
	}
	services := []*application.Service{service, repositoryTestService(t, peer)}
	start := make(chan struct{})
	errorsByAgent := make([]error, len(identities))
	var wait sync.WaitGroup
	for index, identity := range identities {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByAgent[index] = services[index].ClaimTask(ctx, application.ClaimTaskCommand{
				TaskID: task.ID, Identity: identity,
			})
		}()
	}
	close(start)
	wait.Wait()

	succeeded := 0
	conflicted := 0
	for _, err := range errorsByAgent {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, application.ErrConflict):
			conflicted++
		default:
			t.Fatalf("claim result: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("claim results: success=%d conflict=%d", succeeded, conflicted)
	}
}

func testIndependentWorkItemsDoNotBlock(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	service := repositoryTestService(t, repository)
	identity := application.Identity{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "parallel-work-creator"},
		Role:  "generalist",
	}
	first, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identity, Title: "First parallel work", Goal: "Hold one row lock",
	})
	if err != nil {
		t.Fatalf("create first work item: %v", err)
	}
	second, err := service.CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identity, Title: "Second parallel work", Goal: "Acquire another row lock",
	})
	if err != nil {
		t.Fatalf("create second work item: %v", err)
	}

	firstLocked := make(chan error, 1)
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- repository.Update(ctx, func(store application.WriteStore) error {
			_, err := store.GetWorkItem(first.ID)
			firstLocked <- err
			if err != nil {
				return err
			}
			<-releaseFirst
			return nil
		})
	}()
	if err := <-firstLocked; err != nil {
		close(releaseFirst)
		<-firstDone
		t.Fatalf("lock first work item: %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- peer.Update(ctx, func(store application.WriteStore) error {
			_, err := store.GetWorkItem(second.ID)
			return err
		})
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			close(releaseFirst)
			<-firstDone
			t.Fatalf("lock independent work item: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(releaseFirst)
		<-firstDone
		t.Fatal("an independent work item was blocked by another work item's transaction")
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("finish first work item transaction: %v", err)
	}
}

func testConcurrentBlackboardPlanning(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	planners := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner-a"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner-b"}, Role: "generalist"},
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: planners[0],
		Title: "Concurrent planning", Goal: "Create one coherent first task",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	start := make(chan struct{})
	errorsByPlanner := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByPlanner[index] = service.CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
				WorkItemID: workItem.ID,
				Identity:   planners[index], Title: fmt.Sprintf("Plan part %d", index+1), Executor: domain.ExecutorAgent,
			})
		}()
	}
	close(start)
	wait.Wait()

	succeeded := 0
	for _, err := range errorsByPlanner {
		switch {
		case err == nil:
			succeeded++
		default:
			t.Fatalf("planning result: %v", err)
		}
	}
	if succeeded != 2 {
		t.Fatalf("planning results: success=%d", succeeded)
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		persisted, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		if persisted.Version != 2 {
			return fmt.Errorf("work item version is %d, want 2", persisted.Version)
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return err
		}
		if len(tasks) != 2 {
			return fmt.Errorf("task count is %d, want 2", len(tasks))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify concurrent planning: %v", err)
	}
}

func testConcurrentIdempotentPlanning(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	planner := application.Identity{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "idempotent-planner"}, Role: "generalist",
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: planner,
		Title: "Idempotent planning", Goal: "Retry one task creation safely",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	command := application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   planner, OperationID: "add-login-task",
		Title: "Implement login", Executor: domain.ExecutorAgent,
	}

	start := make(chan struct{})
	results := make([]domain.Task, len(services))
	errorsByRequest := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index], errorsByRequest[index] = service.CreateBlackboardTask(ctx, command)
		}()
	}
	close(start)
	wait.Wait()
	for _, err := range errorsByRequest {
		if err != nil {
			t.Fatalf("idempotent planning result: %v", err)
		}
	}
	if results[0].ID != results[1].ID {
		t.Fatalf("idempotent task ids differ: %q and %q", results[0].ID, results[1].ID)
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return err
		}
		if len(tasks) != 1 {
			return fmt.Errorf("task count is %d, want 1", len(tasks))
		}
		persisted, err := store.GetWorkItem(workItem.ID)
		if err != nil {
			return err
		}
		if persisted.Version != 1 {
			return fmt.Errorf("work item version is %d, want 1", persisted.Version)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify idempotent planning: %v", err)
	}
}

func testHierarchyClosureRace(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	identities := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "hierarchy-owner"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "hierarchy-planner"}, Role: "generalist"},
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identities[0],
		Title: "Hierarchy race", Goal: "Resolve append and closure atomically",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	root, err := services[0].CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identities[0], Title: "Deliver feature", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create root task: %v", err)
	}
	rootClaim, err := services[0].ClaimTask(ctx, application.ClaimTaskCommand{TaskID: root.ID, Identity: identities[0]})
	if err != nil {
		t.Fatalf("claim root task: %v", err)
	}
	decomposition, err := services[0].DecomposeBlackboardTask(ctx, application.DecomposeBlackboardTaskCommand{
		TaskID: root.ID, ClaimID: rootClaim.ID, Identity: identities[0],
		Children: []application.BlackboardTaskSpec{{Title: "Implement feature", Executor: domain.ExecutorAgent}},
	})
	if err != nil {
		t.Fatalf("decompose root task: %v", err)
	}
	child := decomposition.Children[0]
	childClaim, err := services[0].ClaimTask(ctx, application.ClaimTaskCommand{TaskID: child.ID, Identity: identities[0]})
	if err != nil {
		t.Fatalf("claim child task: %v", err)
	}

	start := make(chan struct{})
	submitDone := make(chan error, 1)
	addDone := make(chan struct {
		task domain.Task
		err  error
	}, 1)
	go func() {
		<-start
		_, err := services[0].SubmitTask(ctx, application.SubmitTaskCommand{
			TaskID: child.ID, ClaimID: childClaim.ID, Identity: identities[0], Result: "Feature implemented",
		})
		submitDone <- err
	}()
	go func() {
		<-start
		task, err := services[1].AddBlackboardChildTask(ctx, application.AddBlackboardChildTaskCommand{
			ParentTaskID: root.ID, Identity: identities[1],
			Task: application.BlackboardTaskSpec{Title: "Late integration check", Executor: domain.ExecutorAgent},
		})
		addDone <- struct {
			task domain.Task
			err  error
		}{task: task, err: err}
	}()
	close(start)
	if err := <-submitDone; err != nil {
		t.Fatalf("submit child: %v", err)
	}
	addResult := <-addDone
	if addResult.err != nil && !errors.Is(addResult.err, application.ErrConflict) {
		t.Fatalf("append child: %v", addResult.err)
	}

	err = repository.View(ctx, func(store application.ReadStore) error {
		parent, err := store.GetTask(root.ID)
		if err != nil {
			return err
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return err
		}
		if addResult.err == nil {
			if parent.Status != domain.TaskStatusWaitingChildren {
				return fmt.Errorf("parent status is %s after successful append", parent.Status)
			}
			if addResult.task.ParentTaskID == nil || *addResult.task.ParentTaskID != root.ID {
				return fmt.Errorf("appended task has parent %v", addResult.task.ParentTaskID)
			}
		} else if parent.Status != domain.TaskStatusCompleted {
			return fmt.Errorf("parent status is %s after closure won", parent.Status)
		}
		return domain.ValidateBlackboardTaskHierarchy(workItem.ID, tasks)
	})
	if err != nil {
		t.Fatalf("verify hierarchy closure race: %v", err)
	}
}

func testConcurrentChildAppends(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	identities := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "child-planner-a"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "child-planner-b"}, Role: "generalist"},
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identities[0],
		Title: "Concurrent child planning", Goal: "Keep both useful child tasks",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	root, err := services[0].CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identities[0], Title: "Implement login", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create root task: %v", err)
	}
	claim, err := services[0].ClaimTask(ctx, application.ClaimTaskCommand{TaskID: root.ID, Identity: identities[0]})
	if err != nil {
		t.Fatalf("claim root task: %v", err)
	}
	if _, err := services[0].DecomposeBlackboardTask(ctx, application.DecomposeBlackboardTaskCommand{
		TaskID: root.ID, ClaimID: claim.ID, Identity: identities[0],
		Children: []application.BlackboardTaskSpec{{Title: "Initial implementation", Executor: domain.ExecutorAgent}},
	}); err != nil {
		t.Fatalf("decompose root task: %v", err)
	}

	start := make(chan struct{})
	errorsByPlanner := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByPlanner[index] = service.AddBlackboardChildTask(ctx, application.AddBlackboardChildTaskCommand{
				ParentTaskID: root.ID, Identity: identities[index],
				Task: application.BlackboardTaskSpec{
					Title: fmt.Sprintf("Concurrent child %d", index+1), Executor: domain.ExecutorAgent,
				},
			})
		}()
	}
	close(start)
	wait.Wait()
	for _, err := range errorsByPlanner {
		if err != nil {
			t.Fatalf("concurrent child append: %v", err)
		}
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		parent, err := store.GetTask(root.ID)
		if err != nil {
			return err
		}
		if parent.Status != domain.TaskStatusWaitingChildren {
			return fmt.Errorf("parent status is %s", parent.Status)
		}
		tasks, err := store.ListTasks(workItem.ID)
		if err != nil {
			return err
		}
		children := 0
		for _, task := range tasks {
			if task.ParentTaskID != nil && *task.ParentTaskID == root.ID {
				children++
			}
		}
		if children != 3 {
			return fmt.Errorf("child count is %d, want 3", children)
		}
		return domain.ValidateBlackboardTaskHierarchy(workItem.ID, tasks)
	})
	if err != nil {
		t.Fatalf("verify concurrent child appends: %v", err)
	}
}

func testConcurrentReciprocalRelations(
	t *testing.T,
	repository *SQLRepository,
	peer *SQLRepository,
	definition domain.BlackboardDefinition,
) {
	t.Helper()
	ctx := context.Background()
	services := []*application.Service{
		repositoryTestService(t, repository),
		repositoryTestService(t, peer),
	}
	identities := []application.Identity{
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "relation-planner-a"}, Role: "generalist"},
		{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "relation-planner-b"}, Role: "generalist"},
	}
	workItem, err := services[0].CreateWorkItem(ctx, application.CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: identities[0],
		Title: "Concurrent relations", Goal: "Keep the suggested graph acyclic",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	first, err := services[0].CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identities[0], Title: "First task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	second, err := services[0].CreateBlackboardTask(ctx, application.CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identities[0], Title: "Second task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}

	commands := []application.AddBlackboardRelationCommand{
		{WorkItemID: workItem.ID, FromTaskID: first.ID, ToTaskID: second.ID, Identity: identities[0]},
		{WorkItemID: workItem.ID, FromTaskID: second.ID, ToTaskID: first.ID, Identity: identities[1]},
	}
	start := make(chan struct{})
	errorsByPlanner := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByPlanner[index] = service.AddBlackboardRelation(ctx, commands[index])
		}()
	}
	close(start)
	wait.Wait()
	succeeded := 0
	rejected := 0
	for _, err := range errorsByPlanner {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrInvalidModel):
			rejected++
		default:
			t.Fatalf("concurrent relation result: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("relation results: success=%d rejected=%d", succeeded, rejected)
	}
	err = repository.View(ctx, func(store application.ReadStore) error {
		relations, err := store.ListTaskRelations(workItem.ID)
		if err != nil {
			return err
		}
		if len(relations) != 1 {
			return fmt.Errorf("relation count is %d, want 1", len(relations))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify reciprocal relations: %v", err)
	}
}

func repositoryTaskByDefinition(
	t *testing.T,
	repository *SQLRepository,
	workItemID domain.WorkItemID,
	definitionID domain.WorkflowTaskID,
) domain.Task {
	t.Helper()
	var found domain.Task
	err := repository.View(context.Background(), func(store application.ReadStore) error {
		tasks, err := store.ListTasks(workItemID)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if task.WorkflowTaskID != nil && *task.WorkflowTaskID == definitionID {
				found = task
				return nil
			}
		}
		return application.ErrNotFound
	})
	if err != nil {
		t.Fatalf("find workflow task %q: %v", definitionID, err)
	}
	return found
}

type repositoryTestClock struct{}

func (repositoryTestClock) Now() time.Time { return repositoryTestTime }

type repositoryTestClockAt struct{ now time.Time }

func (c repositoryTestClockAt) Now() time.Time { return c.now }

type repositoryTestIDs struct{}

func (g *repositoryTestIDs) NewID() string {
	return fmt.Sprintf("sql-generated-%d", repositoryTestIDSequence.Add(1))
}

func repositoryTestService(t *testing.T, repository application.Repository) *application.Service {
	t.Helper()
	service, err := application.NewService(repository, repositoryTestClock{}, &repositoryTestIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func repositoryWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "sql-workflow", Version: 1, Name: "SQL workflow",
			CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "docs", Title: "Documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "test", Title: "Test", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs"},
				{ID: "docs-test", FromTaskID: "docs", ToTaskID: "test"},
			},
		},
	}
}

func repositoryBlackboardDefinition() domain.BlackboardDefinition {
	return domain.BlackboardDefinition{DefinitionMetadata: domain.DefinitionMetadata{
		ID: "sql-blackboard", Version: 1, Name: "SQL blackboard",
		CreatedAt: repositoryTestTime, UpdatedAt: repositoryTestTime,
	}}
}
