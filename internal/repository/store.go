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

func (s *sqlStore) ListWorkItems() ([]domain.WorkItem, error) {
	rows, err := s.query("SELECT payload FROM work_items ORDER BY id")
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
		"SELECT payload, executor_kind, executor_id, last_heartbeat_at_ns, lease_until_ns, lease_seconds FROM claims WHERE task_id = ? ORDER BY claimed_at_ns, id",
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
		var heartbeat, lease sql.NullInt64
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
			value.LastHeartbeatAt = time.Unix(0, heartbeat.Int64).UTC()
		}
		if lease.Valid {
			value.LeaseUntil = time.Unix(0, lease.Int64).UTC()
		}
		if leaseSeconds.Valid {
			value.LeaseSeconds = leaseSeconds.Int64
		}
		result = append(result, value)
	}
	return result, normalizeError(rows.Err())
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
		"SELECT payload FROM workflow_activations WHERE work_item_id = ? ORDER BY created_at_ns, id",
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

func (s *sqlStore) ListOpenTasks() ([]application.WorkCandidate, error) {
	rows, err := s.query(`
		SELECT w.payload, t.payload
		FROM tasks t
		JOIN work_items w ON w.id = t.work_item_id
		WHERE w.status = ? AND t.status = ?
		ORDER BY w.id, t.position, t.id`, domain.WorkItemStatusOpen, domain.TaskStatusPending)
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

func (s *sqlStore) ListEmptyBlackboards() ([]domain.WorkItem, error) {
	rows, err := s.query(`
		SELECT w.payload
		FROM work_items w
		WHERE w.status = ? AND w.mode = ?
		  AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.work_item_id = w.id)
		ORDER BY w.id`, domain.WorkItemStatusOpen, domain.CoordinationModeBlackboard)
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

func (s *sqlStore) ListBlackboardsAwaitingLifecycleDecision() ([]domain.WorkItem, error) {
	rows, err := s.query(`
		SELECT w.payload
		FROM work_items w
		WHERE w.mode = ?
		  AND (
			w.status = ?
			OR (
			  w.status = ?
			  AND EXISTS (SELECT 1 FROM tasks t WHERE t.work_item_id = w.id)
			  AND NOT EXISTS (
				SELECT 1 FROM tasks t
				WHERE t.work_item_id = w.id AND t.status NOT IN (?, ?)
			  )
			)
		  )
		ORDER BY w.id`,
		domain.CoordinationModeBlackboard,
		domain.WorkItemStatusAwaitingAgentAcceptance,
		domain.WorkItemStatusOpen,
		domain.TaskStatusCompleted,
		domain.TaskStatusSkipped,
	)
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
	var createdAtNS int64
	err := s.queryRow(`
		SELECT actor_kind, actor_id, operation_id, operation, request_hash, response, created_at_ns
		FROM idempotency_records
		WHERE actor_kind = ? AND actor_id = ? AND operation_id = ?`,
		actor.Kind, actor.ID, operationID,
	).Scan(
		&actorKind,
		&actorID,
		&record.OperationID,
		&record.Operation,
		&record.RequestHash,
		&record.Response,
		&createdAtNS,
	)
	if err != nil {
		return application.IdempotencyRecord{}, normalizeError(err)
	}
	record.Actor = domain.ActorRef{Kind: domain.ActorKind(actorKind), ID: domain.ActorID(actorID)}
	record.CreatedAt = time.Unix(0, createdAtNS).UTC()
	return record, nil
}

func (s *sqlStore) CreateWorkflowDefinition(value domain.WorkflowDefinition) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.createDefinition(value.ID, value.Version, domain.CoordinationModeWorkflow, value)
}

func (s *sqlStore) CreateBlackboardDefinition(value domain.BlackboardDefinition) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.createDefinition(value.ID, value.Version, domain.CoordinationModeBlackboard, value)
}

func (s *sqlStore) createDefinition(
	id domain.DefinitionID,
	version int64,
	mode domain.CoordinationMode,
	value any,
) error {
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = s.exec(
		"INSERT INTO definitions (id, version, mode, payload) VALUES (?, ?, ?, ?)",
		id, version, mode, payload,
	)
	return err
}

func (s *sqlStore) CreateWorkItem(value domain.WorkItem) error {
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
	_, err = s.exec(`
		INSERT INTO work_items
			(id, definition_id, definition_version, mode, status, acceptance_mode, version, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID,
		value.Definition.ID,
		value.Definition.Version,
		value.Definition.Mode,
		value.Status,
		value.AcceptanceMode,
		value.Version,
		payload,
	)
	return err
}

func (s *sqlStore) SaveWorkItem(value domain.WorkItem) error {
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
	result, err := s.exec(`
		UPDATE work_items
		SET status = ?, acceptance_mode = ?, version = ?, payload = ?
		WHERE id = ? AND version = ?`,
		value.Status, value.AcceptanceMode, value.Version, payload, value.ID, value.Version-1,
	)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "work_items", value.ID)
}

func (s *sqlStore) CreateTask(value domain.Task) error {
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
	_, err = s.exec(`
		INSERT INTO tasks
			(id, work_item_id, parent_task_id, status, active_claim_id, position, version, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID,
		value.WorkItemID,
		nullString(value.ParentTaskID),
		value.Status,
		nullString(value.ActiveClaimID),
		value.Position,
		value.Version,
		payload,
	)
	return err
}

func (s *sqlStore) SaveTask(value domain.Task) error {
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
	result, err := s.exec(`
		UPDATE tasks
		SET status = ?, active_claim_id = ?, position = ?, version = ?, payload = ?
		WHERE id = ? AND version = ?`,
		value.Status,
		nullString(value.ActiveClaimID),
		value.Position,
		value.Version,
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
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO task_relations (work_item_id, from_task_id, to_task_id, payload)
		VALUES (?, ?, ?, ?)`,
		value.WorkItemID, value.FromTaskID, value.ToTaskID, payload,
	)
	return err
}

func (s *sqlStore) CreateWorkflowTaskActivation(value domain.WorkflowTaskActivation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO workflow_activations
			(id, work_item_id, workflow_task_id, correlation_id, status, created_at_ns, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		value.ID,
		value.WorkItemID,
		value.WorkflowTaskID,
		value.CorrelationID,
		value.Status,
		value.CreatedAt.UnixNano(),
		payload,
	)
	return err
}

func (s *sqlStore) SaveWorkflowTaskActivation(value domain.WorkflowTaskActivation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	result, err := s.exec(`
		UPDATE workflow_activations
		SET status = ?, payload = ?
		WHERE id = ?`,
		value.Status, payload, value.ID,
	)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "workflow_activations", value.ID)
}

func (s *sqlStore) CreateClaim(value domain.Claim) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	heartbeat, lease, leaseSeconds := claimLeaseColumns(value)
	_, err = s.exec(`
		INSERT INTO claims (id, task_id, executor_kind, executor_id, active, claimed_at_ns, last_heartbeat_at_ns, lease_until_ns, lease_seconds, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.TaskID, value.Executor.Kind, value.Executor.ID, value.Active(), value.ClaimedAt.UnixNano(), heartbeat, lease, leaseSeconds, payload,
	)
	return err
}

func (s *sqlStore) SaveClaim(value domain.Claim) error {
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
		SET task_id = ?, executor_kind = ?, executor_id = ?, active = ?, claimed_at_ns = ?, last_heartbeat_at_ns = ?, lease_until_ns = ?, lease_seconds = ?, payload = ?
		WHERE id = ?`,
		value.TaskID, value.Executor.Kind, value.Executor.ID, value.Active(), value.ClaimedAt.UnixNano(), heartbeat, lease, leaseSeconds, payload, value.ID,
	)
	if err != nil {
		return err
	}
	return s.requireUpdated(result, "claims", value.ID)
}

func claimLeaseColumns(value domain.Claim) (any, any, any) {
	if value.Executor.Kind != domain.ActorAgent {
		return nil, nil, nil
	}
	return value.LastHeartbeatAt.UnixNano(), value.LeaseUntil.UnixNano(), value.LeaseSeconds
}

func (s *sqlStore) AppendWorkItemEvent(value domain.WorkItemEvent) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO work_item_events (id, work_item_id, sequence, occurred_at_ns, payload)
		VALUES (?, ?, ?, ?, ?)`,
		value.ID, value.WorkItemID, value.Sequence, value.OccurredAt.UnixNano(), payload,
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
	_, err := s.exec(`
		INSERT INTO idempotency_records
			(actor_kind, actor_id, operation_id, operation, request_hash, response, created_at_ns)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		value.Actor.Kind,
		value.Actor.ID,
		value.OperationID,
		value.Operation,
		value.RequestHash,
		value.Response,
		value.CreatedAt.UnixNano(),
	)
	return err
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
