package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
)

type sqlStore struct {
	ctx      context.Context
	tx       *sql.Tx
	dialect  dialect
	writable bool
}

func (s *sqlStore) exec(query string, args ...any) (sql.Result, error) {
	result, err := s.tx.ExecContext(s.ctx, rebind(s.dialect, query), args...)
	return result, normalizeError(err)
}

func (s *sqlStore) query(query string, args ...any) (*sql.Rows, error) {
	rows, err := s.tx.QueryContext(s.ctx, rebind(s.dialect, query), args...)
	return rows, normalizeError(err)
}

func (s *sqlStore) queryRow(query string, args ...any) *sql.Row {
	return s.tx.QueryRowContext(s.ctx, rebind(s.dialect, query), args...)
}

func encodeJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode repository payload: %w", err)
	}
	return string(payload), nil
}

func decodeJSON[T any](payload string) (T, error) {
	var value T
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return value, fmt.Errorf("decode repository payload: %w", err)
	}
	return value, nil
}

func (s *sqlStore) GetWorkItem(id domain.WorkItemID) (domain.WorkItem, error) {
	var payload string
	query := "SELECT payload FROM work_items WHERE id = ?"
	if s.writable && s.dialect == dialectPostgres {
		query += " FOR UPDATE"
	}
	if err := s.queryRow(query, id).Scan(&payload); err != nil {
		return domain.WorkItem{}, normalizeError(err)
	}
	return decodeJSON[domain.WorkItem](payload)
}

func (s *sqlStore) ListWorkItems(filter application.WorkItemFilter) ([]domain.WorkItem, error) {
	conditions := make([]string, 0)
	args := make([]any, 0)
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for index, status := range filter.Statuses {
			placeholders[index] = "?"
			args = append(args, status)
		}
		conditions = append(conditions, "status IN ("+strings.Join(placeholders, ", ")+")")
	}
	if len(filter.Modes) > 0 {
		placeholders := make([]string, len(filter.Modes))
		for index, mode := range filter.Modes {
			placeholders[index] = "?"
			args = append(args, mode)
		}
		conditions = append(conditions, "mode IN ("+strings.Join(placeholders, ", ")+")")
	}
	for _, tag := range filter.Tags {
		if s.dialect == dialectPostgres {
			conditions = append(conditions, "tags @> ARRAY[?]::TEXT[]")
		} else {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM json_each(work_items.tags) WHERE value = ?)")
		}
		args = append(args, tag)
	}
	query := "SELECT payload FROM work_items"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY updated_at DESC, id"
	rows, err := s.query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.WorkItem
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, normalizeError(err)
		}
		value, err := decodeJSON[domain.WorkItem](payload)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) GetTask(id domain.TaskID) (domain.Task, error) {
	if s.writable && s.dialect == dialectPostgres {
		var workItemID domain.WorkItemID
		if err := s.queryRow("SELECT work_item_id FROM tasks WHERE id = ?", id).Scan(&workItemID); err != nil {
			return domain.Task{}, normalizeError(err)
		}
		var lockedID domain.WorkItemID
		if err := s.queryRow("SELECT id FROM work_items WHERE id = ? FOR UPDATE", workItemID).Scan(&lockedID); err != nil {
			return domain.Task{}, normalizeError(err)
		}
	}
	var payload string
	if err := s.queryRow("SELECT payload FROM tasks WHERE id = ?", id).Scan(&payload); err != nil {
		return domain.Task{}, normalizeError(err)
	}
	return decodeJSON[domain.Task](payload)
}

func (s *sqlStore) ListTasks(workItemID domain.WorkItemID) ([]domain.Task, error) {
	rows, err := s.query(
		"SELECT payload FROM tasks WHERE work_item_id = ? ORDER BY position, id",
		workItemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Task
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, normalizeError(err)
		}
		value, err := decodeJSON[domain.Task](payload)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) ListTaskRelations(workItemID domain.WorkItemID) ([]domain.TaskRelation, error) {
	rows, err := s.query(
		"SELECT payload FROM task_relations WHERE work_item_id = ? ORDER BY from_task_id, to_task_id",
		workItemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.TaskRelation
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, normalizeError(err)
		}
		value, err := decodeJSON[domain.TaskRelation](payload)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) ListClaims(taskID domain.TaskID) ([]domain.Claim, error) {
	rows, err := s.query(
		"SELECT payload, executor_kind, executor_id, last_heartbeat_at, lease_until, lease_seconds FROM claims WHERE task_id = ? ORDER BY claimed_at, id",
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Claim
	for rows.Next() {
		var payload string
		var executorKind, executorID string
		var heartbeat, lease nullableScannedTime
		var leaseSeconds sql.NullInt64
		if err := rows.Scan(&payload, &executorKind, &executorID, &heartbeat, &lease, &leaseSeconds); err != nil {
			return nil, normalizeError(err)
		}
		value, err := decodeJSON[domain.Claim](payload)
		if err != nil {
			return nil, err
		}
		value.Executor = domain.ActorRef{Kind: domain.ActorKind(executorKind), ID: domain.ActorID(executorID)}
		if heartbeat.Valid {
			value.LastHeartbeatAt = heartbeat.Time
		}
		if lease.Valid {
			value.LeaseUntil = lease.Time
		}
		if leaseSeconds.Valid {
			value.LeaseSeconds = leaseSeconds.Int64
		}
		result = append(result, value)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) GetArtifact(id domain.ArtifactID) (domain.Artifact, error) {
	var artifact domain.Artifact
	var submissionID sql.NullString
	var createdAt scannedTime
	if err := s.queryRow(`
		SELECT id, work_item_id, task_id, claim_id, submission_id, name, uri, created_at
		FROM artifacts WHERE id = ?`, id).Scan(
		&artifact.ID, &artifact.WorkItemID, &artifact.TaskID, &artifact.ClaimID,
		&submissionID, &artifact.Name, &artifact.URI, &createdAt,
	); err != nil {
		return domain.Artifact{}, normalizeError(err)
	}
	if submissionID.Valid {
		value := domain.SubmissionID(submissionID.String)
		artifact.SubmissionID = &value
	}
	artifact.CreatedAt = createdAt.Time
	return artifact, nil
}

func (s *sqlStore) ListArtifacts(workItemID domain.WorkItemID) ([]domain.Artifact, error) {
	rows, err := s.query(`
		SELECT id, work_item_id, task_id, claim_id, submission_id, name, uri, created_at
		FROM artifacts WHERE work_item_id = ? ORDER BY created_at, id`, workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Artifact, 0)
	for rows.Next() {
		var artifact domain.Artifact
		var submissionID sql.NullString
		var createdAt scannedTime
		if err := rows.Scan(
			&artifact.ID, &artifact.WorkItemID, &artifact.TaskID, &artifact.ClaimID,
			&submissionID, &artifact.Name, &artifact.URI, &createdAt,
		); err != nil {
			return nil, normalizeError(err)
		}
		if submissionID.Valid {
			value := domain.SubmissionID(submissionID.String)
			artifact.SubmissionID = &value
		}
		artifact.CreatedAt = createdAt.Time
		result = append(result, artifact)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) GetArtifactBlob(uri string) (domain.ArtifactBlob, error) {
	var blob domain.ArtifactBlob
	var createdAt scannedTime
	if err := s.queryRow(`
		SELECT uri, digest, size, created_at FROM artifact_blobs WHERE uri = ?`, uri,
	).Scan(&blob.URI, &blob.Digest, &blob.Size, &createdAt); err != nil {
		return domain.ArtifactBlob{}, normalizeError(err)
	}
	blob.CreatedAt = createdAt.Time
	return blob, nil
}

func (s *sqlStore) ListArtifactGarbage(before time.Time) ([]domain.Artifact, error) {
	rows, err := s.query(`
		SELECT a.id, a.work_item_id, a.task_id, a.claim_id, a.submission_id, a.name, a.uri, a.created_at
		FROM artifacts a
		JOIN claims c ON c.id = a.claim_id
		WHERE a.submission_id IS NULL AND c.active = ? AND a.created_at <= ?
		ORDER BY a.created_at, a.id`, false, databaseTime(before))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Artifact, 0)
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, artifact)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) ListUnreferencedArtifactBlobs(before time.Time) ([]domain.ArtifactBlob, error) {
	rows, err := s.query(`
		SELECT b.uri, b.digest, b.size, b.created_at
		FROM artifact_blobs b
		WHERE b.created_at <= ?
		  AND NOT EXISTS (SELECT 1 FROM artifacts a WHERE a.uri = b.uri)
		ORDER BY b.created_at, b.uri`, databaseTime(before))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.ArtifactBlob, 0)
	for rows.Next() {
		var blob domain.ArtifactBlob
		var createdAt scannedTime
		if err := rows.Scan(&blob.URI, &blob.Digest, &blob.Size, &createdAt); err != nil {
			return nil, normalizeError(err)
		}
		blob.CreatedAt = createdAt.Time
		result = append(result, blob)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) ArtifactBlobReferenced(uri string) (bool, error) {
	var referenced bool
	if err := s.queryRow("SELECT EXISTS (SELECT 1 FROM artifacts WHERE uri = ?)", uri).Scan(&referenced); err != nil {
		return false, normalizeError(err)
	}
	return referenced, nil
}

type artifactScanner interface {
	Scan(...any) error
}

func scanArtifact(scanner artifactScanner) (domain.Artifact, error) {
	var artifact domain.Artifact
	var submissionID sql.NullString
	var createdAt scannedTime
	if err := scanner.Scan(
		&artifact.ID, &artifact.WorkItemID, &artifact.TaskID, &artifact.ClaimID,
		&submissionID, &artifact.Name, &artifact.URI, &createdAt,
	); err != nil {
		return domain.Artifact{}, normalizeError(err)
	}
	if submissionID.Valid {
		value := domain.SubmissionID(submissionID.String)
		artifact.SubmissionID = &value
	}
	artifact.CreatedAt = createdAt.Time
	return artifact, nil
}

func (s *sqlStore) GetWorkflowTaskActivation(
	id domain.WorkflowTaskActivationID,
) (domain.WorkflowTaskActivation, error) {
	var payload string
	if err := s.queryRow("SELECT payload FROM workflow_activations WHERE id = ?", id).Scan(&payload); err != nil {
		return domain.WorkflowTaskActivation{}, normalizeError(err)
	}
	return decodeJSON[domain.WorkflowTaskActivation](payload)
}

func (s *sqlStore) ListWorkflowTaskActivations(
	workItemID domain.WorkItemID,
) ([]domain.WorkflowTaskActivation, error) {
	rows, err := s.query(
		"SELECT payload FROM workflow_activations WHERE work_item_id = ? ORDER BY created_at, id",
		workItemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.WorkflowTaskActivation
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, normalizeError(err)
		}
		value, err := decodeJSON[domain.WorkflowTaskActivation](payload)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) ListOpenTasks(filter application.OpenTaskFilter) ([]application.WorkCandidate, error) {
	conditions := []string{"w.status = ?", "t.status = ?"}
	args := []any{domain.WorkItemStatusOpen, domain.TaskStatusPending}
	switch filter.ActorKind {
	case domain.ActorHuman:
		conditions = append(conditions, "t.executor IN (?, ?)")
		args = append(args, domain.ExecutorHuman, domain.ExecutorEither)
	case domain.ActorAgent:
		conditions = append(conditions, "t.executor IN (?, ?)")
		args = append(args, domain.ExecutorAgent, domain.ExecutorEither)
		if s.dialect == dialectPostgres {
			conditions = append(conditions, "(cardinality(t.allowed_roles) = 0 OR t.allowed_roles @> ARRAY[?]::TEXT[])")
		} else {
			conditions = append(conditions, "(json_array_length(t.allowed_roles) = 0 OR EXISTS (SELECT 1 FROM json_each(t.allowed_roles) WHERE value = ?))")
		}
		args = append(args, filter.Role)
	default:
		conditions = append(conditions, "1 = 0")
	}
	for _, tag := range filter.Tags {
		if s.dialect == dialectPostgres {
			conditions = append(conditions, "(w.mode = ? OR t.tags @> ARRAY[?]::TEXT[])")
		} else {
			conditions = append(conditions, "(w.mode = ? OR EXISTS (SELECT 1 FROM json_each(t.tags) WHERE value = ?))")
		}
		args = append(args, domain.CoordinationModeWorkflow, tag)
	}
	rows, err := s.query(`
		SELECT w.payload, t.payload
		FROM tasks t
		JOIN work_items w ON w.id = t.work_item_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY w.id, t.position, t.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []application.WorkCandidate
	for rows.Next() {
		var workItemPayload, taskPayload string
		if err := rows.Scan(&workItemPayload, &taskPayload); err != nil {
			return nil, normalizeError(err)
		}
		workItem, err := decodeJSON[domain.WorkItem](workItemPayload)
		if err != nil {
			return nil, err
		}
		task, err := decodeJSON[domain.Task](taskPayload)
		if err != nil {
			return nil, err
		}
		result = append(result, application.WorkCandidate{
			Kind: application.WorkCandidateTask, WorkItem: workItem, Task: &task,
		})
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) ListEmptyBlackboards(tags []string) ([]domain.WorkItem, error) {
	conditions := []string{"w.status = ?", "w.mode = ?", "NOT EXISTS (SELECT 1 FROM tasks t WHERE t.work_item_id = w.id)"}
	args := []any{domain.WorkItemStatusOpen, domain.CoordinationModeBlackboard}
	conditions, args = s.appendWorkItemTagConditions(conditions, args, "w", tags)
	rows, err := s.query(`
		SELECT w.payload
		FROM work_items w
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY w.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.WorkItem
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, normalizeError(err)
		}
		workItem, err := decodeJSON[domain.WorkItem](payload)
		if err != nil {
			return nil, err
		}
		result = append(result, workItem)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) ListBlackboardsAwaitingLifecycleDecision(tags []string) ([]domain.WorkItem, error) {
	conditions := []string{`w.mode = ?`, `(
			w.status = ?
			OR (
			  w.status = ?
			  AND EXISTS (SELECT 1 FROM tasks t WHERE t.work_item_id = w.id)
			  AND NOT EXISTS (
				SELECT 1 FROM tasks t
				WHERE t.work_item_id = w.id AND t.status NOT IN (?, ?)
			  )
			)
		  )`}
	args := []any{
		domain.CoordinationModeBlackboard,
		domain.WorkItemStatusAwaitingAgentAcceptance,
		domain.WorkItemStatusOpen,
		domain.TaskStatusCompleted,
		domain.TaskStatusSkipped,
	}
	conditions, args = s.appendWorkItemTagConditions(conditions, args, "w", tags)
	rows, err := s.query(`
		SELECT w.payload
		FROM work_items w
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY w.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.WorkItem
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, normalizeError(err)
		}
		workItem, err := decodeJSON[domain.WorkItem](payload)
		if err != nil {
			return nil, err
		}
		result = append(result, workItem)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) appendWorkItemTagConditions(conditions []string, args []any, alias string, tags []string) ([]string, []any) {
	for _, tag := range tags {
		if s.dialect == dialectPostgres {
			conditions = append(conditions, alias+".tags @> ARRAY[?]::TEXT[]")
		} else {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM json_each("+alias+".tags) WHERE value = ?)")
		}
		args = append(args, tag)
	}
	return conditions, args
}

func (s *sqlStore) ListReapableAgentClaimTasks(now time.Time) ([]domain.TaskID, error) {
	rows, err := s.query(`
		SELECT c.task_id
		FROM claims c
		JOIN tasks t ON t.id = c.task_id
		JOIN work_items w ON w.id = t.work_item_id
		WHERE c.executor_kind = ?
		  AND c.active = ?
		  AND c.lease_until IS NOT NULL
		  AND c.lease_until <= ?
		  AND t.status = ?
		  AND t.active_claim_id = c.id
		  AND w.status = ?
		ORDER BY c.lease_until, c.task_id`,
		domain.ActorAgent,
		true,
		databaseTime(now),
		domain.TaskStatusWorking,
		domain.WorkItemStatusOpen,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.TaskID, 0)
	for rows.Next() {
		var taskID domain.TaskID
		if err := rows.Scan(&taskID); err != nil {
			return nil, normalizeError(err)
		}
		result = append(result, taskID)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) GetWorkflowDefinition(
	id domain.DefinitionID,
	version int64,
) (domain.WorkflowDefinition, error) {
	var payload string
	if err := s.queryRow(
		"SELECT payload FROM definitions WHERE id = ? AND version = ? AND mode = ?",
		id, version, domain.CoordinationModeWorkflow,
	).Scan(&payload); err != nil {
		return domain.WorkflowDefinition{}, normalizeError(err)
	}
	return decodeJSON[domain.WorkflowDefinition](payload)
}

func (s *sqlStore) GetDefinitionMetadata(bindings []domain.DefinitionBinding) (map[domain.DefinitionBinding]domain.DefinitionMetadata, error) {
	result := make(map[domain.DefinitionBinding]domain.DefinitionMetadata, len(bindings))
	if len(bindings) == 0 {
		return result, nil
	}

	placeholders := make([]string, 0, len(bindings))
	args := make([]any, 0, len(bindings)*3)
	for _, binding := range bindings {
		placeholders = append(placeholders, "(?, ?, ?)")
		args = append(args, binding.ID, binding.Version, binding.Mode)
	}
	rows, err := s.query(fmt.Sprintf(`
		SELECT id, version, mode, payload
		FROM definitions
		WHERE (id, version, mode) IN (%s)`, strings.Join(placeholders, ", ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var binding domain.DefinitionBinding
		var payload string
		if err := rows.Scan(&binding.ID, &binding.Version, &binding.Mode, &payload); err != nil {
			return nil, normalizeError(err)
		}
		switch binding.Mode {
		case domain.CoordinationModeWorkflow:
			definition, err := decodeJSON[domain.WorkflowDefinition](payload)
			if err != nil {
				return nil, err
			}
			result[binding] = definition.DefinitionMetadata
		case domain.CoordinationModeBlackboard:
			definition, err := decodeJSON[domain.BlackboardDefinition](payload)
			if err != nil {
				return nil, err
			}
			result[binding] = definition.DefinitionMetadata
		default:
			return nil, fmt.Errorf("unsupported definition mode %q", binding.Mode)
		}
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) GetBlackboardDefinition(
	id domain.DefinitionID,
	version int64,
) (domain.BlackboardDefinition, error) {
	var payload string
	if err := s.queryRow(
		"SELECT payload FROM definitions WHERE id = ? AND version = ? AND mode = ?",
		id, version, domain.CoordinationModeBlackboard,
	).Scan(&payload); err != nil {
		return domain.BlackboardDefinition{}, normalizeError(err)
	}
	return decodeJSON[domain.BlackboardDefinition](payload)
}

func (s *sqlStore) GetLatestPublishedWorkflowDefinition(id domain.DefinitionID) (domain.WorkflowDefinition, error) {
	return latestPublishedDefinition[domain.WorkflowDefinition](s, id, domain.CoordinationModeWorkflow)
}

func (s *sqlStore) GetLatestPublishedBlackboardDefinition(id domain.DefinitionID) (domain.BlackboardDefinition, error) {
	return latestPublishedDefinition[domain.BlackboardDefinition](s, id, domain.CoordinationModeBlackboard)
}

func latestPublishedDefinition[T any](s *sqlStore, id domain.DefinitionID, mode domain.CoordinationMode) (T, error) {
	var payload string
	if err := s.queryRow(`
		SELECT payload FROM definitions
		WHERE id = ? AND mode = ? AND status = ?
		ORDER BY version DESC LIMIT 1`, id, mode, domain.DefinitionStatusPublished,
	).Scan(&payload); err != nil {
		var zero T
		return zero, normalizeError(err)
	}
	return decodeJSON[T](payload)
}

func (s *sqlStore) ListWorkflowDefinitions() ([]domain.WorkflowDefinition, error) {
	return listDefinitions[domain.WorkflowDefinition](s, domain.CoordinationModeWorkflow)
}

func (s *sqlStore) ListBlackboardDefinitions() ([]domain.BlackboardDefinition, error) {
	return listDefinitions[domain.BlackboardDefinition](s, domain.CoordinationModeBlackboard)
}

func listDefinitions[T any](s *sqlStore, mode domain.CoordinationMode) ([]T, error) {
	rows, err := s.query("SELECT payload FROM definitions WHERE mode = ? ORDER BY id, version", mode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]T, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, normalizeError(err)
		}
		definition, err := decodeJSON[T](payload)
		if err != nil {
			return nil, err
		}
		result = append(result, definition)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) LastWorkItemEventSequence(workItemID domain.WorkItemID) (int64, error) {
	var sequence int64
	if err := s.queryRow(
		"SELECT COALESCE(MAX(sequence), 0) FROM work_item_events WHERE work_item_id = ?",
		workItemID,
	).Scan(&sequence); err != nil {
		return 0, normalizeError(err)
	}
	return sequence, nil
}

func (s *sqlStore) GetIdempotencyRecord(
	actor domain.ActorRef,
	operationID string,
) (application.IdempotencyRecord, error) {
	var record application.IdempotencyRecord
	var actorKind, actorID string
	var createdAt scannedTime
	err := s.queryRow(`
		SELECT actor_kind, actor_id, operation_id, operation, status, request_hash, response, created_at
		FROM idempotency_records
		WHERE actor_kind = ? AND actor_id = ? AND operation_id = ?`,
		actor.Kind, actor.ID, operationID,
	).Scan(
		&actorKind,
		&actorID,
		&record.OperationID,
		&record.Operation,
		&record.Status,
		&record.RequestHash,
		&record.Response,
		&createdAt,
	)
	if err != nil {
		return application.IdempotencyRecord{}, normalizeError(err)
	}
	record.Actor = domain.ActorRef{Kind: domain.ActorKind(actorKind), ID: domain.ActorID(actorID)}
	record.CreatedAt = createdAt.Time
	return record, nil
}

func (s *sqlStore) ListPendingIdempotencyRecords(before time.Time) ([]application.IdempotencyRecord, error) {
	rows, err := s.query(`
		SELECT actor_kind, actor_id, operation_id, operation, status, request_hash, response, created_at
		FROM idempotency_records
		WHERE status = ? AND created_at <= ?
		ORDER BY created_at, actor_kind, actor_id, operation_id`, application.IdempotencyPending, databaseTime(before))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]application.IdempotencyRecord, 0)
	for rows.Next() {
		var record application.IdempotencyRecord
		var actorKind, actorID string
		var createdAt scannedTime
		if err := rows.Scan(&actorKind, &actorID, &record.OperationID, &record.Operation, &record.Status, &record.RequestHash, &record.Response, &createdAt); err != nil {
			return nil, normalizeError(err)
		}
		record.Actor = domain.ActorRef{Kind: domain.ActorKind(actorKind), ID: domain.ActorID(actorID)}
		record.CreatedAt = createdAt.Time
		result = append(result, record)
	}
	return result, normalizeError(rows.Err())
}

func (s *sqlStore) CreateWorkflowDefinition(value domain.WorkflowDefinition) error {
	value.DefinitionMetadata = normalizeDefinitionTimes(value.DefinitionMetadata)
	if err := value.Validate(); err != nil {
		return err
	}
	return s.createDefinition(value.DefinitionMetadata, domain.CoordinationModeWorkflow, value)
}

func (s *sqlStore) CreateBlackboardDefinition(value domain.BlackboardDefinition) error {
	value.DefinitionMetadata = normalizeDefinitionTimes(value.DefinitionMetadata)
	if err := value.Validate(); err != nil {
		return err
	}
	return s.createDefinition(value.DefinitionMetadata, domain.CoordinationModeBlackboard, value)
}

func (s *sqlStore) createDefinition(
	metadata domain.DefinitionMetadata,
	mode domain.CoordinationMode,
	value any,
) error {
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = s.exec(
		"INSERT INTO definitions (id, version, mode, status, created_at, updated_at, payload) VALUES (?, ?, ?, ?, ?, ?, ?)",
		metadata.ID, metadata.Version, mode, metadata.Status,
		databaseTime(metadata.CreatedAt), databaseTime(metadata.UpdatedAt), payload,
	)
	return err
}

func (s *sqlStore) CreateWorkItem(value domain.WorkItem) error {
	value = normalizeWorkItemTimes(value)
	if value.AcceptanceMode == "" {
		value.AcceptanceMode = domain.WorkItemAcceptanceNone
	}
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	tags, err := databaseStrings(s.dialect, value.Tags)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO work_items
			(id, definition_id, definition_version, mode, status, acceptance_mode, tags, version, created_at, updated_at, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID,
		value.Definition.ID,
		value.Definition.Version,
		value.Definition.Mode,
		value.Status,
		value.AcceptanceMode,
		tags,
		value.Version,
		databaseTime(value.CreatedAt),
		databaseTime(value.UpdatedAt),
		payload,
	)
	return err
}

func (s *sqlStore) SaveWorkItem(value domain.WorkItem) error {
	value = normalizeWorkItemTimes(value)
	if value.AcceptanceMode == "" {
		value.AcceptanceMode = domain.WorkItemAcceptanceNone
	}
	if err := value.Validate(); err != nil {
		return err
	}
	if value.Version <= 0 {
		return fmt.Errorf("%w: work item %q has no previous version", application.ErrConflict, value.ID)
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	tags, err := databaseStrings(s.dialect, value.Tags)
	if err != nil {
		return err
	}
	result, err := s.exec(`
		UPDATE work_items
		SET status = ?, acceptance_mode = ?, tags = ?, version = ?, updated_at = ?, payload = ?
		WHERE id = ? AND version = ?`,
		value.Status, value.AcceptanceMode, tags, value.Version, databaseTime(value.UpdatedAt), payload, value.ID, value.Version-1,
	)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "work_items", value.ID)
}

func (s *sqlStore) CreateTask(value domain.Task) error {
	value = normalizeTaskTimes(value)
	mode, err := s.workItemMode(value.WorkItemID)
	if err != nil {
		return err
	}
	if err := value.Validate(mode); err != nil {
		return err
	}
	if value.ParentTaskID != nil {
		var parentWorkItemID domain.WorkItemID
		if err := s.queryRow("SELECT work_item_id FROM tasks WHERE id = ?", *value.ParentTaskID).Scan(&parentWorkItemID); err != nil {
			return normalizeError(err)
		}
		if parentWorkItemID != value.WorkItemID {
			return fmt.Errorf("%w: parent task %q belongs to another work item", application.ErrConflict, *value.ParentTaskID)
		}
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	allowedRoles, err := databaseStrings(s.dialect, value.AllowedRoles)
	if err != nil {
		return err
	}
	tags, err := databaseStrings(s.dialect, value.Tags)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO tasks
			(id, work_item_id, parent_task_id, status, executor, allowed_roles, tags, active_claim_id, position, version, created_at, updated_at, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID,
		value.WorkItemID,
		nullString(value.ParentTaskID),
		value.Status,
		value.Executor,
		allowedRoles,
		tags,
		nullString(value.ActiveClaimID),
		value.Position,
		value.Version,
		databaseTime(value.CreatedAt),
		databaseTime(value.UpdatedAt),
		payload,
	)
	return err
}

func (s *sqlStore) SaveTask(value domain.Task) error {
	value = normalizeTaskTimes(value)
	mode, err := s.workItemMode(value.WorkItemID)
	if err != nil {
		return err
	}
	if err := value.Validate(mode); err != nil {
		return err
	}
	if value.Version <= 0 {
		return fmt.Errorf("%w: task %q has no previous version", application.ErrConflict, value.ID)
	}
	var storedParent sql.NullString
	if err := s.queryRow("SELECT parent_task_id FROM tasks WHERE id = ?", value.ID).Scan(&storedParent); err != nil {
		return normalizeError(err)
	}
	if storedParent.Valid != (value.ParentTaskID != nil) ||
		(storedParent.Valid && storedParent.String != string(*value.ParentTaskID)) {
		return fmt.Errorf("%w: task %q parent is immutable", application.ErrConflict, value.ID)
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	allowedRoles, err := databaseStrings(s.dialect, value.AllowedRoles)
	if err != nil {
		return err
	}
	tags, err := databaseStrings(s.dialect, value.Tags)
	if err != nil {
		return err
	}
	result, err := s.exec(`
		UPDATE tasks
		SET status = ?, executor = ?, allowed_roles = ?, tags = ?, active_claim_id = ?, position = ?, version = ?, updated_at = ?, payload = ?
		WHERE id = ? AND version = ?`,
		value.Status,
		value.Executor,
		allowedRoles,
		tags,
		nullString(value.ActiveClaimID),
		value.Position,
		value.Version,
		databaseTime(value.UpdatedAt),
		payload,
		value.ID,
		value.Version-1,
	)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "tasks", value.ID)
}

func (s *sqlStore) CreateTaskRelation(value domain.TaskRelation) error {
	value.CreatedAt = normalizeTime(value.CreatedAt)
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO task_relations (work_item_id, from_task_id, to_task_id, created_at, payload)
		VALUES (?, ?, ?, ?, ?)`,
		value.WorkItemID, value.FromTaskID, value.ToTaskID, databaseTime(value.CreatedAt), payload,
	)
	return err
}

func (s *sqlStore) CreateWorkflowTaskActivation(value domain.WorkflowTaskActivation) error {
	value.CreatedAt = normalizeTime(value.CreatedAt)
	value.UpdatedAt = normalizeTime(value.UpdatedAt)
	value.ResolvedAt = normalizeOptionalTime(value.ResolvedAt)
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO workflow_activations
			(id, work_item_id, workflow_task_id, correlation_id, status, created_at, updated_at, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID,
		value.WorkItemID,
		value.WorkflowTaskID,
		value.CorrelationID,
		value.Status,
		databaseTime(value.CreatedAt),
		databaseTime(value.UpdatedAt),
		payload,
	)
	return err
}

func (s *sqlStore) SaveWorkflowTaskActivation(value domain.WorkflowTaskActivation) error {
	value.CreatedAt = normalizeTime(value.CreatedAt)
	value.UpdatedAt = normalizeTime(value.UpdatedAt)
	value.ResolvedAt = normalizeOptionalTime(value.ResolvedAt)
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	result, err := s.exec(`
		UPDATE workflow_activations
		SET status = ?, updated_at = ?, payload = ?
		WHERE id = ?`,
		value.Status, databaseTime(value.UpdatedAt), payload, value.ID,
	)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "workflow_activations", value.ID)
}

func (s *sqlStore) CreateClaim(value domain.Claim) error {
	value = normalizeClaimTimes(value)
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	heartbeat, lease, leaseSeconds := claimLeaseColumns(value)
	_, err = s.exec(`
		INSERT INTO claims (id, task_id, executor_kind, executor_id, active, claimed_at, last_heartbeat_at, lease_until, lease_seconds, updated_at, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.TaskID, value.Executor.Kind, value.Executor.ID, value.Active(), databaseTime(value.ClaimedAt), heartbeat, lease, leaseSeconds, databaseTime(claimUpdatedAt(value)), payload,
	)
	return err
}

func (s *sqlStore) SaveClaim(value domain.Claim) error {
	value = normalizeClaimTimes(value)
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	heartbeat, lease, leaseSeconds := claimLeaseColumns(value)
	result, err := s.exec(`
		UPDATE claims
		SET task_id = ?, executor_kind = ?, executor_id = ?, active = ?, claimed_at = ?, last_heartbeat_at = ?, lease_until = ?, lease_seconds = ?, updated_at = ?, payload = ?
		WHERE id = ?`,
		value.TaskID, value.Executor.Kind, value.Executor.ID, value.Active(), databaseTime(value.ClaimedAt), heartbeat, lease, leaseSeconds, databaseTime(claimUpdatedAt(value)), payload, value.ID,
	)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "claims", value.ID)
}

func (s *sqlStore) CreateArtifact(value domain.Artifact) error {
	if err := value.Validate(); err != nil {
		return err
	}
	_, err := s.exec(`
		INSERT INTO artifacts
			(id, work_item_id, task_id, claim_id, submission_id, name, uri, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.WorkItemID, value.TaskID, value.ClaimID,
		nullString(value.SubmissionID), value.Name, value.URI, databaseTime(value.CreatedAt), databaseTime(value.CreatedAt),
	)
	return err
}

func (s *sqlStore) SaveArtifact(value domain.Artifact, updatedAt time.Time) error {
	if err := value.Validate(); err != nil {
		return err
	}
	result, err := s.exec(`
		UPDATE artifacts SET submission_id = ?, updated_at = ?
		WHERE id = ? AND work_item_id = ? AND task_id = ? AND claim_id = ? AND name = ? AND uri = ?`,
		nullString(value.SubmissionID), databaseTime(updatedAt), value.ID, value.WorkItemID, value.TaskID,
		value.ClaimID, value.Name, value.URI,
	)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "artifacts", value.ID)
}

func (s *sqlStore) DeleteArtifact(id domain.ArtifactID) error {
	result, err := s.exec("DELETE FROM artifacts WHERE id = ? AND submission_id IS NULL", id)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "artifacts", id)
}

func (s *sqlStore) CreateArtifactBlob(value domain.ArtifactBlob) error {
	if err := value.Validate(); err != nil {
		return err
	}
	result, err := s.exec(`
		INSERT INTO artifact_blobs (uri, digest, size, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (uri) DO NOTHING`,
		value.URI, value.Digest, value.Size, databaseTime(value.CreatedAt),
	)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return normalizeError(err)
	}
	if count == 1 {
		return nil
	}
	existing, err := s.GetArtifactBlob(value.URI)
	if err != nil {
		return err
	}
	if existing.Digest != value.Digest || existing.Size != value.Size {
		return application.ErrConflict
	}
	return nil
}

func (s *sqlStore) DeleteArtifactBlob(uri string) error {
	result, err := s.exec(`
		DELETE FROM artifact_blobs
		WHERE uri = ? AND NOT EXISTS (SELECT 1 FROM artifacts WHERE uri = ?)`, uri, uri)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "artifact_blobs", uri)
}

func claimLeaseColumns(value domain.Claim) (any, any, any) {
	if value.Executor.Kind != domain.ActorAgent {
		return nil, nil, nil
	}
	return databaseTime(value.LastHeartbeatAt), databaseTime(value.LeaseUntil), value.LeaseSeconds
}

func claimUpdatedAt(value domain.Claim) time.Time {
	updatedAt := value.ClaimedAt
	if value.LastHeartbeatAt.After(updatedAt) {
		updatedAt = value.LastHeartbeatAt
	}
	if value.EndedAt != nil && value.EndedAt.After(updatedAt) {
		updatedAt = *value.EndedAt
	}
	return updatedAt
}

func (s *sqlStore) AppendWorkItemEvent(value domain.WorkItemEvent) error {
	value.OccurredAt = normalizeTime(value.OccurredAt)
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO work_item_events (id, work_item_id, sequence, occurred_at, payload)
		VALUES (?, ?, ?, ?, ?)`,
		value.ID, value.WorkItemID, value.Sequence, databaseTime(value.OccurredAt), payload,
	)
	return err
}

func (s *sqlStore) LockIdempotencyKey(actor domain.ActorRef, operationID string) error {
	if s.dialect != dialectPostgres {
		return nil
	}
	key := string(actor.Kind) + ":" + string(actor.ID) + ":" + operationID
	_, err := s.exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key)
	return err
}

func (s *sqlStore) CreateIdempotencyRecord(value application.IdempotencyRecord) error {
	value.CreatedAt = normalizeTime(value.CreatedAt)
	_, err := s.exec(`
		INSERT INTO idempotency_records
			(actor_kind, actor_id, operation_id, operation, status, request_hash, response, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.Actor.Kind,
		value.Actor.ID,
		value.OperationID,
		value.Operation,
		value.Status,
		value.RequestHash,
		value.Response,
		databaseTime(value.CreatedAt),
		databaseTime(value.CreatedAt),
	)
	return err
}

func (s *sqlStore) SaveIdempotencyRecord(value application.IdempotencyRecord, updatedAt time.Time) error {
	result, err := s.exec(`
		UPDATE idempotency_records
		SET operation = ?, status = ?, request_hash = ?, response = ?, updated_at = ?
		WHERE actor_kind = ? AND actor_id = ? AND operation_id = ?`,
		value.Operation,
		value.Status,
		value.RequestHash,
		value.Response,
		databaseTime(updatedAt),
		value.Actor.Kind,
		value.Actor.ID,
		value.OperationID,
	)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return normalizeError(err)
	}
	if count != 1 {
		return application.ErrNotFound
	}
	return nil
}

func (s *sqlStore) DeleteIdempotencyRecord(actor domain.ActorRef, operationID string) error {
	result, err := s.exec(`
		DELETE FROM idempotency_records
		WHERE actor_kind = ? AND actor_id = ? AND operation_id = ? AND status = ?`,
		actor.Kind, actor.ID, operationID, application.IdempotencyPending)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return normalizeError(err)
	}
	if count != 1 {
		return application.ErrNotFound
	}
	return nil
}

func (s *sqlStore) workItemMode(workItemID domain.WorkItemID) (domain.CoordinationMode, error) {
	var mode domain.CoordinationMode
	if err := s.queryRow("SELECT mode FROM work_items WHERE id = ?", workItemID).Scan(&mode); err != nil {
		return "", normalizeError(err)
	}
	return mode, nil
}

func (s *sqlStore) requireUpdated(result sql.Result, table string, id any) error {
	count, err := result.RowsAffected()
	if err != nil {
		return normalizeError(err)
	}
	if count == 1 {
		return nil
	}
	var exists bool
	if err := s.queryRow("SELECT EXISTS (SELECT 1 FROM "+table+" WHERE id = ?)", id).Scan(&exists); err != nil {
		return normalizeError(err)
	}
	if !exists {
		return application.ErrNotFound
	}
	return application.ErrConflict
}

func nullString[T ~string](value *T) any {
	if value == nil {
		return nil
	}
	return string(*value)
}
