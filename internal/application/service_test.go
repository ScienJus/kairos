package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/ScienJus/kairos/internal/artifactstore"
	"github.com/ScienJus/kairos/internal/domain"
)

func TestGetWorkflowTaskExecutionContext(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := consecutiveOptionalWorkflowDefinition()
	definition.AgentInstructions = "Prefer the smallest safe delivery path."
	definition.SuggestedTags = []string{"module:*", "risk:*"}
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Execution context", Goal: "Inspect workflow choices",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	task := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	contextView, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{
		TaskID: task.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get execution context: %v", err)
	}
	if contextView.Workflow == nil || contextView.Blackboard != nil {
		t.Fatalf("mode context: %#v", contextView)
	}
	if contextView.Definition.AgentInstructions != definition.AgentInstructions || len(contextView.Claims) != 1 {
		t.Fatalf("definition or claims: %#v", contextView)
	}
	if len(contextView.Workflow.ChoiceGroups) != 1 {
		t.Fatalf("choice groups: %#v", contextView.Workflow.ChoiceGroups)
	}
	choice := contextView.Workflow.ChoiceGroups[0]
	if choice.ID != "exit:implement" || len(choice.Targets) != 1 || choice.Targets[0].ID != "docs" {
		t.Fatalf("exit choice: %#v", choice)
	}
	if len(choice.Relations) != 1 || choice.Relations[0].RelationID != "implement-docs" || choice.Relations[0].Label != "Documentation needed" || choice.Relations[0].AgentGuidance != "Keep documentation when the change affects users." {
		t.Fatalf("relation guidance: %#v", choice.Relations)
	}
	if got := workflowDefinitionTaskIDs(choice.SkippableOptionalTasks); !slices.Equal(got, []domain.WorkflowTaskID{"docs", "examples"}) {
		t.Fatalf("skip candidates: got %v", got)
	}

	other := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "other"}, Role: "backend"}
	_, err = service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{TaskID: task.ID, Identity: other})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("context for non-owner: got %v", err)
	}

	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID: "exit:implement", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs", "examples"},
		},
	}); err != nil {
		t.Fatalf("submit task: %v", err)
	}
	detail, err := service.GetTaskDetail(context.Background(), GetTaskDetailQuery{
		TaskID: task.ID, Identity: Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}},
	})
	if err != nil {
		t.Fatalf("human get completed agent task detail: %v", err)
	}
	if detail.Responsibility.Kind != "executed_by" || detail.Responsibility.Actor == nil || detail.Responsibility.Actor.ID != agent.Actor.ID {
		t.Fatalf("completed task responsibility: %#v", detail.Responsibility)
	}
	if detail.Capabilities.CanClaim || detail.Capabilities.CanSubmit || detail.Capabilities.CanReview {
		t.Fatalf("completed task capabilities: %#v", detail.Capabilities)
	}
	if detail.History.Reviews == nil || detail.History.Failures == nil || detail.Artifacts == nil {
		t.Fatalf("empty detail collections must be normalized: %#v", detail)
	}
	integration := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["integration"]
	if _, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: integration.ID, Identity: agent}); err != nil {
		t.Fatalf("claim integration: %v", err)
	}
	integrationContext, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{
		TaskID: integration.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get integration context: %v", err)
	}
	if got := runtimeWorkflowTaskIDs(integrationContext.Workflow.UpstreamTasks); !slices.Equal(got, []domain.WorkflowTaskID{"examples", "docs", "implement"}) {
		t.Fatalf("integration upstream tasks: got %v", got)
	}
}

func TestWorkflowArtifactsGuideAndGateSubmission(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := workflowDefinition()
	definition.Graph.Relations = nil
	definition.Graph.Tasks = definition.Graph.Tasks[:1]
	definition.Graph.Tasks[0].Artifacts = []domain.ArtifactDefinition{
		{Name: "commit", Description: "Provide the immutable Git commit."},
		{Name: "branch", Description: "Provide the remote integration branch."},
	}
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Artifacts", Goal: "Ship a traceable change",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	commit, err := service.CreateArtifact(context.Background(), CreateArtifactCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Name: "commit", URI: "https://example.test/repo/commit/abc",
	})
	if err != nil {
		t.Fatalf("create commit artifact: %v", err)
	}
	contextView, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("get task context: %v", err)
	}
	if len(contextView.ExpectedArtifacts) != 2 || len(contextView.Artifacts) != 1 || contextView.Artifacts[0].SubmissionID != nil {
		t.Fatalf("artifact context: %#v", contextView)
	}
	_, err = service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "done", ArtifactIDs: []domain.ArtifactID{commit.ID},
	})
	if !errors.Is(err, ErrInvalidCommand) || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("submit without required branch: %v", err)
	}
	branch, err := service.CreateArtifact(context.Background(), CreateArtifactCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Name: "branch", URI: "https://example.test/repo/tree/feature",
	})
	if err != nil {
		t.Fatalf("create branch artifact: %v", err)
	}
	submission, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "done", ArtifactIDs: []domain.ArtifactID{commit.ID, branch.ID},
	})
	if err != nil {
		t.Fatalf("submit artifacts: %v", err)
	}
	artifacts, err := service.ListArtifacts(context.Background(), workItem.ID, agent, PageRequest[ArtifactCursor]{Limit: 50})
	if err != nil || len(artifacts.Items) != 2 {
		t.Fatalf("list committed artifacts: %v %#v", err, artifacts)
	}
	for _, artifact := range artifacts.Items {
		if artifact.SubmissionID == nil || *artifact.SubmissionID != submission.ID {
			t.Fatalf("artifact was not bound to submission: %#v", artifact)
		}
	}
	detail, err := service.GetTaskDetail(context.Background(), GetTaskDetailQuery{
		TaskID: task.ID, Identity: Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}},
	})
	if err != nil {
		t.Fatalf("get task detail with artifacts: %v", err)
	}
	if len(detail.Artifacts) != 2 || detail.Artifacts[0].TaskID != task.ID || detail.Artifacts[0].SubmissionID == nil {
		t.Fatalf("task detail Artifacts = %#v", detail.Artifacts)
	}
}

func TestManagedArtifactUploadRetryAfterClaimEnds(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	local, err := artifactstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local Artifact Store: %v", err)
	}
	if err := service.ConfigureArtifactStore(local); err != nil {
		t.Fatalf("configure Artifact Store: %v", err)
	}
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "upload-agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Managed upload retry", Goal: "Retain an upload response",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Upload", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	command := UploadArtifactCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, OperationID: "upload-report", Name: "report",
	}
	withoutOperationID := command
	withoutOperationID.OperationID = ""
	if _, err := service.UploadArtifact(context.Background(), withoutOperationID, strings.NewReader("report content")); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("upload without operation id: %v", err)
	}
	if _, err := service.UploadArtifact(context.Background(), command, iotest.ErrReader(errors.New("upload interrupted"))); err == nil {
		t.Fatal("expected interrupted upload to fail")
	}
	pending := repository.idempotency[idempotencyTestKey(agent.Actor, command.OperationID)]
	if pending.Operation != artifactUploadOperation || pending.Status != IdempotencyPending || len(repository.artifacts) != 0 || len(repository.blobs) != 0 {
		t.Fatalf("pending upload state = %#v, Artifacts = %#v, Blobs = %#v", pending, repository.artifacts, repository.blobs)
	}
	created, err := service.UploadArtifact(context.Background(), command, strings.NewReader("report content"))
	if err != nil {
		t.Fatalf("resume pending Artifact upload: %v", err)
	}
	completed := repository.idempotency[idempotencyTestKey(agent.Actor, command.OperationID)]
	if completed.Operation != artifactUploadOperation || completed.Status != IdempotencyCompleted || len(repository.artifacts) != 1 || len(repository.blobs) != 1 {
		t.Fatalf("completed upload state = %#v, Artifacts = %#v, Blobs = %#v", completed, repository.artifacts, repository.blobs)
	}
	if err := service.ReleaseClaim(context.Background(), ReleaseClaimCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, OperationID: "release-upload-claim",
	}); err != nil {
		t.Fatalf("release Claim: %v", err)
	}

	retried, err := service.UploadArtifact(context.Background(), command, strings.NewReader("report content"))
	if err != nil {
		t.Fatalf("retry upload after Claim ended: %v", err)
	}
	if retried.ID != created.ID || retried.URI != created.URI {
		t.Fatalf("retried Artifact = %#v, want %#v", retried, created)
	}
	if _, err := service.UploadArtifact(context.Background(), command, strings.NewReader("changed content")); !errors.Is(err, ErrConflict) {
		t.Fatalf("retry changed upload: %v", err)
	}
	delete(repository.artifacts, created.ID)
	if _, err := service.UploadArtifact(context.Background(), command, strings.NewReader("report content")); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "no longer retained") {
		t.Fatalf("retry collected upload: %v", err)
	}
}

func TestManagedArtifactUploadSerializesConcurrentRetry(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	local, err := artifactstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local Artifact Store: %v", err)
	}
	if err := service.ConfigureArtifactStore(local); err != nil {
		t.Fatalf("configure Artifact Store: %v", err)
	}
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "concurrent-upload-agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Concurrent managed upload", Goal: "Keep content and metadata consistent",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Upload", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	command := UploadArtifactCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, OperationID: "same-upload", Name: "report",
	}
	type uploadResult struct {
		artifact domain.Artifact
		content  string
		err      error
	}
	start := make(chan struct{})
	results := make(chan uploadResult, 2)
	for _, content := range []string{"first content", "second content"} {
		go func() {
			<-start
			artifact, err := service.UploadArtifact(context.Background(), command, strings.NewReader(content))
			results <- uploadResult{artifact: artifact, content: content, err: err}
		}()
	}
	close(start)

	var succeeded *uploadResult
	conflicts := 0
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			copy := result
			succeeded = &copy
		case errors.Is(result.err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent upload result: %v", result.err)
		}
	}
	if succeeded == nil || conflicts != 1 {
		t.Fatalf("concurrent upload: succeeded=%#v conflicts=%d", succeeded, conflicts)
	}
	reader, err := local.Open(context.Background(), succeeded.artifact.URI)
	if err != nil {
		t.Fatalf("open uploaded content: %v", err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != succeeded.content {
		t.Fatalf("stored content = %q, err=%v; want %q", content, err, succeeded.content)
	}
}

func TestManagedArtifactUploadRewritesFileAfterPendingGCDeleteFailure(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	local, err := artifactstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local Artifact Store: %v", err)
	}
	if err := service.ConfigureArtifactStore(local); err != nil {
		t.Fatalf("configure Artifact Store: %v", err)
	}
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "gc-retry-agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "GC retry", Goal: "Recover a deleted pending file",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Upload", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	command := UploadArtifactCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: agent, OperationID: "pending-upload", Name: "report",
	}
	uploadKey, err := artifactUploadStorageKey(agent.Actor, command.OperationID)
	if err != nil {
		t.Fatalf("prepare upload key: %v", err)
	}
	uploadURI, err := local.UploadURI(uploadKey)
	if err != nil {
		t.Fatalf("prepare upload URI: %v", err)
	}
	blob, err := local.Put(context.Background(), uploadURI, strings.NewReader("recoverable content"))
	if err != nil {
		t.Fatalf("write pending content: %v", err)
	}
	requestHash, err := idempotencyRequestHash(artifactUploadReservation{TaskID: task.ID, ClaimID: claim.ID, Name: command.Name})
	if err != nil {
		t.Fatalf("hash reservation: %v", err)
	}
	repository.idempotency[idempotencyTestKey(agent.Actor, command.OperationID)] = IdempotencyRecord{
		Actor: agent.Actor, OperationID: command.OperationID, Operation: artifactUploadOperation,
		Status: IdempotencyPending, RequestHash: requestHash,
		Response:  mustArtifactUploadState(artifactUploadState{BlobURI: blob.URI, Digest: blob.Digest, Size: blob.Size}),
		CreatedAt: applicationTestTime.Add(-2 * time.Hour),
	}
	deleteFailure := errors.New("delete pending record")
	repository.deleteIdempotencyError = deleteFailure
	if _, err := service.GarbageCollectArtifacts(context.Background(), time.Hour); !errors.Is(err, deleteFailure) {
		t.Fatalf("garbage collection error = %v, want %v", err, deleteFailure)
	}
	if _, err := local.Open(context.Background(), blob.URI); err == nil {
		t.Fatal("pending file was not deleted")
	}
	repository.deleteIdempotencyError = nil

	created, err := service.UploadArtifact(context.Background(), command, strings.NewReader("recoverable content"))
	if err != nil {
		t.Fatalf("retry pending upload: %v", err)
	}
	reader, err := local.Open(context.Background(), created.URI)
	if err != nil {
		t.Fatalf("open recovered content: %v", err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "recoverable content" {
		t.Fatalf("recovered content = %q, err=%v", content, err)
	}
}

func TestManagedArtifactUploadStorageKeyIsUnambiguous(t *testing.T) {
	first, err := artifactUploadStorageKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "a:b"}, "c")
	if err != nil {
		t.Fatalf("first storage key: %v", err)
	}
	second, err := artifactUploadStorageKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "a"}, "b:c")
	if err != nil {
		t.Fatalf("second storage key: %v", err)
	}
	if first == second {
		t.Fatalf("storage key collision: %q", first)
	}
}

func TestArtifactDownloadDoesNotBlockManagedUpload(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	service := newTestService(t, repository)
	local, err := artifactstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local Artifact Store: %v", err)
	}
	if err := service.ConfigureArtifactStore(local); err != nil {
		t.Fatalf("configure Artifact Store: %v", err)
	}
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "download-agent"}, Role: "generalist"}
	claimID := domain.ClaimID("claim")
	task := domain.Task{ID: "task", WorkItemID: "work-item", Status: domain.TaskStatusWorking, ActiveClaimID: &claimID}
	claim := domain.Claim{ID: claimID, TaskID: task.ID, Executor: identity.Actor, ClaimedAt: applicationTestTime}
	repository.workItems[task.WorkItemID] = domain.WorkItem{ID: task.WorkItemID, Status: domain.WorkItemStatusOpen}
	repository.tasks[task.ID] = task
	repository.claims[claim.ID] = claim
	uploadURI, err := local.UploadURI("downloaded-artifact")
	if err != nil {
		t.Fatalf("prepare downloaded Artifact URI: %v", err)
	}
	if _, err := local.Put(context.Background(), uploadURI, strings.NewReader("download content")); err != nil {
		t.Fatalf("write downloaded Artifact: %v", err)
	}
	artifact := domain.Artifact{
		ID: "downloaded-artifact", WorkItemID: task.WorkItemID, TaskID: task.ID, ClaimID: claim.ID,
		Name: "download", URI: uploadURI, CreatedAt: applicationTestTime,
	}
	repository.artifacts[artifact.ID] = artifact
	_, reader, err := service.OpenArtifact(context.Background(), artifact.ID, identity)
	if err != nil {
		t.Fatalf("open Artifact download: %v", err)
	}
	defer reader.Close()

	uploadDone := make(chan error, 1)
	go func() {
		_, err := service.UploadArtifact(context.Background(), UploadArtifactCommand{
			TaskID: task.ID, ClaimID: claim.ID, Identity: identity, OperationID: "parallel-upload", Name: "parallel",
		}, strings.NewReader("parallel content"))
		uploadDone <- err
	}()
	select {
	case err := <-uploadDone:
		if err != nil {
			t.Fatalf("upload while download is open: %v", err)
		}
	case <-time.After(2 * time.Second):
		reader.Close()
		<-uploadDone
		t.Fatal("open Artifact download blocked managed upload")
	}
}

func TestSubmittedArtifactIsVisibleToOtherBlackboardTask(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Shared Artifacts", Goal: "Reuse a durable deliverable",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	producer, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent,
		Title: "Produce", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create producer: %v", err)
	}
	consumer, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent,
		Title: "Consume", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	producerClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: producer.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim producer: %v", err)
	}
	artifact, err := service.CreateArtifact(context.Background(), CreateArtifactCommand{
		TaskID: producer.ID, ClaimID: producerClaim.ID, Identity: agent,
		Name: "research", URI: "https://example.test/research",
	})
	if err != nil {
		t.Fatalf("create Artifact: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: producer.ID, ClaimID: producerClaim.ID, Identity: agent,
		Result: "Research complete", ArtifactIDs: []domain.ArtifactID{artifact.ID},
	}); err != nil {
		t.Fatalf("submit producer: %v", err)
	}
	if _, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: consumer.ID, Identity: agent}); err != nil {
		t.Fatalf("claim consumer: %v", err)
	}
	contextView, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{TaskID: consumer.ID, Identity: agent})
	if err != nil {
		t.Fatalf("get consumer context: %v", err)
	}
	if len(contextView.Artifacts) != 1 || contextView.Artifacts[0].ID != artifact.ID {
		t.Fatalf("consumer Artifacts = %#v", contextView.Artifacts)
	}
}

func TestArtifactGarbageCollectionRemovesOnlyAbandonedContent(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	service := newTestService(t, repository)
	local, err := artifactstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local Artifact Store: %v", err)
	}
	if err := service.ConfigureArtifactStore(local); err != nil {
		t.Fatalf("configure Artifact Store: %v", err)
	}
	put := func(content string) domain.ArtifactBlob {
		t.Helper()
		uploadURI, err := local.UploadURI("gc:" + content)
		if err != nil {
			t.Fatalf("prepare %q: %v", content, err)
		}
		blob, err := local.Put(context.Background(), uploadURI, strings.NewReader(content))
		if err != nil {
			t.Fatalf("put %q: %v", content, err)
		}
		blob.CreatedAt = applicationTestTime.Add(-2 * time.Hour)
		repository.blobs[blob.URI] = blob
		return blob
	}
	shared := put("shared")
	orphan := put("orphan")
	active := put("active")
	young := put("young")
	endedAt := applicationTestTime.Add(-90 * time.Minute)
	repository.claims["ended"] = domain.Claim{ID: "ended", TaskID: "task", EndedAt: &endedAt}
	repository.claims["active"] = domain.Claim{ID: "active", TaskID: "task"}
	submissionID := domain.SubmissionID("submission")
	old := applicationTestTime.Add(-2 * time.Hour)
	recent := applicationTestTime.Add(-30 * time.Minute)
	repository.artifacts["abandoned-shared"] = domain.Artifact{ID: "abandoned-shared", ClaimID: "ended", URI: shared.URI, CreatedAt: old}
	repository.artifacts["committed-shared"] = domain.Artifact{ID: "committed-shared", ClaimID: "ended", SubmissionID: &submissionID, URI: shared.URI, CreatedAt: old}
	repository.artifacts["abandoned-orphan"] = domain.Artifact{ID: "abandoned-orphan", ClaimID: "ended", URI: orphan.URI, CreatedAt: old}
	repository.artifacts["active-staged"] = domain.Artifact{ID: "active-staged", ClaimID: "active", URI: active.URI, CreatedAt: old}
	repository.artifacts["young-abandoned"] = domain.Artifact{ID: "young-abandoned", ClaimID: "ended", URI: young.URI, CreatedAt: recent}
	repository.idempotency[idempotencyTestKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "stale-upload"}, "upload-old")] = IdempotencyRecord{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "stale-upload"}, OperationID: "upload-old", Operation: artifactUploadOperation,
		Status: IdempotencyPending, CreatedAt: old,
	}
	registeredURI, err := local.UploadURI("stale-upload-with-file")
	if err != nil {
		t.Fatalf("prepare registered upload URI: %v", err)
	}
	registeredBlob, err := local.Put(context.Background(), registeredURI, strings.NewReader("pending content"))
	if err != nil {
		t.Fatalf("write registered pending upload: %v", err)
	}
	state, err := json.Marshal(artifactUploadState{BlobURI: registeredBlob.URI, Digest: registeredBlob.Digest, Size: registeredBlob.Size})
	if err != nil {
		t.Fatalf("encode pending upload state: %v", err)
	}
	repository.idempotency[idempotencyTestKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "stale-upload-file"}, "upload-old-file")] = IdempotencyRecord{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "stale-upload-file"}, OperationID: "upload-old-file", Operation: artifactUploadOperation,
		Status: IdempotencyPending, Response: string(state), CreatedAt: old,
	}
	repository.idempotency[idempotencyTestKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "kept-upload"}, "upload-recent")] = IdempotencyRecord{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "kept-upload"}, OperationID: "upload-recent", Operation: artifactUploadOperation,
		Status: IdempotencyPending, CreatedAt: recent,
	}
	completedUploadKey := idempotencyTestKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "completed-upload"}, "upload-completed")
	repository.idempotency[completedUploadKey] = IdempotencyRecord{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "completed-upload"}, OperationID: "upload-completed", Operation: artifactUploadOperation,
		Status: IdempotencyCompleted, CreatedAt: old,
	}
	otherPendingKey := idempotencyTestKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "other-pending"}, "other-operation")
	repository.idempotency[otherPendingKey] = IdempotencyRecord{
		Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "other-pending"}, OperationID: "other-operation", Operation: "other_pending_operation",
		Status: IdempotencyPending, CreatedAt: old,
	}

	result, err := service.GarbageCollectArtifacts(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("garbage collect Artifacts: %v", err)
	}
	if result.ArtifactsDeleted != 2 || result.BlobsDeleted != 1 || result.PendingDeleted != 2 {
		t.Fatalf("GC result = %#v", result)
	}
	if _, exists := repository.artifacts["abandoned-shared"]; exists {
		t.Fatal("shared abandoned Artifact was not deleted")
	}
	if _, exists := repository.artifacts["abandoned-orphan"]; exists {
		t.Fatal("orphaned abandoned Artifact was not deleted")
	}
	for _, id := range []domain.ArtifactID{"committed-shared", "active-staged", "young-abandoned"} {
		if _, exists := repository.artifacts[id]; !exists {
			t.Fatalf("Artifact %q should have been retained", id)
		}
	}
	if _, exists := repository.blobs[orphan.URI]; exists {
		t.Fatal("unreferenced Blob metadata was not deleted")
	}
	if _, exists := repository.idempotency[idempotencyTestKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "stale-upload"}, "upload-old")]; exists {
		t.Fatal("stale pending upload was not collected")
	}
	if _, exists := repository.idempotency[idempotencyTestKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "stale-upload-file"}, "upload-old-file")]; exists {
		t.Fatal("stale pending upload with file was not collected")
	}
	if _, err := local.Open(context.Background(), registeredBlob.URI); err == nil {
		t.Fatal("pending registered file was not deleted")
	}
	if _, exists := repository.idempotency[idempotencyTestKey(domain.ActorRef{Kind: domain.ActorAgent, ID: "kept-upload"}, "upload-recent")]; !exists {
		t.Fatal("recent pending upload was collected")
	}
	if _, exists := repository.idempotency[completedUploadKey]; !exists {
		t.Fatal("completed upload idempotency record was collected")
	}
	if _, exists := repository.idempotency[otherPendingKey]; !exists {
		t.Fatal("non-upload pending operation was collected")
	}
	if _, err := local.Open(context.Background(), orphan.URI); err == nil {
		t.Fatal("unreferenced Blob content was not deleted")
	}
	for _, uri := range []string{shared.URI, active.URI, young.URI} {
		reader, err := local.Open(context.Background(), uri)
		if err != nil {
			t.Fatalf("retained Blob %q: %v", uri, err)
		}
		_ = reader.Close()
	}
}

func TestGetBlackboardTaskExecutionContext(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	definition.AgentInstructions = "Keep the board small and update shared findings."
	definition.SuggestedTags = []string{"module:*"}
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Blackboard context", Goal: "Inspect shared work",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	first, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   agent, Title: "Investigate", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	second, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   agent, Title: "Summarize", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	if _, err := service.AddBlackboardRelation(context.Background(), AddBlackboardRelationCommand{
		WorkItemID: workItem.ID,
		FromTaskID: first.ID, ToTaskID: second.ID, Identity: agent,
	}); err != nil {
		t.Fatalf("add relation: %v", err)
	}
	pendingContext, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{
		TaskID: second.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get pending execution context: %v", err)
	}
	if pendingContext.Claims == nil || len(pendingContext.Claims) != 0 {
		t.Fatalf("pending claims = %#v, want non-nil empty slice", pendingContext.Claims)
	}
	if pendingContext.Task.AllowedRoles == nil || pendingContext.Task.Tags == nil || pendingContext.Task.Reviews == nil ||
		pendingContext.Task.Submissions == nil || pendingContext.Task.Failures == nil || pendingContext.Task.TransitionDecisions == nil {
		t.Fatalf("pending task contains nil collections: %#v", pendingContext.Task)
	}
	if pendingContext.Blackboard == nil || pendingContext.Blackboard.Tasks == nil || pendingContext.Blackboard.Relations == nil {
		t.Fatalf("pending blackboard contains nil collections: %#v", pendingContext.Blackboard)
	}
	if _, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: first.ID, Identity: agent}); err != nil {
		t.Fatalf("claim first task: %v", err)
	}
	contextView, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{
		TaskID: first.ID, Identity: agent,
	})
	if err != nil {
		t.Fatalf("get execution context: %v", err)
	}
	if contextView.Blackboard == nil || contextView.Workflow != nil {
		t.Fatalf("mode context: %#v", contextView)
	}
	if !contextView.Blackboard.CanDecompose {
		t.Fatalf("freshly claimed blackboard task cannot decompose: %#v", contextView.Blackboard)
	}
	if len(contextView.Blackboard.Tasks) != 1 || contextView.Blackboard.Tasks[0].ID != second.ID || len(contextView.Blackboard.Relations) != 1 {
		t.Fatalf("blackboard space: %#v", contextView.Blackboard)
	}
	if contextView.Definition.AgentInstructions != definition.AgentInstructions {
		t.Fatalf("agent instructions: got %q", contextView.Definition.AgentInstructions)
	}
}

func TestCreateWorkItemBindsLatestDefinition(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	v1 := blackboardDefinition()
	v2 := v1
	v2.Version = 2
	v2.Name = "Current collaboration"
	v3 := v2
	v3.Version = 3
	repository.blackboards[definitionKey(v1.ID, v1.Version)] = v1
	repository.blackboards[definitionKey(v2.ID, v2.Version)] = v2
	repository.blackboards[definitionKey(v3.ID, v3.Version)] = v3
	service := newTestService(t, repository)

	created, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: v1.ID, Mode: domain.CoordinationModeBlackboard},
		Identity:   Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}},
		Title:      "Use current rules", Goal: "Bind without choosing a version",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	if created.Definition != v3.Binding() {
		t.Fatalf("definition binding = %#v, want %#v", created.Definition, v3.Binding())
	}
}

func TestCancelWorkItemEndsActiveClaimsAndRejectsFurtherMutations(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "worker"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: human, Title: "Cancelable work", Goal: "Stop cleanly", AcceptanceMode: domain.WorkItemAcceptanceHuman,
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	agentTask, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: human, Title: "Agent task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create agent task: %v", err)
	}
	humanTask, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: human, Title: "Human task", Executor: domain.ExecutorHuman,
	})
	if err != nil {
		t.Fatalf("create human task: %v", err)
	}
	pendingTask, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: human, Title: "Pending task", Executor: domain.ExecutorEither,
	})
	if err != nil {
		t.Fatalf("create pending task: %v", err)
	}
	agentClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: agentTask.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim agent task: %v", err)
	}
	humanClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: humanTask.ID, Identity: human})
	if err != nil {
		t.Fatalf("claim human task: %v", err)
	}
	pendingVersion := repository.tasks[pendingTask.ID].Version

	command := CancelWorkItemCommand{
		WorkItemID: workItem.ID, Identity: human, OperationID: "cancel-work", Reason: "  Work is no longer required.  ",
	}
	cancelled, err := service.CancelWorkItem(context.Background(), command)
	if err != nil {
		t.Fatalf("cancel work item: %v", err)
	}
	if cancelled.Status != domain.WorkItemStatusCancelled || cancelled.CancelledAt == nil || cancelled.CancelledBy == nil || *cancelled.CancelledBy != human.Actor {
		t.Fatalf("cancelled WorkItem metadata = %#v", cancelled)
	}
	if cancelled.CancellationReason != "Work is no longer required." || cancelled.Result != "" || cancelled.CompletedAt != nil {
		t.Fatalf("cancelled WorkItem outcome = %#v", cancelled)
	}
	for _, taskID := range []domain.TaskID{agentTask.ID, humanTask.ID} {
		task := repository.tasks[taskID]
		if task.Status != domain.TaskStatusPending || task.ActiveClaimID != nil {
			t.Fatalf("cancelled active task %q = %#v", taskID, task)
		}
	}
	if task := repository.tasks[pendingTask.ID]; task.Status != domain.TaskStatusPending || task.Version != pendingVersion {
		t.Fatalf("unclaimed pending task changed = %#v", task)
	}
	for _, claimID := range []domain.ClaimID{agentClaim.ID, humanClaim.ID} {
		claim := repository.claims[claimID]
		if claim.EndedAt == nil || claim.EndReason != domain.ClaimEndWorkItemCancelled {
			t.Fatalf("cancelled claim %q = %#v", claimID, claim)
		}
	}
	var revoked int
	for _, event := range repository.events {
		if event.Type == domain.WorkItemEventTaskRevoked {
			revoked++
		}
	}
	if revoked != 2 || repository.events[len(repository.events)-1].Type != domain.WorkItemEventWorkItemCancelled {
		t.Fatalf("cancellation events = %#v", repository.events)
	}

	replayed, err := service.CancelWorkItem(context.Background(), command)
	if err != nil || replayed.Status != domain.WorkItemStatusCancelled || replayed.CancellationReason != cancelled.CancellationReason {
		t.Fatalf("idempotent cancellation = %#v, %v", replayed, err)
	}
	if _, err := service.CancelWorkItem(context.Background(), CancelWorkItemCommand{
		WorkItemID: workItem.ID, Identity: human, OperationID: "cancel-again", Reason: "again",
	}); !errors.Is(err, ErrWorkItemCancelled) {
		t.Fatalf("second cancellation error = %v, want ErrWorkItemCancelled", err)
	}
	if _, err := service.HeartbeatClaim(context.Background(), HeartbeatClaimCommand{
		TaskID: agentTask.ID, ClaimID: agentClaim.ID, Identity: agent,
	}); !errors.Is(err, ErrWorkItemCancelled) {
		t.Fatalf("heartbeat after cancellation error = %v, want ErrWorkItemCancelled", err)
	}
	if _, err := service.ClaimTask(context.Background(), ClaimTaskCommand{
		TaskID: pendingTask.ID, Identity: human,
	}); !errors.Is(err, ErrWorkItemCancelled) {
		t.Fatalf("claim after cancellation error = %v, want ErrWorkItemCancelled", err)
	}
}

func TestCancelWorkItemRequiresHumanReasonAndActiveState(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "worker"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: human, Title: "Cancelable work", Goal: "Stop cleanly",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	if _, err := service.CancelWorkItem(context.Background(), CancelWorkItemCommand{
		WorkItemID: workItem.ID, Identity: agent, Reason: "not mine",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent cancellation error = %v, want ErrForbidden", err)
	}
	if _, err := service.CancelWorkItem(context.Background(), CancelWorkItemCommand{
		WorkItemID: workItem.ID, Identity: human, Reason: "  ",
	}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("empty reason error = %v, want ErrInvalidCommand", err)
	}
	completed := repository.workItems[workItem.ID]
	completed.Status = domain.WorkItemStatusCompleted
	completedAt := applicationTestTime
	completed.CompletedAt = &completedAt
	repository.workItems[workItem.ID] = completed
	if _, err := service.CancelWorkItem(context.Background(), CancelWorkItemCommand{
		WorkItemID: workItem.ID, Identity: human, Reason: "too late",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("completed cancellation error = %v, want ErrConflict", err)
	}
}

var applicationTestTime = time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)

func TestCreateWorkflowWorkItemAndClaimByRole(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.workflows[definitionKey("workflow", 1)] = workflowDefinition()
	service := newTestService(t, repository)
	backend := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-backend"}, Role: "backend"}

	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "workflow", Version: 1, Mode: domain.CoordinationModeWorkflow},
		Identity:   backend,
		Title:      "Implement login",
		Goal:       "Users can log in",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	tasks := repository.tasksFor(workItem.ID)
	if len(tasks) != 1 || tasks[0].WorkflowTaskID == nil || *tasks[0].WorkflowTaskID != "implement" {
		t.Fatalf("workflow start tasks: %#v", tasks)
	}

	frontend := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-frontend"}, Role: "frontend"}
	frontendWork, err := service.FindWork(context.Background(), FindWorkQuery{Identity: frontend})
	if err != nil {
		t.Fatalf("find frontend work: %v", err)
	}
	if len(frontendWork) != 0 {
		t.Fatalf("frontend candidates: got %d, want 0", len(frontendWork))
	}

	candidates, err := service.FindWork(context.Background(), FindWorkQuery{Identity: backend, Tags: []string{"unrelated-visual-label"}})
	if err != nil {
		t.Fatalf("find backend work: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("workflow candidates must ignore tags: got %d, want 1", len(candidates))
	}
	if candidates[0].Definition.Name == "" {
		t.Fatalf("backend candidate is missing Definition context: %#v", candidates[0])
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: candidates[0].Task.ID, Identity: backend})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	claimed := repository.tasks[candidates[0].Task.ID]
	if claimed.Status != domain.TaskStatusWorking || claimed.ActiveClaimID == nil || *claimed.ActiveClaimID != claim.ID {
		t.Fatalf("claimed task state: %#v", claimed)
	}

	if err := service.ReleaseClaim(context.Background(), ReleaseClaimCommand{
		TaskID: claimed.ID, ClaimID: claim.ID, Identity: backend, Reason: "Waiting for external access",
	}); err != nil {
		t.Fatalf("release claim: %v", err)
	}
	released := repository.tasks[claimed.ID]
	if released.Status != domain.TaskStatusPending || released.ActiveClaimID != nil {
		t.Fatalf("released task state: %#v", released)
	}
	if got := repository.events[len(repository.events)-1].Message; got != "Waiting for external access" {
		t.Fatalf("release event reason = %q", got)
	}
}

func TestFindWorkDiscoversEmptyBlackboard(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	definition.AgentInstructions = "Create the smallest useful plan."
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent,
		Title: "Empty board", Goal: "Create a plan", Tags: []string{"module:auth"},
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	candidates, err := service.FindWork(context.Background(), FindWorkQuery{
		Identity: agent, Tags: []string{"module:auth"},
	})
	if err != nil {
		t.Fatalf("find empty blackboard: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Kind != WorkCandidateEmptyBlackboard || candidates[0].WorkItem.ID != workItem.ID {
		t.Fatalf("empty blackboard candidate: %#v", candidates)
	}
	if candidates[0].Definition.AgentInstructions != definition.AgentInstructions {
		t.Fatalf("definition context: %#v", candidates[0].Definition)
	}
	other, err := service.FindWork(context.Background(), FindWorkQuery{
		Identity: agent, Tags: []string{"module:billing"},
	})
	if err != nil || len(other) != 0 {
		t.Fatalf("unmatched empty blackboard: candidates=%#v err=%v", other, err)
	}
	completed, err := service.SubmitBlackboardCompletion(context.Background(), SubmitBlackboardCompletionCommand{
		WorkItemID: workItem.ID,
		Identity:   agent,
		Result:     "The goal requires no execution tasks.",
	})
	if err != nil {
		t.Fatalf("complete empty blackboard: %v", err)
	}
	if completed.Status != domain.WorkItemStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed empty blackboard: %#v", completed)
	}
}

func TestFindWorkPrioritizesLifecycleDecisionsAndFiltersAgentAcceptanceFromHumans(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "generalist"}
	human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "human"}}

	createWorkItem := func(title string, acceptanceMode domain.WorkItemAcceptanceMode) domain.WorkItem {
		t.Helper()
		workItem, err := service.CreateWorkItem(ctx, CreateWorkItemCommand{
			Definition: definition.Binding(), Identity: agent, Title: title, Goal: "Exercise find_work ordering", AcceptanceMode: acceptanceMode,
		})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		return workItem
	}

	awaitingAcceptance := createWorkItem("Awaiting acceptance", domain.WorkItemAcceptanceAgent)
	if _, err := service.SubmitBlackboardCompletion(ctx, SubmitBlackboardCompletionCommand{
		WorkItemID: awaitingAcceptance.ID, Identity: agent, Result: "ready",
	}); err != nil {
		t.Fatalf("submit completion for acceptance: %v", err)
	}

	converged := createWorkItem("Converged", domain.WorkItemAcceptanceNone)
	completedTask, err := service.CreateBlackboardTask(ctx, CreateBlackboardTaskCommand{
		WorkItemID: converged.ID, Identity: agent, Title: "Completed task", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create completed task: %v", err)
	}
	claim, err := service.ClaimTask(ctx, ClaimTaskCommand{TaskID: completedTask.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim completed task: %v", err)
	}
	if _, err := service.SubmitTask(ctx, SubmitTaskCommand{TaskID: completedTask.ID, ClaimID: claim.ID, Identity: agent, Result: "done"}); err != nil {
		t.Fatalf("submit completed task: %v", err)
	}

	pending := createWorkItem("Executable task", domain.WorkItemAcceptanceNone)
	if _, err := service.CreateBlackboardTask(ctx, CreateBlackboardTaskCommand{
		WorkItemID: pending.ID, Identity: agent, Title: "Pending task", Executor: domain.ExecutorAgent,
	}); err != nil {
		t.Fatalf("create pending task: %v", err)
	}
	createWorkItem("Empty board", domain.WorkItemAcceptanceNone)

	agentCandidates, err := service.FindWork(ctx, FindWorkQuery{Identity: agent, Limit: 1})
	if err != nil {
		t.Fatalf("find agent work: %v", err)
	}
	if len(agentCandidates) != 1 || agentCandidates[0].Kind != WorkCandidateWorkItemAcceptance || agentCandidates[0].WorkItem.ID != awaitingAcceptance.ID {
		t.Fatalf("agent candidates = %#v, want acceptance first", agentCandidates)
	}
	if agentCandidates[0].WorkItem.Tags == nil || agentCandidates[0].Definition.SuggestedTags == nil {
		t.Fatalf("lifecycle candidate contains nil collections: %#v", agentCandidates[0])
	}

	humanCandidates, err := service.FindWork(ctx, FindWorkQuery{Identity: human})
	if err != nil {
		t.Fatalf("find human work: %v", err)
	}
	if len(humanCandidates) == 0 || humanCandidates[0].Kind != WorkCandidateBlackboardCompletion || humanCandidates[0].WorkItem.ID != converged.ID {
		t.Fatalf("human candidates = %#v, want completion first", humanCandidates)
	}
	for _, candidate := range humanCandidates {
		if candidate.Kind == WorkCandidateWorkItemAcceptance {
			t.Fatalf("human candidates include agent acceptance: %#v", humanCandidates)
		}
	}

	if _, err := service.AcceptBlackboardCompletion(ctx, AcceptBlackboardCompletionCommand{WorkItemID: awaitingAcceptance.ID, Identity: agent}); err != nil {
		t.Fatalf("accept completion: %v", err)
	}
	nextCandidates, err := service.FindWork(ctx, FindWorkQuery{Identity: agent, Limit: 1})
	if err != nil {
		t.Fatalf("find work after acceptance: %v", err)
	}
	if len(nextCandidates) != 1 || nextCandidates[0].Kind != WorkCandidateBlackboardCompletion || nextCandidates[0].WorkItem.ID != converged.ID {
		t.Fatalf("next candidates = %#v, want completion before task or planning", nextCandidates)
	}
	if _, err := service.SubmitBlackboardCompletion(ctx, SubmitBlackboardCompletionCommand{
		WorkItemID: converged.ID, Identity: agent, Result: "completed",
	}); err != nil {
		t.Fatalf("submit converged completion: %v", err)
	}
	taskCandidates, err := service.FindWork(ctx, FindWorkQuery{Identity: agent, Limit: 1})
	if err != nil {
		t.Fatalf("find task work: %v", err)
	}
	if len(taskCandidates) != 1 || taskCandidates[0].Kind != WorkCandidateTask || taskCandidates[0].Task == nil {
		t.Fatalf("task candidates = %#v", taskCandidates)
	}
	taskCandidate := taskCandidates[0]
	if taskCandidate.WorkItem.Tags == nil || taskCandidate.Definition.SuggestedTags == nil ||
		taskCandidate.Task.AllowedRoles == nil || taskCandidate.Task.Tags == nil || taskCandidate.Task.Reviews == nil ||
		taskCandidate.Task.Submissions == nil || taskCandidate.Task.Failures == nil || taskCandidate.Task.TransitionDecisions == nil {
		t.Fatalf("task candidate contains nil collections: %#v", taskCandidate)
	}
}

func TestBlackboardAcceptanceModes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "acceptor"}, Role: "generalist"}
	for _, mode := range []domain.WorkItemAcceptanceMode{domain.WorkItemAcceptanceNone, domain.WorkItemAcceptanceAgent, domain.WorkItemAcceptanceHuman} {
		t.Run(string(mode), func(t *testing.T) {
			repository := newTestRepository()
			repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
			service := newTestService(t, repository)
			workItem, err := service.CreateWorkItem(ctx, CreateWorkItemCommand{Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard}, Identity: agent, Title: "Acceptance", Goal: "Test acceptance", AcceptanceMode: mode})
			if err != nil {
				t.Fatalf("create work item: %v", err)
			}
			task, err := service.CreateBlackboardTask(ctx, CreateBlackboardTaskCommand{WorkItemID: workItem.ID, Identity: agent, Title: "Finish", Executor: domain.ExecutorAgent})
			if err != nil {
				t.Fatalf("create task: %v", err)
			}
			if _, err := service.SubmitBlackboardCompletion(ctx, SubmitBlackboardCompletionCommand{
				WorkItemID: workItem.ID, Identity: agent, Result: "too early",
			}); !errors.Is(err, ErrConflict) {
				t.Fatalf("submit completion before task convergence: got %v", err)
			}
			claim, err := service.ClaimTask(ctx, ClaimTaskCommand{TaskID: task.ID, Identity: agent})
			if err != nil {
				t.Fatalf("claim task: %v", err)
			}
			if _, err := service.SubmitTask(ctx, SubmitTaskCommand{TaskID: task.ID, ClaimID: claim.ID, Identity: agent, Result: "done"}); err != nil {
				t.Fatalf("submit task: %v", err)
			}
			got := repository.workItems[workItem.ID]
			if got.Status != domain.WorkItemStatusOpen {
				t.Fatalf("status after task convergence = %s, want open", got.Status)
			}
			candidates, err := service.FindWork(ctx, FindWorkQuery{Identity: agent})
			if err != nil || len(candidates) != 1 || candidates[0].Kind != WorkCandidateBlackboardCompletion {
				t.Fatalf("completion candidates = %#v, err=%v", candidates, err)
			}
			got, err = service.SubmitBlackboardCompletion(ctx, SubmitBlackboardCompletionCommand{
				WorkItemID: workItem.ID, Identity: agent, Result: "done",
			})
			if err != nil {
				t.Fatalf("submit completion: %v", err)
			}
			switch mode {
			case domain.WorkItemAcceptanceNone:
				if got.Status != domain.WorkItemStatusCompleted {
					t.Fatalf("status = %s", got.Status)
				}
			case domain.WorkItemAcceptanceAgent:
				if got.Status != domain.WorkItemStatusAwaitingAgentAcceptance {
					t.Fatalf("status = %s", got.Status)
				}
				candidates, err := service.FindWork(ctx, FindWorkQuery{Identity: agent})
				if err != nil || len(candidates) != 1 || candidates[0].Kind != WorkCandidateWorkItemAcceptance {
					t.Fatalf("acceptance candidates = %#v, err=%v", candidates, err)
				}
				followUp, err := service.CreateBlackboardTask(ctx, CreateBlackboardTaskCommand{
					WorkItemID: workItem.ID, Identity: agent, Title: "Address acceptance feedback", Executor: domain.ExecutorAgent,
				})
				if err != nil {
					t.Fatalf("create acceptance follow-up: %v", err)
				}
				reopened := repository.workItems[workItem.ID]
				if reopened.Status != domain.WorkItemStatusOpen || reopened.Result != "" {
					t.Fatalf("reopened WorkItem = %#v, want open with no stale result", reopened)
				}
				followUpClaim, err := service.ClaimTask(ctx, ClaimTaskCommand{TaskID: followUp.ID, Identity: agent})
				if err != nil {
					t.Fatalf("claim acceptance follow-up: %v", err)
				}
				if _, err := service.SubmitTask(ctx, SubmitTaskCommand{TaskID: followUp.ID, ClaimID: followUpClaim.ID, Identity: agent, Result: "feedback addressed"}); err != nil {
					t.Fatalf("submit acceptance follow-up: %v", err)
				}
				got, err = service.SubmitBlackboardCompletion(ctx, SubmitBlackboardCompletionCommand{
					WorkItemID: workItem.ID, Identity: agent, Result: "done after feedback",
				})
				if err != nil || got.Status != domain.WorkItemStatusAwaitingAgentAcceptance || got.Result != "done after feedback" {
					t.Fatalf("resubmit completion = %#v, err=%v", got, err)
				}
				accepted, err := service.AcceptBlackboardCompletion(ctx, AcceptBlackboardCompletionCommand{WorkItemID: workItem.ID, Identity: agent})
				if err != nil || accepted.Status != domain.WorkItemStatusCompleted {
					t.Fatalf("accept agent completion = %#v, err=%v", accepted, err)
				}
			case domain.WorkItemAcceptanceHuman:
				if got.Status != domain.WorkItemStatusAwaitingHumanAcceptance {
					t.Fatalf("status = %s", got.Status)
				}
				attention, err := service.ListHumanAttention(ctx, Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}, PageRequest[HumanAttentionCursor]{Limit: 50})
				if err != nil || len(attention.Items) != 1 || attention.Items[0].Kind != HumanAttentionAcceptance || attention.Items[0].WorkItem.ID != workItem.ID {
					t.Fatalf("human acceptance attention = %#v, err=%v", attention, err)
				}
				candidates, err := service.FindWork(ctx, FindWorkQuery{Identity: agent})
				if err != nil || len(candidates) != 0 {
					t.Fatalf("human acceptance candidates = %#v, err=%v", candidates, err)
				}
				human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
				accepted, err := service.AcceptBlackboardCompletion(ctx, AcceptBlackboardCompletionCommand{WorkItemID: workItem.ID, Identity: human})
				if err != nil || accepted.Status != domain.WorkItemStatusCompleted {
					t.Fatalf("accept human completion = %#v, err=%v", accepted, err)
				}
			}
		})
	}
}

func TestHumanCanSubmitBlackboardCompletion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "operator"}}
	workItem, err := service.CreateWorkItem(ctx, CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   human, Title: "Human completion", Goal: "Allow people to close collaborative work",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(ctx, CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: human, Title: "Human task", Executor: domain.ExecutorHuman,
	})
	if err != nil {
		t.Fatalf("create human task: %v", err)
	}
	claim, err := service.ClaimTask(ctx, ClaimTaskCommand{TaskID: task.ID, Identity: human})
	if err != nil {
		t.Fatalf("claim human task: %v", err)
	}
	if _, err := service.SubmitTask(ctx, SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: human, Result: "human task completed",
	}); err != nil {
		t.Fatalf("submit human task: %v", err)
	}
	completed, err := service.SubmitBlackboardCompletion(ctx, SubmitBlackboardCompletionCommand{
		WorkItemID: workItem.ID, Identity: human, Result: "human completed the work item",
	})
	if err != nil || completed.Status != domain.WorkItemStatusCompleted || completed.Result != "human completed the work item" {
		t.Fatalf("submit human completion = %#v, err=%v", completed, err)
	}
}

func TestProjectTaskLifecycleUsesStateSpecificResponsibility(t *testing.T) {
	now := time.Now().UTC()
	actor := domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}
	claim := domain.Claim{ID: "claim-1", Executor: actor, ClaimedAt: now}
	task := domain.Task{ID: "task-1", Status: domain.TaskStatusSkipped, CompletedAt: &now, SkippedBy: &actor, SkipReason: "obsolete"}
	responsibility, outcome := projectTaskLifecycle(task, []domain.Claim{claim})
	if responsibility.Kind != "skipped_by" || responsibility.Actor == nil || responsibility.Actor.ID != "planner" || outcome.Kind != "skipped" || outcome.Reason != "obsolete" {
		t.Fatalf("skipped projection: %#v %#v", responsibility, outcome)
	}
	task.Status = domain.TaskStatusWorking
	claimID := domain.ClaimID("claim-1")
	task.ActiveClaimID = &claimID
	responsibility, outcome = projectTaskLifecycle(task, []domain.Claim{claim})
	if responsibility.Kind != "claimed_by" || responsibility.Actor == nil || outcome.Kind != "active" {
		t.Fatalf("working projection: %#v %#v", responsibility, outcome)
	}

	task = domain.Task{ID: "task-1", Status: domain.TaskStatusInReview, TransitionDecisions: []domain.TransitionDecision{{DecidedBy: actor}}}
	responsibility, outcome = projectTaskLifecycle(task, nil)
	if responsibility.Kind != "skip_requested_by" || responsibility.Actor == nil || responsibility.Actor.ID != actor.ID || outcome.Kind != "in_review" {
		t.Fatalf("skip review projection: %#v %#v", responsibility, outcome)
	}

	task = domain.Task{ID: "task-1", Status: domain.TaskStatusCompleted, CompletedAt: &now, DecomposedAt: &now}
	claim.EndReason = domain.ClaimEndTaskDecomposed
	responsibility, outcome = projectTaskLifecycle(task, []domain.Claim{claim})
	if responsibility.Kind != "decomposed_by" || outcome.Kind != "completed" {
		t.Fatalf("completed decomposition projection: %#v %#v", responsibility, outcome)
	}
}

func TestBlackboardPlanningRequiresExplicitCompletion(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}

	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   identity,
		Title:      "Investigate flaky tests",
		Goal:       "Make the suite reliable",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	first, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity,
		Title:      "Collect failures",
		Executor:   domain.ExecutorEither,
		Tags:       []string{"investigation"},
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}
	second, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity,
		Title:      "Remove obsolete hypothesis",
		Executor:   domain.ExecutorAgent,
		Tags:       []string{"cleanup"},
	})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	if _, err := service.AddBlackboardRelation(context.Background(), AddBlackboardRelationCommand{
		WorkItemID: workItem.ID,
		FromTaskID: first.ID,
		ToTaskID:   second.ID,
		Identity:   identity,
	}); err != nil {
		t.Fatalf("add relation: %v", err)
	}
	if _, err := service.AddBlackboardRelation(context.Background(), AddBlackboardRelationCommand{
		WorkItemID: workItem.ID,
		FromTaskID: second.ID,
		ToTaskID:   first.ID,
		Identity:   identity,
	}); err == nil {
		t.Fatal("add cyclic relation: got nil error")
	}

	skippedFirst, err := service.SkipBlackboardTask(context.Background(), SkipBlackboardTaskCommand{
		TaskID: first.ID, Identity: identity, Reason: "Covered by existing logs",
	})
	if err != nil {
		t.Fatalf("skip first task: %v", err)
	}
	if skippedFirst.SkippedBy == nil || *skippedFirst.SkippedBy != identity.Actor || skippedFirst.SkipReason != "Covered by existing logs" {
		t.Fatalf("blackboard skip decision: %#v", skippedFirst)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusOpen {
		t.Fatalf("blackboard completed with pending task: got %s", got)
	}
	if _, err := service.SkipBlackboardTask(context.Background(), SkipBlackboardTaskCommand{
		TaskID: second.ID, Identity: identity, Reason: "No longer useful",
	}); err != nil {
		t.Fatalf("skip second task: %v", err)
	}
	converged := repository.workItems[workItem.ID]
	if converged.Status != domain.WorkItemStatusOpen || converged.CompletedAt != nil {
		t.Fatalf("converged blackboard: %#v", converged)
	}
	completed, err := service.SubmitBlackboardCompletion(context.Background(), SubmitBlackboardCompletionCommand{
		WorkItemID: workItem.ID, Identity: identity, Result: "No remaining work is useful.",
	})
	if err != nil || completed.Status != domain.WorkItemStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("submit blackboard completion: %#v, err=%v", completed, err)
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: identity, Title: "Late task", Executor: domain.ExecutorAgent,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("append after submitted completion: got %v", err)
	}
}

func TestListHumanAttentionAggregatesHumanTasksAndReviews(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	planner := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   planner, Title: "Human attention", Goal: "Exercise the attention feed",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: planner, Title: "Check the result", Executor: domain.ExecutorHuman,
	})
	if err != nil {
		t.Fatalf("create human task: %v", err)
	}
	page, err := service.ListHumanAttention(context.Background(), human, PageRequest[HumanAttentionCursor]{Limit: 50})
	if err != nil {
		t.Fatalf("list human attention: %v", err)
	}
	items := page.Items
	if len(items) != 1 || items[0].Kind != HumanAttentionTask || items[0].Task.ID != task.ID {
		t.Fatalf("pending human attention = %+v", items)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: human})
	if err != nil {
		t.Fatalf("claim human task: %v", err)
	}
	page, err = service.ListHumanAttention(context.Background(), human, PageRequest[HumanAttentionCursor]{Limit: 50})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("claimed human attention = %+v, err = %v", page.Items, err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: human, Result: "Ready for review", RequestReview: true,
	}); err != nil {
		t.Fatalf("submit for review: %v", err)
	}
	page, err = service.ListHumanAttention(context.Background(), human, PageRequest[HumanAttentionCursor]{Limit: 50})
	if err != nil {
		t.Fatalf("list review attention: %v", err)
	}
	items = page.Items
	if len(items) != 1 || items[0].Kind != HumanAttentionReview || items[0].Task.ID != task.ID {
		t.Fatalf("review attention = %+v", items)
	}
}

func TestListHumanAttentionPaginatesWithStableCursor(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	planner := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	human := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	for _, title := range []string{"First attention item", "Second attention item"} {
		workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
			Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
			Identity:   planner, Title: title, Goal: "Exercise Human Attention pagination",
		})
		if err != nil {
			t.Fatalf("create WorkItem: %v", err)
		}
		if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
			WorkItemID: workItem.ID, Identity: planner, Title: title, Executor: domain.ExecutorHuman,
		}); err != nil {
			t.Fatalf("create human Task: %v", err)
		}
	}

	first, err := service.ListHumanAttention(context.Background(), human, PageRequest[HumanAttentionCursor]{Limit: 1})
	if err != nil {
		t.Fatalf("list first Human Attention page: %v", err)
	}
	if len(first.Items) != 1 || !first.HasMore {
		t.Fatalf("first Human Attention page = %#v, want one item and more", first)
	}
	cursor := first.Items[0].Cursor()
	second, err := service.ListHumanAttention(context.Background(), human, PageRequest[HumanAttentionCursor]{Limit: 1, After: &cursor})
	if err != nil {
		t.Fatalf("list second Human Attention page: %v", err)
	}
	if len(second.Items) != 1 || second.HasMore || second.Items[0].Cursor() == cursor {
		t.Fatalf("second Human Attention page = %#v after %#v", second, first)
	}
}

func TestBlackboardFollowUpCreatedBeforeSubmissionKeepsWorkItemOpen(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := blackboardDefinition()
	repository.blackboards[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Investigate login", Goal: "Resolve the login issue",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	first, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Investigate failure", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create initial task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: first.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim initial task: %v", err)
	}
	followUp, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: agent, Title: "Apply discovered fix", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create follow-up task: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: first.ID, ClaimID: claim.ID, Identity: agent, Result: "Found the root cause",
	}); err != nil {
		t.Fatalf("submit initial task: %v", err)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusOpen {
		t.Fatalf("blackboard with follow-up task: got %s", got)
	}
	followUpClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: followUp.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim follow-up task: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: followUp.ID, ClaimID: followUpClaim.ID, Identity: agent, Result: "Applied the fix",
	}); err != nil {
		t.Fatalf("submit follow-up task: %v", err)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusOpen {
		t.Fatalf("blackboard after final task: got %s, want open", got)
	}
	completed, err := service.SubmitBlackboardCompletion(context.Background(), SubmitBlackboardCompletionCommand{
		WorkItemID: workItem.ID, Identity: agent,
		Result: summarizeWorkItemResult(repository.tasksFor(workItem.ID)),
	})
	if err != nil {
		t.Fatalf("submit completion: %v", err)
	}
	if !strings.Contains(completed.Result, "Investigate failure\nFound the root cause") ||
		!strings.Contains(completed.Result, "Apply discovered fix\nApplied the fix") {
		t.Fatalf("aggregated WorkItem result = %q", completed.Result)
	}
	contextView, err := service.GetWorkItemExecutionContext(context.Background(), GetWorkItemExecutionContextQuery{
		WorkItemID: workItem.ID,
		Identity:   agent,
	})
	if err != nil {
		t.Fatalf("get completed WorkItem context: %v", err)
	}
	if contextView.WorkItem.Status != domain.WorkItemStatusCompleted || len(contextView.Tasks) != 2 || contextView.Relations == nil {
		t.Fatalf("completed WorkItem context: %#v", contextView)
	}
}

func TestBlackboardPlanningAppendsAgainstLatestRevision(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   identity,
		Title:      "Plan login",
		Goal:       "Produce one coherent plan",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity, Title: "Implement login", Executor: domain.ExecutorAgent,
	}); err != nil {
		t.Fatalf("create first task: %v", err)
	}
	if _, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity, Title: "Test login", Executor: domain.ExecutorAgent,
	}); err != nil {
		t.Fatalf("append second task: %v", err)
	}
	if got := repository.workItems[workItem.ID].Version; got != 2 {
		t.Fatalf("work item version: got %d, want 2", got)
	}
	if got := len(repository.tasksFor(workItem.ID)); got != 2 {
		t.Fatalf("task count: got %d, want 2", got)
	}
}

func TestBlackboardTaskHierarchySupportsOpenAppendAndRecursiveCompletion(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	planner := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	contributor := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "contributor"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   planner, Title: "Implement login", Goal: "Deliver login",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	root, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID, Identity: planner, Title: "Implement login", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create root task: %v", err)
	}
	if root.AllowedRoles == nil || root.Tags == nil || root.Reviews == nil || root.Submissions == nil || root.Failures == nil || root.TransitionDecisions == nil {
		t.Fatalf("created root contains nil collections: %#v", root)
	}
	rootClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: root.ID, Identity: planner})
	if err != nil {
		t.Fatalf("claim root task: %v", err)
	}
	decomposeCommand := DecomposeBlackboardTaskCommand{
		TaskID: root.ID, ClaimID: rootClaim.ID, Identity: planner,
		OperationID: "decompose-login",
		Children: []BlackboardTaskSpec{
			{Title: "Implement authentication", Executor: domain.ExecutorAgent},
			{Title: "Run login tests", Executor: domain.ExecutorAgent},
		},
	}
	decomposition, err := service.DecomposeBlackboardTask(context.Background(), decomposeCommand)
	if err != nil {
		t.Fatalf("decompose root task: %v", err)
	}
	repeated, err := service.DecomposeBlackboardTask(context.Background(), decomposeCommand)
	if err != nil {
		t.Fatalf("repeat decomposition: %v", err)
	}
	if repeated.Parent.ID != decomposition.Parent.ID || repeated.Children[0].ID != decomposition.Children[0].ID {
		t.Fatalf("idempotent decomposition: first=%#v repeated=%#v", decomposition, repeated)
	}
	if decomposition.Parent.Status != domain.TaskStatusWaitingChildren || decomposition.Parent.ActiveClaimID != nil || decomposition.Parent.DecomposedAt == nil {
		t.Fatalf("aggregate root: %#v", decomposition.Parent)
	}
	if decomposition.Parent.Tags == nil || decomposition.Children[0].Tags == nil || decomposition.Children[0].Reviews == nil {
		t.Fatalf("decomposition contains nil collections: %#v", decomposition)
	}
	if claim := repository.claims[rootClaim.ID]; claim.Active() || claim.EndReason != domain.ClaimEndTaskDecomposed {
		t.Fatalf("decomposition claim: %#v", claim)
	}
	for _, child := range decomposition.Children {
		if child.ParentTaskID == nil || *child.ParentTaskID != root.ID {
			t.Fatalf("child parent: %#v", child)
		}
	}

	authentication := decomposition.Children[0]
	authenticationClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: authentication.ID, Identity: planner})
	if err != nil {
		t.Fatalf("claim authentication task: %v", err)
	}
	nested, err := service.DecomposeBlackboardTask(context.Background(), DecomposeBlackboardTaskCommand{
		TaskID: authentication.ID, ClaimID: authenticationClaim.ID, Identity: planner,
		Children: []BlackboardTaskSpec{
			{Title: "Implement password verification", Executor: domain.ExecutorAgent},
			{Title: "Implement session management", Executor: domain.ExecutorAgent},
		},
	})
	if err != nil {
		t.Fatalf("decompose authentication task: %v", err)
	}
	extra, err := service.AddBlackboardChildTask(context.Background(), AddBlackboardChildTaskCommand{
		ParentTaskID: authentication.ID,
		Identity:     contributor,
		Task:         BlackboardTaskSpec{Title: "Add brute force protection", Executor: domain.ExecutorAgent},
	})
	if err != nil {
		t.Fatalf("append child task: %v", err)
	}
	if extra.ParentTaskID == nil || *extra.ParentTaskID != authentication.ID {
		t.Fatalf("appended child: %#v", extra)
	}
	if extra.AllowedRoles == nil || extra.Tags == nil || extra.Reviews == nil || extra.Submissions == nil || extra.Failures == nil || extra.TransitionDecisions == nil {
		t.Fatalf("appended child contains nil collections: %#v", extra)
	}

	complete := func(task domain.Task) {
		t.Helper()
		claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: contributor})
		if err != nil {
			t.Fatalf("claim %q: %v", task.Title, err)
		}
		if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
			TaskID: task.ID, ClaimID: claim.ID, Identity: contributor, Result: "Completed " + task.Title,
		}); err != nil {
			t.Fatalf("complete %q: %v", task.Title, err)
		}
	}
	complete(decomposition.Children[1])
	for _, child := range nested.Children {
		complete(child)
	}
	if parent := repository.tasks[root.ID]; parent.Status != domain.TaskStatusWaitingChildren {
		t.Fatalf("root completed too early: %#v", parent)
	}
	complete(extra)

	completedNested := repository.tasks[authentication.ID]
	completedRoot := repository.tasks[root.ID]
	if completedNested.Status != domain.TaskStatusCompleted || completedRoot.Status != domain.TaskStatusCompleted {
		t.Fatalf("recursive completion: nested=%s root=%s", completedNested.Status, completedRoot.Status)
	}
	if len(completedNested.Submissions) != 0 || len(completedRoot.Submissions) != 0 {
		t.Fatalf("aggregate submissions: nested=%d root=%d", len(completedNested.Submissions), len(completedRoot.Submissions))
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusOpen {
		t.Fatalf("blackboard after aggregate closure: got %s, want open", got)
	}
	if _, err := service.SubmitBlackboardCompletion(context.Background(), SubmitBlackboardCompletionCommand{
		WorkItemID: workItem.ID, Identity: contributor, Result: "All aggregate children completed.",
	}); err != nil {
		t.Fatalf("submit aggregate completion: %v", err)
	}
	if _, err := service.AddBlackboardChildTask(context.Background(), AddBlackboardChildTaskCommand{
		ParentTaskID: root.ID, Identity: contributor,
		Task: BlackboardTaskSpec{Title: "Late child", Executor: domain.ExecutorAgent},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("append to completed aggregate: got %v", err)
	}
	if err := domain.ValidateBlackboardTaskHierarchy(workItem.ID, repository.tasksFor(workItem.ID)); err != nil {
		t.Fatalf("final task hierarchy: %v", err)
	}
}

func TestOperationIDReturnsOriginalResultAndRejectsReuse(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "planner"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   identity, Title: "Idempotency", Goal: "Avoid duplicate tasks",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	command := CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity, OperationID: "create-login-task",
		Title: "Implement login", Executor: domain.ExecutorAgent,
	}
	first, err := service.CreateBlackboardTask(context.Background(), command)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	second, err := service.CreateBlackboardTask(context.Background(), command)
	if err != nil {
		t.Fatalf("repeated request: %v", err)
	}
	if first.ID != second.ID || len(repository.tasksFor(workItem.ID)) != 1 {
		t.Fatalf("repeated result: first=%q second=%q tasks=%d", first.ID, second.ID, len(repository.tasksFor(workItem.ID)))
	}
	command.Title = "Implement another login"
	if _, err := service.CreateBlackboardTask(context.Background(), command); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused operation id: got %v", err)
	}
}

func TestFailTaskReopensAndPreservesFailure(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	identity := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "generalist"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   identity,
		Title:      "Diagnose deployment",
		Goal:       "Restore deployment",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   identity, Title: "Inspect logs", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: identity})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	failure, err := service.FailTask(context.Background(), FailTaskCommand{
		TaskID:      task.ID,
		ClaimID:     claim.ID,
		Identity:    identity,
		Action:      domain.TaskFailureReopen,
		Reason:      "Logs are unavailable",
		RetryPrompt: "Check the archive after replication catches up.",
	})
	if err != nil {
		t.Fatalf("fail task: %v", err)
	}
	reopened := repository.tasks[task.ID]
	if reopened.Status != domain.TaskStatusPending || len(reopened.Failures) != 1 || reopened.Failures[0].ID != failure.ID {
		t.Fatalf("reopened task: %#v", reopened)
	}
	retryClaim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: identity})
	if err != nil {
		t.Fatalf("reclaim task: %v", err)
	}
	retryContext, err := service.GetTaskExecutionContext(context.Background(), GetTaskExecutionContextQuery{TaskID: task.ID, Identity: identity})
	if err != nil {
		t.Fatalf("get retried task context: %v", err)
	}
	if retryContext.Blackboard == nil || retryContext.Blackboard.CanDecompose {
		t.Fatalf("task with failure history can decompose: %#v", retryContext.Blackboard)
	}
	if _, err := service.DecomposeBlackboardTask(context.Background(), DecomposeBlackboardTaskCommand{
		TaskID: task.ID, ClaimID: retryClaim.ID, Identity: identity,
		Children: []BlackboardTaskSpec{{Title: "Retry child", Executor: domain.ExecutorAgent}},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("decompose task with failure history: got %v", err)
	}
}

func TestWorkflowReviewGatesTransitionApplication(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := reviewedWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	reviewer := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}

	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Implement login", Goal: "Users can log in",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID:   start.ID,
		ClaimID:  claim.ID,
		Identity: agent,
		Result:   "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID: "exit:implement",
			Reason:        "Ready for testing",
		},
	}); err != nil {
		t.Fatalf("submit reviewed task: %v", err)
	}
	start = repository.tasks[start.ID]
	if start.Status != domain.TaskStatusInReview || len(start.TransitionDecisions) != 1 || start.TransitionDecisions[0].AppliedAt != nil {
		t.Fatalf("task awaiting review: %#v", start)
	}
	if got := len(repository.tasksFor(workItem.ID)); got != 1 {
		t.Fatalf("tasks before approval: got %d, want 1", got)
	}

	review := start.Reviews[len(start.Reviews)-1]
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: start.ID, ReviewID: review.ID, Identity: reviewer, Decision: domain.ReviewStatusApproved,
	}); err != nil {
		t.Fatalf("approve review: %v", err)
	}
	start = repository.tasks[start.ID]
	if start.Status != domain.TaskStatusCompleted || start.TransitionDecisions[0].AppliedAt == nil {
		t.Fatalf("approved task: %#v", start)
	}
	tasks := repository.tasksFor(workItem.ID)
	if len(tasks) != 2 || tasks[1].WorkflowTaskID == nil || *tasks[1].WorkflowTaskID != "test" {
		t.Fatalf("tasks after approval: %#v", tasks)
	}
}

func TestBlackboardReviewReopensTaskWithCompleteHistory(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	repository.blackboards[definitionKey("blackboard", 1)] = blackboardDefinition()
	service := newTestService(t, repository)
	firstAgent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-1"}, Role: "generalist"}
	secondAgent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent-2"}, Role: "generalist"}
	reviewer := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: "blackboard", Version: 1, Mode: domain.CoordinationModeBlackboard},
		Identity:   firstAgent, Title: "Investigate incident", Goal: "Identify the cause",
	})
	if err != nil {
		t.Fatalf("create blackboard: %v", err)
	}
	task, err := service.CreateBlackboardTask(context.Background(), CreateBlackboardTaskCommand{
		WorkItemID: workItem.ID,
		Identity:   firstAgent, Title: "Analyze traces", Executor: domain.ExecutorAgent,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: firstAgent})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: firstAgent, Result: "Initial diagnosis", RequestReview: true,
	}); err != nil {
		t.Fatalf("submit for review: %v", err)
	}
	task = repository.tasks[task.ID]
	review := task.Reviews[len(task.Reviews)-1]
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: task.ID, ReviewID: review.ID, Identity: reviewer,
		Decision: domain.ReviewStatusRejected, Feedback: "Include database traces.",
	}); err != nil {
		t.Fatalf("reject review: %v", err)
	}
	task = repository.tasks[task.ID]
	if task.Status != domain.TaskStatusPending || len(task.Submissions) != 1 || len(task.Reviews) != 1 {
		t.Fatalf("reopened task history: %#v", task)
	}

	claim, err = service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: secondAgent})
	if err != nil {
		t.Fatalf("reclaim task: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: secondAgent,
		Result: "Diagnosis with database traces", RequestReview: true,
	}); err != nil {
		t.Fatalf("submit revised task: %v", err)
	}
	task = repository.tasks[task.ID]
	review = task.Reviews[len(task.Reviews)-1]
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: task.ID, ReviewID: review.ID, Identity: reviewer, Decision: domain.ReviewStatusApproved,
	}); err != nil {
		t.Fatalf("approve revised task: %v", err)
	}
	task = repository.tasks[task.ID]
	if task.Status != domain.TaskStatusCompleted || len(task.Submissions) != 2 || len(task.Reviews) != 2 || task.Reviews[0].Feedback != "Include database traces." {
		t.Fatalf("completed revised task: %#v", task)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusOpen {
		t.Fatalf("blackboard after approved final task: got %s, want open", got)
	}
	if _, err := service.SubmitBlackboardCompletion(context.Background(), SubmitBlackboardCompletionCommand{
		WorkItemID: workItem.ID, Identity: secondAgent, Result: "Reviewed diagnosis completed.",
	}); err != nil {
		t.Fatalf("submit reviewed completion: %v", err)
	}
}

func TestWorkflowActivationJoinsParallelTasks(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := joiningWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}

	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Parallel delivery", Goal: "Deliver joined result",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	completeWorkflowTask(t, service, repository, agent, start, WorkflowTransitionCommand{
		ChoiceGroupID: "exit:start",
	})

	tasks := repository.tasksFor(workItem.ID)
	if len(tasks) != 3 {
		t.Fatalf("parallel tasks: got %d, want 3", len(tasks))
	}
	byDefinition := workflowTasksByDefinition(tasks)
	completeWorkflowTask(t, service, repository, agent, byDefinition["b"], WorkflowTransitionCommand{
		ChoiceGroupID: "exit:b",
	})
	if _, exists := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["d"]; exists {
		t.Fatal("join task was created before all inputs resolved")
	}

	completeWorkflowTask(t, service, repository, agent, byDefinition["c"], WorkflowTransitionCommand{
		ChoiceGroupID: "exit:c",
	})
	joined, exists := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["d"]
	if !exists {
		t.Fatal("join task was not created after all inputs resolved")
	}
	relations, err := repository.ListTaskRelations(workItem.ID)
	if err != nil {
		t.Fatalf("list relations: %v", err)
	}
	predecessors := 0
	for _, relation := range relations {
		if relation.ToTaskID == joined.ID {
			predecessors++
		}
	}
	if predecessors != 2 {
		t.Fatalf("join predecessors: got %d, want 2", predecessors)
	}
}

func TestWorkflowOptionalSkipRequiresConfiguredReview(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := optionalReviewWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	reviewer := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Optional documentation", Goal: "Finish delivery",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	completeWorkflowTask(t, service, repository, agent, start, WorkflowTransitionCommand{
		ChoiceGroupID:       "exit:implement",
		SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs"},
	})
	docs := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["docs"]
	if docs.Status != domain.TaskStatusInReview || len(docs.Reviews) != 1 || docs.Reviews[0].SubmissionID != nil {
		t.Fatalf("optional skip review: %#v", docs)
	}
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: docs.ID, ReviewID: docs.Reviews[0].ID, Identity: reviewer, Decision: domain.ReviewStatusApproved,
	}); err != nil {
		t.Fatalf("approve optional skip: %v", err)
	}
	docs = repository.tasks[docs.ID]
	if docs.Status != domain.TaskStatusSkipped || docs.CompletedAt == nil {
		t.Fatalf("approved optional skip: %#v", docs)
	}
	if got := repository.workItems[workItem.ID].Status; got != domain.WorkItemStatusCompleted {
		t.Fatalf("workflow status: got %q, want completed", got)
	}
}

func TestWorkflowSkipIntentCanRequestOptionalReview(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := optionalReviewWorkflowDefinition()
	definition.ID = "optional-requested-review"
	definition.Graph.Tasks[1].ReviewPolicy = domain.ReviewExecutorDecides
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Optional requested review", Goal: "Review a skip decision",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID:        "exit:implement",
			SkipOptionalTaskIDs:  []domain.WorkflowTaskID{"docs"},
			ReviewSkippedTaskIDs: []domain.WorkflowTaskID{"docs"},
		},
	}); err != nil {
		t.Fatalf("submit reviewed skip intent: %v", err)
	}
	docs := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["docs"]
	if docs.Status != domain.TaskStatusInReview || len(docs.Reviews) != 1 {
		t.Fatalf("requested skip review: %#v", docs)
	}
}

func TestWorkflowSkipIntentAdvancesConsecutiveOptionalTasks(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := consecutiveOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Deliver feature", Goal: "Reach integration testing",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	transition := &WorkflowTransitionCommand{
		ChoiceGroupID:       "exit:implement",
		SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs", "examples"},
		Reason:              "Documentation and examples are unnecessary for this internal change.",
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete", Transition: transition,
	}); err != nil {
		t.Fatalf("submit skip intent: %v", err)
	}

	byDefinition := workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	if byDefinition["docs"].Status != domain.TaskStatusSkipped || byDefinition["examples"].Status != domain.TaskStatusSkipped {
		t.Fatalf("skipped optional tasks: docs=%s examples=%s", byDefinition["docs"].Status, byDefinition["examples"].Status)
	}
	if byDefinition["docs"].SkippedBy == nil || *byDefinition["docs"].SkippedBy != agent.Actor || byDefinition["docs"].SkipReason != "Skipped by upstream intent" {
		t.Fatalf("workflow skip decision: %#v", byDefinition["docs"])
	}
	if byDefinition["integration"].Status != domain.TaskStatusPending {
		t.Fatalf("integration task: got %q, want pending", byDefinition["integration"].Status)
	}
	if len(byDefinition["docs"].TransitionDecisions) != 1 || byDefinition["docs"].TransitionDecisions[0].AppliedAt == nil {
		t.Fatalf("docs transition decision: %#v", byDefinition["docs"].TransitionDecisions)
	}
	if len(byDefinition["examples"].TransitionDecisions) != 1 || byDefinition["examples"].TransitionDecisions[0].AppliedAt == nil {
		t.Fatalf("examples transition decision: %#v", byDefinition["examples"].TransitionDecisions)
	}
}

func TestWorkflowSkipIntentWaitsForOptionalSkipReview(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := consecutiveOptionalWorkflowDefinition()
	definition.ID = "reviewed-skip-intent"
	definition.Name = "Reviewed skip intent"
	definition.Graph.Tasks[1].ReviewPolicy = domain.ReviewRequired
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	reviewer := Identity{Actor: domain.ActorRef{Kind: domain.ActorHuman, ID: "reviewer"}}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Reviewed optional skip", Goal: "Reach examples",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID:       "exit:implement",
			SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs"},
		},
	}); err != nil {
		t.Fatalf("submit skip intent: %v", err)
	}
	byDefinition := workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	docs := byDefinition["docs"]
	if docs.Status != domain.TaskStatusInReview || len(docs.TransitionDecisions) != 1 || docs.TransitionDecisions[0].AppliedAt != nil {
		t.Fatalf("reviewed planned skip: %#v", docs)
	}
	if _, exists := byDefinition["examples"]; exists {
		t.Fatal("planned successor was created before skip review approval")
	}
	if _, err := service.DecideReview(context.Background(), DecideReviewCommand{
		TaskID: docs.ID, ReviewID: docs.Reviews[0].ID, Identity: reviewer, Decision: domain.ReviewStatusApproved,
	}); err != nil {
		t.Fatalf("approve planned skip: %v", err)
	}
	byDefinition = workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	if byDefinition["examples"].Status != domain.TaskStatusPending {
		t.Fatalf("planned successor after review: got %q", byDefinition["examples"].Status)
	}
}

func TestWorkflowSkipIntentRejectsTaskBehindKeptOptionalTask(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := consecutiveOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Incomplete plan", Goal: "Reject incomplete skip planning",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	_, err = service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete",
		Transition: &WorkflowTransitionCommand{
			ChoiceGroupID:       "exit:implement",
			SkipOptionalTaskIDs: []domain.WorkflowTaskID{"examples"},
		},
	})
	if err == nil {
		t.Fatal("submit unreachable skip intent: got nil error")
	}
}

func TestWorkflowSkipIntentAdvancesParallelOptionalBranches(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := branchingOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Parallel optional branches", Goal: "Reach both publishers",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	transition := &WorkflowTransitionCommand{
		ChoiceGroupID:       "exit:implement",
		SkipOptionalTaskIDs: []domain.WorkflowTaskID{"docs", "examples"},
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Implementation complete", Transition: transition,
	}); err != nil {
		t.Fatalf("submit branching plan: %v", err)
	}
	byDefinition := workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	if byDefinition["docs"].Status != domain.TaskStatusSkipped || byDefinition["examples"].Status != domain.TaskStatusSkipped {
		t.Fatalf("optional branch status: docs=%s examples=%s", byDefinition["docs"].Status, byDefinition["examples"].Status)
	}
	if byDefinition["publish-docs"].Status != domain.TaskStatusPending || byDefinition["publish-examples"].Status != domain.TaskStatusPending {
		t.Fatalf(
			"published branch status: docs=%s examples=%s",
			byDefinition["publish-docs"].Status,
			byDefinition["publish-examples"].Status,
		)
	}
}

func TestWorkflowSkipIntentJoinsOptionalBranchesIntoOneTask(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := joiningOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Joined optional branches", Goal: "Reach final verification",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: start.ID, Identity: agent})
	if err != nil {
		t.Fatalf("claim start: %v", err)
	}
	transition := &WorkflowTransitionCommand{
		ChoiceGroupID:       "exit:start",
		SkipOptionalTaskIDs: []domain.WorkflowTaskID{"b", "c", "d"},
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: start.ID, ClaimID: claim.ID, Identity: agent, Result: "Planning complete", Transition: transition,
	}); err != nil {
		t.Fatalf("submit joining plan: %v", err)
	}

	tasks := repository.tasksFor(workItem.ID)
	counts := make(map[domain.WorkflowTaskID]int)
	for _, task := range tasks {
		if task.WorkflowTaskID != nil {
			counts[*task.WorkflowTaskID]++
		}
	}
	for _, taskID := range []domain.WorkflowTaskID{"start", "b", "c", "d", "final"} {
		if counts[taskID] != 1 {
			t.Fatalf("runtime task %q count: got %d, want 1", taskID, counts[taskID])
		}
	}
	byDefinition := workflowTasksByDefinition(tasks)
	if byDefinition["b"].Status != domain.TaskStatusSkipped || byDefinition["c"].Status != domain.TaskStatusSkipped || byDefinition["d"].Status != domain.TaskStatusSkipped {
		t.Fatalf("joined skip states: b=%s c=%s d=%s", byDefinition["b"].Status, byDefinition["c"].Status, byDefinition["d"].Status)
	}
	if byDefinition["final"].Status != domain.TaskStatusPending {
		t.Fatalf("final task status: got %s, want pending", byDefinition["final"].Status)
	}
}

func TestWorkflowSkipIntentFanInKeepsTaskWhenOnePredecessorKeeps(t *testing.T) {
	t.Parallel()

	repository := newTestRepository()
	definition := joiningOptionalWorkflowDefinition()
	repository.workflows[definitionKey(definition.ID, definition.Version)] = definition
	service := newTestService(t, repository)
	agent := Identity{Actor: domain.ActorRef{Kind: domain.ActorAgent, ID: "agent"}, Role: "backend"}
	workItem, err := service.CreateWorkItem(context.Background(), CreateWorkItemCommand{
		Definition: definition.Binding(), Identity: agent, Title: "Keep joined task", Goal: "Preserve one requested task",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	start := repository.tasksFor(workItem.ID)[0]
	completeWorkflowTask(t, service, repository, agent, start, WorkflowTransitionCommand{ChoiceGroupID: "exit:start"})
	byDefinition := workflowTasksByDefinition(repository.tasksFor(workItem.ID))
	completeWorkflowTask(t, service, repository, agent, byDefinition["b"], WorkflowTransitionCommand{
		ChoiceGroupID: "exit:b", SkipOptionalTaskIDs: []domain.WorkflowTaskID{"d"},
	})
	if _, exists := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["d"]; exists {
		t.Fatal("joined task was created before all predecessors decided")
	}
	completeWorkflowTask(t, service, repository, agent, byDefinition["c"], WorkflowTransitionCommand{ChoiceGroupID: "exit:c"})
	d := workflowTasksByDefinition(repository.tasksFor(workItem.ID))["d"]
	if d.Status != domain.TaskStatusPending {
		t.Fatalf("joined task status: got %s, want pending", d.Status)
	}
}

func completeWorkflowTask(
	t *testing.T,
	service *Service,
	repository *testRepository,
	identity Identity,
	task domain.Task,
	transition WorkflowTransitionCommand,
) {
	t.Helper()
	claim, err := service.ClaimTask(context.Background(), ClaimTaskCommand{TaskID: task.ID, Identity: identity})
	if err != nil {
		t.Fatalf("claim task %s: %v", task.ID, err)
	}
	if _, err := service.SubmitTask(context.Background(), SubmitTaskCommand{
		TaskID: task.ID, ClaimID: claim.ID, Identity: identity, Result: "Completed " + task.Title,
		Transition: &transition,
	}); err != nil {
		t.Fatalf("submit task %s: %v", task.ID, err)
	}
}

func workflowTasksByDefinition(tasks []domain.Task) map[domain.WorkflowTaskID]domain.Task {
	result := make(map[domain.WorkflowTaskID]domain.Task, len(tasks))
	for _, task := range tasks {
		if task.WorkflowTaskID != nil {
			result[*task.WorkflowTaskID] = task
		}
	}
	return result
}

func workflowDefinitionTaskIDs(tasks []domain.WorkflowTaskDefinition) []domain.WorkflowTaskID {
	result := make([]domain.WorkflowTaskID, len(tasks))
	for index, task := range tasks {
		result[index] = task.ID
	}
	return result
}

func runtimeWorkflowTaskIDs(tasks []domain.Task) []domain.WorkflowTaskID {
	result := make([]domain.WorkflowTaskID, 0, len(tasks))
	for _, task := range tasks {
		if task.WorkflowTaskID != nil {
			result = append(result, *task.WorkflowTaskID)
		}
	}
	return result
}

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

type testIDs struct{ next int }

func (g *testIDs) NewID() string {
	g.next++
	return fmt.Sprintf("generated-%d", g.next)
}

func newTestService(t *testing.T, repository Repository) *Service {
	t.Helper()
	service, err := NewService(repository, testClock{now: applicationTestTime}, &testIDs{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func workflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "workflow", Version: 1, Name: "Development",
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{
					ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent,
					AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired,
					ReviewPolicy: domain.ReviewNone, DefaultTags: []string{"backend"},
				},
			},
		},
	}
}

func reviewedWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "reviewed-workflow", Version: 1, Name: "Reviewed development",
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewRequired},
				{ID: "test", Title: "Test", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-test", FromTaskID: "implement", ToTaskID: "test"},
			},
		},
	}
}

func joiningWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "joining-workflow", Version: 1, Name: "Joining workflow",
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"start"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "start", Title: "Start", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "b", Title: "B", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "c", Title: "C", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "d", Title: "D", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "start-b", FromTaskID: "start", ToTaskID: "b"},
				{ID: "start-c", FromTaskID: "start", ToTaskID: "c"},
				{ID: "b-d", FromTaskID: "b", ToTaskID: "d"},
				{ID: "c-d", FromTaskID: "c", ToTaskID: "d"},
			},
		},
	}
}

func optionalReviewWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "optional-review-workflow", Version: 1, Name: "Optional review",
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "docs", Title: "Documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewRequired},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs"},
			},
		},
	}
}

func consecutiveOptionalWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "consecutive-optional-workflow", Version: 1, Name: "Consecutive optional tasks",
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "docs", Title: "Documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "examples", Title: "Examples", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "integration", Title: "Integration test", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs", Label: "Documentation needed", AgentGuidance: "Keep documentation when the change affects users."},
				{ID: "docs-examples", FromTaskID: "docs", ToTaskID: "examples"},
				{ID: "examples-integration", FromTaskID: "examples", ToTaskID: "integration"},
			},
		},
	}
}

func branchingOptionalWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "branching-optional-workflow", Version: 1, Name: "Branching optional tasks",
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"implement"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "implement", Title: "Implement", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "docs", Title: "Documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "examples", Title: "Examples", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "publish-docs", Title: "Publish documentation", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "publish-examples", Title: "Publish examples", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "implement-docs", FromTaskID: "implement", ToTaskID: "docs"},
				{ID: "implement-examples", FromTaskID: "implement", ToTaskID: "examples"},
				{ID: "docs-publish", FromTaskID: "docs", ToTaskID: "publish-docs"},
				{ID: "examples-publish", FromTaskID: "examples", ToTaskID: "publish-examples"},
			},
		},
	}
}

func joiningOptionalWorkflowDefinition() domain.WorkflowDefinition {
	return domain.WorkflowDefinition{
		DefinitionMetadata: domain.DefinitionMetadata{
			ID: "joining-optional-workflow", Version: 1, Name: "Joining optional workflow",
			CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
		},
		Graph: domain.WorkflowGraph{
			StartTaskIDs: []domain.WorkflowTaskID{"start"},
			Tasks: []domain.WorkflowTaskDefinition{
				{ID: "start", Title: "Start", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
				{ID: "b", Title: "Optional B", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "c", Title: "Optional C", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "d", Title: "Joined optional D", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionOptional, ReviewPolicy: domain.ReviewNone},
				{ID: "final", Title: "Final verification", Executor: domain.ExecutorAgent, AllowedRoles: []string{"backend"}, Execution: domain.ExecutionRequired, ReviewPolicy: domain.ReviewNone},
			},
			Relations: []domain.WorkflowRelationDefinition{
				{ID: "start-b", FromTaskID: "start", ToTaskID: "b"},
				{ID: "start-c", FromTaskID: "start", ToTaskID: "c"},
				{ID: "b-d", FromTaskID: "b", ToTaskID: "d"},
				{ID: "c-d", FromTaskID: "c", ToTaskID: "d"},
				{ID: "d-final", FromTaskID: "d", ToTaskID: "final"},
			},
		},
	}
}

func blackboardDefinition() domain.BlackboardDefinition {
	return domain.BlackboardDefinition{DefinitionMetadata: domain.DefinitionMetadata{
		ID: "blackboard", Version: 1, Name: "Open collaboration",
		CreatedAt: applicationTestTime, UpdatedAt: applicationTestTime,
	}}
}

func definitionKey(id domain.DefinitionID, version int64) string {
	return fmt.Sprintf("%s:%d", id, version)
}

type testRepository struct {
	workItems              map[domain.WorkItemID]domain.WorkItem
	tasks                  map[domain.TaskID]domain.Task
	relations              []domain.TaskRelation
	claims                 map[domain.ClaimID]domain.Claim
	artifacts              map[domain.ArtifactID]domain.Artifact
	blobs                  map[string]domain.ArtifactBlob
	activations            map[domain.WorkflowTaskActivationID]domain.WorkflowTaskActivation
	events                 []domain.WorkItemEvent
	workflows              map[string]domain.WorkflowDefinition
	blackboards            map[string]domain.BlackboardDefinition
	idempotency            map[string]IdempotencyRecord
	deleteIdempotencyError error
}

func newTestRepository() *testRepository {
	return &testRepository{
		workItems:   make(map[domain.WorkItemID]domain.WorkItem),
		tasks:       make(map[domain.TaskID]domain.Task),
		claims:      make(map[domain.ClaimID]domain.Claim),
		artifacts:   make(map[domain.ArtifactID]domain.Artifact),
		blobs:       make(map[string]domain.ArtifactBlob),
		activations: make(map[domain.WorkflowTaskActivationID]domain.WorkflowTaskActivation),
		workflows:   make(map[string]domain.WorkflowDefinition),
		blackboards: make(map[string]domain.BlackboardDefinition),
		idempotency: make(map[string]IdempotencyRecord),
	}
}

func (r *testRepository) View(_ context.Context, operation func(ReadStore) error) error {
	return operation(r)
}

func (r *testRepository) Update(_ context.Context, operation func(WriteStore) error) error {
	return operation(r)
}

func (r *testRepository) GetWorkItem(id domain.WorkItemID) (domain.WorkItem, error) {
	value, ok := r.workItems[id]
	if !ok {
		return domain.WorkItem{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListWorkItems(filter WorkItemFilter) ([]domain.WorkItem, error) {
	result := make([]domain.WorkItem, 0, len(r.workItems))
	for _, workItem := range r.workItems {
		if len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, workItem.Status) {
			continue
		}
		if len(filter.Modes) > 0 && !slices.Contains(filter.Modes, workItem.CoordinationMode()) {
			continue
		}
		if !containsAll(workItem.Tags, filter.Tags) {
			continue
		}
		result = append(result, workItem)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	if filter.Page.After != nil {
		result = slices.DeleteFunc(result, func(item domain.WorkItem) bool {
			return item.UpdatedAt.After(filter.Page.After.UpdatedAt) ||
				(item.UpdatedAt.Equal(filter.Page.After.UpdatedAt) && item.ID <= filter.Page.After.ID)
		})
	}
	if filter.Page.Limit > 0 && len(result) > filter.Page.Limit+1 {
		result = result[:filter.Page.Limit+1]
	}
	return result, nil
}

func (r *testRepository) GetTask(id domain.TaskID) (domain.Task, error) {
	value, ok := r.tasks[id]
	if !ok {
		return domain.Task{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListTasks(workItemID domain.WorkItemID) ([]domain.Task, error) {
	return r.tasksFor(workItemID), nil
}

func (r *testRepository) ListHumanAttention(page PageRequest[HumanAttentionCursor]) ([]HumanAttentionItem, error) {
	result := make([]HumanAttentionItem, 0)
	for _, workItem := range r.workItems {
		if workItem.Status == domain.WorkItemStatusAwaitingHumanAcceptance {
			result = append(result, HumanAttentionItem{Kind: HumanAttentionAcceptance, WorkItem: workItem})
			continue
		}
		if workItem.Status != domain.WorkItemStatusOpen {
			continue
		}
		for _, task := range r.tasksFor(workItem.ID) {
			kind := HumanAttentionKind("")
			if task.Status == domain.TaskStatusInReview {
				kind = HumanAttentionReview
			} else if task.Status == domain.TaskStatusPending && task.ActiveClaimID == nil && task.Executor == domain.ExecutorHuman {
				kind = HumanAttentionTask
			}
			if kind != "" {
				taskCopy := task
				result = append(result, HumanAttentionItem{Kind: kind, WorkItem: workItem, Task: &taskCopy})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return humanAttentionCursorLess(result[i].Cursor(), result[j].Cursor())
	})
	if page.After != nil {
		result = slices.DeleteFunc(result, func(item HumanAttentionItem) bool {
			return !humanAttentionCursorLess(*page.After, item.Cursor())
		})
	}
	if page.Limit > 0 && len(result) > page.Limit+1 {
		result = result[:page.Limit+1]
	}
	return result, nil
}

func humanAttentionCursorLess(left, right HumanAttentionCursor) bool {
	if left.Priority != right.Priority {
		return left.Priority < right.Priority
	}
	if !left.UpdatedAt.Equal(right.UpdatedAt) {
		return left.UpdatedAt.After(right.UpdatedAt)
	}
	if left.WorkItemID != right.WorkItemID {
		return left.WorkItemID < right.WorkItemID
	}
	return left.TaskID < right.TaskID
}

func (r *testRepository) tasksFor(workItemID domain.WorkItemID) []domain.Task {
	var result []domain.Task
	for _, task := range r.tasks {
		if task.WorkItemID == workItemID {
			result = append(result, task)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Position < result[j].Position })
	return result
}

func (r *testRepository) ListTaskRelations(workItemID domain.WorkItemID) ([]domain.TaskRelation, error) {
	var result []domain.TaskRelation
	for _, relation := range r.relations {
		if relation.WorkItemID == workItemID {
			result = append(result, relation)
		}
	}
	return result, nil
}

func (r *testRepository) ListClaims(taskID domain.TaskID) ([]domain.Claim, error) {
	var result []domain.Claim
	for _, claim := range r.claims {
		if claim.TaskID == taskID {
			result = append(result, claim)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ClaimedAt.Before(result[j].ClaimedAt) })
	return result, nil
}

func (r *testRepository) GetArtifact(id domain.ArtifactID) (domain.Artifact, error) {
	value, ok := r.artifacts[id]
	if !ok {
		return domain.Artifact{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListArtifacts(filter ArtifactFilter) ([]domain.Artifact, error) {
	result := make([]domain.Artifact, 0)
	for _, artifact := range r.artifacts {
		if artifact.WorkItemID == filter.WorkItemID &&
			(filter.TaskID == "" || artifact.TaskID == filter.TaskID) &&
			(!filter.SubmittedOnly || artifact.SubmissionID != nil) {
			result = append(result, artifact)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	if filter.Page.After != nil {
		result = slices.DeleteFunc(result, func(item domain.Artifact) bool {
			return item.CreatedAt.Before(filter.Page.After.CreatedAt) ||
				(item.CreatedAt.Equal(filter.Page.After.CreatedAt) && item.ID <= filter.Page.After.ID)
		})
	}
	if filter.Page.Limit > 0 && len(result) > filter.Page.Limit+1 {
		result = result[:filter.Page.Limit+1]
	}
	return result, nil
}

func (r *testRepository) GetArtifactBlob(uri string) (domain.ArtifactBlob, error) {
	value, ok := r.blobs[uri]
	if !ok {
		return domain.ArtifactBlob{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListArtifactGarbage(before time.Time) ([]domain.Artifact, error) {
	result := make([]domain.Artifact, 0)
	for _, artifact := range r.artifacts {
		claim, exists := r.claims[artifact.ClaimID]
		if artifact.SubmissionID == nil && exists && !claim.Active() && !artifact.CreatedAt.After(before) {
			result = append(result, artifact)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (r *testRepository) ListUnreferencedArtifactBlobs(before time.Time) ([]domain.ArtifactBlob, error) {
	result := make([]domain.ArtifactBlob, 0)
	for _, blob := range r.blobs {
		if blob.CreatedAt.After(before) {
			continue
		}
		referenced, _ := r.ArtifactBlobReferenced(blob.URI)
		if !referenced {
			result = append(result, blob)
		}
	}
	return result, nil
}

func (r *testRepository) ArtifactBlobReferenced(uri string) (bool, error) {
	for _, artifact := range r.artifacts {
		if artifact.URI == uri {
			return true, nil
		}
	}
	return false, nil
}

func (r *testRepository) GetWorkflowTaskActivation(id domain.WorkflowTaskActivationID) (domain.WorkflowTaskActivation, error) {
	value, ok := r.activations[id]
	if !ok {
		return domain.WorkflowTaskActivation{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListWorkflowTaskActivations(workItemID domain.WorkItemID) ([]domain.WorkflowTaskActivation, error) {
	var result []domain.WorkflowTaskActivation
	for _, activation := range r.activations {
		if activation.WorkItemID == workItemID {
			result = append(result, activation)
		}
	}
	return result, nil
}

func (r *testRepository) ListOpenTasks(filter OpenTaskFilter) ([]WorkCandidate, error) {
	var result []WorkCandidate
	for _, task := range r.tasks {
		workItem := r.workItems[task.WorkItemID]
		identity := Identity{Actor: domain.ActorRef{Kind: filter.ActorKind, ID: "repository-filter"}, Role: filter.Role}
		if workItem.Status == domain.WorkItemStatusOpen &&
			identityCanExecute(identity, task) == nil &&
			(workItem.CoordinationMode() == domain.CoordinationModeWorkflow || containsAll(task.Tags, filter.Tags)) {
			result = append(result, WorkCandidate{Kind: WorkCandidateTask, WorkItem: workItem, Task: &task})
		}
	}
	return result, nil
}

func (r *testRepository) ListEmptyBlackboards(tags []string) ([]domain.WorkItem, error) {
	var result []domain.WorkItem
	for _, workItem := range r.workItems {
		if workItem.Status == domain.WorkItemStatusOpen &&
			workItem.CoordinationMode() == domain.CoordinationModeBlackboard &&
			containsAll(workItem.Tags, tags) &&
			len(r.tasksFor(workItem.ID)) == 0 {
			result = append(result, workItem)
		}
	}
	return result, nil
}

func (r *testRepository) ListBlackboardsAwaitingLifecycleDecision(tags []string) ([]domain.WorkItem, error) {
	var result []domain.WorkItem
	for _, workItem := range r.workItems {
		if workItem.CoordinationMode() != domain.CoordinationModeBlackboard {
			continue
		}
		if !containsAll(workItem.Tags, tags) {
			continue
		}
		tasks := r.tasksFor(workItem.ID)
		if workItem.Status == domain.WorkItemStatusAwaitingAgentAcceptance ||
			(workItem.Status == domain.WorkItemStatusOpen && len(tasks) > 0 && blackboardTasksConverged(tasks)) {
			result = append(result, workItem)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (r *testRepository) ListReapableAgentClaimTasks(now time.Time) ([]domain.TaskID, error) {
	result := make([]domain.TaskID, 0)
	for _, task := range r.tasks {
		if task.Status != domain.TaskStatusWorking || task.ActiveClaimID == nil {
			continue
		}
		workItem := r.workItems[task.WorkItemID]
		if workItem.Status != domain.WorkItemStatusOpen {
			continue
		}
		claim, exists := r.claims[*task.ActiveClaimID]
		if !exists || claim.Executor.Kind != domain.ActorAgent || !claim.Active() || claim.LeaseUntil.IsZero() || now.Before(claim.LeaseUntil) {
			continue
		}
		result = append(result, task.ID)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func (r *testRepository) GetWorkflowDefinition(id domain.DefinitionID, version int64) (domain.WorkflowDefinition, error) {
	value, ok := r.workflows[definitionKey(id, version)]
	if !ok {
		return domain.WorkflowDefinition{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) GetDefinitionMetadata(bindings []domain.DefinitionBinding) (map[domain.DefinitionBinding]domain.DefinitionMetadata, error) {
	result := make(map[domain.DefinitionBinding]domain.DefinitionMetadata, len(bindings))
	for _, binding := range bindings {
		switch binding.Mode {
		case domain.CoordinationModeWorkflow:
			definition, ok := r.workflows[definitionKey(binding.ID, binding.Version)]
			if ok {
				result[binding] = definition.DefinitionMetadata
			}
		case domain.CoordinationModeBlackboard:
			definition, ok := r.blackboards[definitionKey(binding.ID, binding.Version)]
			if ok {
				result[binding] = definition.DefinitionMetadata
			}
		}
	}
	return result, nil
}

func (r *testRepository) GetBlackboardDefinition(id domain.DefinitionID, version int64) (domain.BlackboardDefinition, error) {
	value, ok := r.blackboards[definitionKey(id, version)]
	if !ok {
		return domain.BlackboardDefinition{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) GetLatestWorkflowDefinition(id domain.DefinitionID) (domain.WorkflowDefinition, error) {
	return latestTestDefinition(r.workflows, id)
}

func (r *testRepository) ListWorkflowDefinitionCatalog(filter DefinitionCatalogFilter) ([]domain.WorkflowDefinition, error) {
	result := make([]domain.WorkflowDefinition, 0, len(r.workflows))
	for _, definition := range r.workflows {
		result = append(result, definition)
	}
	result = latestTestDefinitions(result)
	if filter.Page.After != nil {
		result = slices.DeleteFunc(result, func(item domain.WorkflowDefinition) bool {
			return item.ID <= filter.Page.After.ID
		})
	}
	if filter.Page.Limit > 0 && len(result) > filter.Page.Limit+1 {
		result = result[:filter.Page.Limit+1]
	}
	return result, nil
}

func (r *testRepository) ListWorkflowDefinitionVersions(filter DefinitionVersionFilter) ([]domain.WorkflowDefinition, error) {
	result := make([]domain.WorkflowDefinition, 0, len(r.workflows))
	for _, definition := range r.workflows {
		if definition.ID == filter.ID {
			result = append(result, definition)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version > result[j].Version })
	if filter.Page.After != nil {
		result = slices.DeleteFunc(result, func(item domain.WorkflowDefinition) bool {
			return item.Version >= filter.Page.After.Version
		})
	}
	if filter.Page.Limit > 0 && len(result) > filter.Page.Limit+1 {
		result = result[:filter.Page.Limit+1]
	}
	return result, nil
}

func (r *testRepository) GetLatestBlackboardDefinition(id domain.DefinitionID) (domain.BlackboardDefinition, error) {
	return latestTestDefinition(r.blackboards, id)
}

func (r *testRepository) ListBlackboardDefinitionCatalog(filter DefinitionCatalogFilter) ([]domain.BlackboardDefinition, error) {
	result := make([]domain.BlackboardDefinition, 0, len(r.blackboards))
	for _, definition := range r.blackboards {
		result = append(result, definition)
	}
	result = latestTestDefinitions(result)
	if filter.Page.After != nil {
		result = slices.DeleteFunc(result, func(item domain.BlackboardDefinition) bool {
			return item.ID <= filter.Page.After.ID
		})
	}
	if filter.Page.Limit > 0 && len(result) > filter.Page.Limit+1 {
		result = result[:filter.Page.Limit+1]
	}
	return result, nil
}

func (r *testRepository) ListBlackboardDefinitionVersions(filter DefinitionVersionFilter) ([]domain.BlackboardDefinition, error) {
	result := make([]domain.BlackboardDefinition, 0, len(r.blackboards))
	for _, definition := range r.blackboards {
		if definition.ID == filter.ID {
			result = append(result, definition)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version > result[j].Version })
	if filter.Page.After != nil {
		result = slices.DeleteFunc(result, func(item domain.BlackboardDefinition) bool {
			return item.Version >= filter.Page.After.Version
		})
	}
	if filter.Page.Limit > 0 && len(result) > filter.Page.Limit+1 {
		result = result[:filter.Page.Limit+1]
	}
	return result, nil
}

type testDefinition interface {
	domain.WorkflowDefinition | domain.BlackboardDefinition
}

func latestTestDefinition[T testDefinition](definitions map[string]T, id domain.DefinitionID) (T, error) {
	var result T
	found := false
	for _, definition := range definitions {
		metadata := definitionMetadataForTest(definition)
		if metadata.ID == id && (!found || metadata.Version > definitionMetadataForTest(result).Version) {
			result = definition
			found = true
		}
	}
	if !found {
		return result, ErrNotFound
	}
	return result, nil
}

func latestTestDefinitions[T testDefinition](definitions []T) []T {
	latest := make(map[domain.DefinitionID]T)
	for _, definition := range definitions {
		metadata := definitionMetadataForTest(definition)
		current, exists := latest[metadata.ID]
		if !exists || metadata.Version > definitionMetadataForTest(current).Version {
			latest[metadata.ID] = definition
		}
	}
	result := make([]T, 0, len(latest))
	for _, definition := range latest {
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool {
		return definitionMetadataForTest(result[i]).ID < definitionMetadataForTest(result[j]).ID
	})
	return result
}

func definitionMetadataForTest[T testDefinition](definition T) domain.DefinitionMetadata {
	switch value := any(definition).(type) {
	case domain.WorkflowDefinition:
		return value.DefinitionMetadata
	case domain.BlackboardDefinition:
		return value.DefinitionMetadata
	default:
		return domain.DefinitionMetadata{}
	}
}

func (r *testRepository) LastWorkItemEventSequence(workItemID domain.WorkItemID) (int64, error) {
	var sequence int64
	for _, event := range r.events {
		if event.WorkItemID == workItemID && event.Sequence > sequence {
			sequence = event.Sequence
		}
	}
	return sequence, nil
}

func (r *testRepository) GetIdempotencyRecord(actor domain.ActorRef, operationID string) (IdempotencyRecord, error) {
	value, ok := r.idempotency[idempotencyTestKey(actor, operationID)]
	if !ok {
		return IdempotencyRecord{}, ErrNotFound
	}
	return value, nil
}

func (r *testRepository) ListPendingIdempotencyRecords(before time.Time) ([]IdempotencyRecord, error) {
	result := make([]IdempotencyRecord, 0)
	for _, record := range r.idempotency {
		if record.Status == IdempotencyPending && !record.CreatedAt.After(before) {
			result = append(result, record)
		}
	}
	return result, nil
}

func (r *testRepository) CreateWorkflowDefinition(value domain.WorkflowDefinition) error {
	key := definitionKey(value.ID, value.Version)
	if _, exists := r.workflows[key]; exists {
		return ErrConflict
	}
	r.workflows[key] = value
	return nil
}

func (r *testRepository) CreateBlackboardDefinition(value domain.BlackboardDefinition) error {
	key := definitionKey(value.ID, value.Version)
	if _, exists := r.blackboards[key]; exists {
		return ErrConflict
	}
	r.blackboards[key] = value
	return nil
}

func (r *testRepository) CreateWorkItem(value domain.WorkItem) error {
	if _, exists := r.workItems[value.ID]; exists {
		return ErrConflict
	}
	r.workItems[value.ID] = value
	return nil
}

func (r *testRepository) SaveWorkItem(value domain.WorkItem) error {
	if _, exists := r.workItems[value.ID]; !exists {
		return ErrNotFound
	}
	r.workItems[value.ID] = value
	return nil
}

func (r *testRepository) CreateTask(value domain.Task) error {
	if _, exists := r.tasks[value.ID]; exists {
		return ErrConflict
	}
	r.tasks[value.ID] = value
	return nil
}

func (r *testRepository) SaveTask(value domain.Task) error {
	if _, exists := r.tasks[value.ID]; !exists {
		return ErrNotFound
	}
	r.tasks[value.ID] = value
	return nil
}

func (r *testRepository) CreateTaskRelation(value domain.TaskRelation) error {
	r.relations = append(r.relations, value)
	return nil
}

func (r *testRepository) CreateWorkflowTaskActivation(value domain.WorkflowTaskActivation) error {
	if _, exists := r.activations[value.ID]; exists {
		return ErrConflict
	}
	r.activations[value.ID] = value
	return nil
}

func (r *testRepository) SaveWorkflowTaskActivation(value domain.WorkflowTaskActivation) error {
	if _, exists := r.activations[value.ID]; !exists {
		return ErrNotFound
	}
	r.activations[value.ID] = value
	return nil
}

func (r *testRepository) CreateClaim(value domain.Claim) error {
	if _, exists := r.claims[value.ID]; exists {
		return ErrConflict
	}
	r.claims[value.ID] = value
	return nil
}

func (r *testRepository) SaveClaim(value domain.Claim) error {
	if _, exists := r.claims[value.ID]; !exists {
		return ErrNotFound
	}
	r.claims[value.ID] = value
	return nil
}

func (r *testRepository) CreateArtifact(value domain.Artifact) error {
	if _, exists := r.artifacts[value.ID]; exists {
		return ErrConflict
	}
	r.artifacts[value.ID] = value
	return nil
}

func (r *testRepository) SaveArtifact(value domain.Artifact, _ time.Time) error {
	if _, exists := r.artifacts[value.ID]; !exists {
		return ErrNotFound
	}
	r.artifacts[value.ID] = value
	return nil
}

func (r *testRepository) DeleteArtifact(id domain.ArtifactID) error {
	value, exists := r.artifacts[id]
	if !exists {
		return ErrNotFound
	}
	if value.SubmissionID != nil {
		return ErrConflict
	}
	delete(r.artifacts, id)
	return nil
}

func (r *testRepository) CreateArtifactBlob(value domain.ArtifactBlob) error {
	if existing, exists := r.blobs[value.URI]; exists && (existing.Digest != value.Digest || existing.Size != value.Size) {
		return ErrConflict
	}
	r.blobs[value.URI] = value
	return nil
}

func (r *testRepository) DeleteArtifactBlob(uri string) error {
	if _, exists := r.blobs[uri]; !exists {
		return ErrNotFound
	}
	referenced, _ := r.ArtifactBlobReferenced(uri)
	if referenced {
		return ErrConflict
	}
	delete(r.blobs, uri)
	return nil
}

func (r *testRepository) AppendWorkItemEvent(value domain.WorkItemEvent) error {
	r.events = append(r.events, value)
	return nil
}

func (r *testRepository) LockDefinitionVersion(domain.CoordinationMode, domain.DefinitionID) error {
	return nil
}

func (r *testRepository) LockIdempotencyKey(domain.ActorRef, string) error { return nil }

func (r *testRepository) CreateIdempotencyRecord(value IdempotencyRecord) error {
	key := idempotencyTestKey(value.Actor, value.OperationID)
	if _, exists := r.idempotency[key]; exists {
		return ErrConflict
	}
	r.idempotency[key] = value
	return nil
}

func (r *testRepository) SaveIdempotencyRecord(value IdempotencyRecord, _ time.Time) error {
	key := idempotencyTestKey(value.Actor, value.OperationID)
	if _, exists := r.idempotency[key]; !exists {
		return ErrNotFound
	}
	r.idempotency[key] = value
	return nil
}

func (r *testRepository) DeleteIdempotencyRecord(actor domain.ActorRef, operationID string) error {
	if r.deleteIdempotencyError != nil {
		return r.deleteIdempotencyError
	}
	key := idempotencyTestKey(actor, operationID)
	record, exists := r.idempotency[key]
	if !exists || record.Status != IdempotencyPending {
		return ErrNotFound
	}
	delete(r.idempotency, key)
	return nil
}

func idempotencyTestKey(actor domain.ActorRef, operationID string) string {
	return string(actor.Kind) + ":" + string(actor.ID) + ":" + operationID
}
