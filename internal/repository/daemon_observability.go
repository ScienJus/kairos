package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/daemonobs"
)

var _ daemonobs.Store = (*SQLRepository)(nil)

func (r *SQLRepository) RegisterDaemon(ctx context.Context, value daemonobs.Instance) (daemonobs.Instance, bool, error) {
	value.ProcessStartedAt = normalizeTime(value.ProcessStartedAt)
	value.RegisteredAt = normalizeTime(value.RegisteredAt)
	value.LastReportAt = normalizeTime(value.LastReportAt)
	if value.Tags == nil {
		value.Tags = []string{}
	}
	if value.ActiveDispatches == nil {
		value.ActiveDispatches = []daemonobs.Dispatch{}
	}
	var result daemonobs.Instance
	var created bool
	err := r.withWriteTransaction(ctx, func(store *sqlStore) error {
		payload, err := json.Marshal(value)
		if err != nil {
			return err
		}
		query := rebind(r.dialect, `INSERT INTO daemon_instances
			(id, agent_id, registered_at, last_report_at, last_revision, lifecycle, health, active_count, payload)
			VALUES (?, ?, ?, ?, 0, 'running', 'unknown', 0, ?) ON CONFLICT (id) DO NOTHING`)
		res, err := store.tx.ExecContext(ctx, query, value.ID, value.AgentID, databaseTime(value.RegisteredAt), databaseTime(value.LastReportAt), string(payload))
		if err != nil {
			return normalizeError(err)
		}
		count, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if count == 1 {
			result, created = value, true
			return nil
		}
		existing, err := loadDaemon(ctx, store.tx, r.dialect, value.ID)
		if err != nil {
			return err
		}
		if existing.AgentID != value.AgentID || existing.Name != value.Name || existing.DaemonVersion != value.DaemonVersion ||
			existing.Adapter != value.Adapter || existing.Slots != value.Slots ||
			!existing.ProcessStartedAt.Equal(value.ProcessStartedAt) || !slices.Equal(existing.Tags, value.Tags) {
			return daemonobs.ErrConflict
		}
		result = existing
		return nil
	})
	return result, created, err
}

func (r *SQLRepository) SaveDaemonReport(ctx context.Context, agentID, id string, report daemonobs.Report, now time.Time) error {
	err := r.withWriteTransaction(ctx, func(store *sqlStore) error {
		query := "SELECT agent_id, last_revision, lifecycle, payload FROM daemon_instances WHERE id = ?"
		if r.dialect == dialectPostgres {
			query += " FOR UPDATE"
		}
		var storedAgentID, storedLifecycle, raw string
		var storedRevision int64
		err := store.tx.QueryRowContext(ctx, rebind(r.dialect, query), id).Scan(&storedAgentID, &storedRevision, &storedLifecycle, &raw)
		if errors.Is(err, sql.ErrNoRows) {
			return daemonobs.ErrNotFound
		}
		if err != nil {
			return normalizeError(err)
		}
		var value daemonobs.Instance
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return err
		}
		value.AgentID = storedAgentID
		value.LastRevision = storedRevision
		value.Lifecycle = storedLifecycle
		if storedAgentID != agentID {
			return daemonobs.ErrForbidden
		}
		if report.ActiveCount > value.Slots || (storedLifecycle == "stopped" && report.Lifecycle != "stopped" && report.Revision > storedRevision) {
			return daemonobs.ErrConflict
		}
		for _, event := range report.Events {
			event.ReceivedAt = nil
			payload, err := json.Marshal(event)
			if err != nil {
				return err
			}
			insert := rebind(r.dialect, `INSERT INTO daemon_events
				(instance_id, sequence, kind, occurred_at, received_at, work_item_id, task_id, claim_id, payload)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (instance_id, sequence) DO NOTHING`)
			_, err = store.tx.ExecContext(ctx, insert, id, event.Sequence, event.Kind, databaseTime(event.OccurredAt), databaseTime(now),
				optionalString(event.WorkItemID), optionalString(event.TaskID), optionalString(event.ClaimID), string(payload))
			if err != nil {
				return normalizeError(err)
			}
		}
		if report.Revision <= storedRevision {
			return nil
		}
		value.LastRevision = report.Revision
		value.LastReportAt = normalizeTime(now)
		value.Lifecycle = report.Lifecycle
		value.Health = report.Health
		value.ActiveCount = report.ActiveCount
		value.ActiveDispatches = append([]daemonobs.Dispatch{}, report.ActiveDispatches...)
		value.OmittedActiveCount = report.OmittedActiveCount
		value.DroppedEvents = report.DroppedEvents
		if report.Lifecycle == "stopped" && value.StoppedAt == nil {
			stopped := normalizeTime(now)
			value.StoppedAt = &stopped
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return err
		}
		update := rebind(r.dialect, `UPDATE daemon_instances SET last_report_at = ?, stopped_at = ?, last_revision = ?, lifecycle = ?, health = ?, active_count = ?, payload = ? WHERE id = ?`)
		_, err = store.tx.ExecContext(ctx, update, databaseTime(value.LastReportAt), optionalTime(value.StoppedAt), value.LastRevision,
			value.Lifecycle, value.Health, value.ActiveCount, string(payload), id)
		return normalizeError(err)
	})
	return err
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return databaseTime(*value)
}

func loadDaemon(ctx context.Context, tx *sql.Tx, dialect dialect, id string) (daemonobs.Instance, error) {
	var agentID, lifecycle, raw string
	var lastRevision int64
	query := rebind(dialect, "SELECT agent_id, last_revision, lifecycle, payload FROM daemon_instances WHERE id = ?")
	err := tx.QueryRowContext(ctx, query, id).Scan(&agentID, &lastRevision, &lifecycle, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return daemonobs.Instance{}, daemonobs.ErrNotFound
	}
	if err != nil {
		return daemonobs.Instance{}, normalizeError(err)
	}
	var result daemonobs.Instance
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return result, err
	}
	result.AgentID = agentID
	result.LastRevision = lastRevision
	result.Lifecycle = lifecycle
	return result, nil
}

func (r *SQLRepository) GetDaemon(ctx context.Context, id string) (daemonobs.Instance, error) {
	var raw string
	err := r.db.QueryRowContext(ctx, rebind(r.dialect, "SELECT payload FROM daemon_instances WHERE id = ?"), id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return daemonobs.Instance{}, daemonobs.ErrNotFound
	}
	if err != nil {
		return daemonobs.Instance{}, normalizeError(err)
	}
	var result daemonobs.Instance
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return result, err
	}
	return result, nil
}

func (r *SQLRepository) ListDaemons(ctx context.Context, filter daemonobs.ListFilter) ([]daemonobs.Instance, error) {
	conditions := []string{}
	args := []any{}
	if filter.AgentID != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if !filter.IncludeHistory {
		conditions = append(conditions, "last_report_at >= ?")
		args = append(args, databaseTime(filter.Since))
	}
	if filter.Before != nil {
		conditions = append(conditions, "(last_report_at < ? OR (last_report_at = ? AND id > ?))")
		value := databaseTime(filter.Before.LastReportAt)
		args = append(args, value, value, filter.Before.ID)
	}
	query := "SELECT payload FROM daemon_instances"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY last_report_at DESC, id LIMIT ?"
	args = append(args, filter.Limit+1)
	rows, err := r.db.QueryContext(ctx, rebind(r.dialect, query), args...)
	if err != nil {
		return nil, normalizeError(err)
	}
	defer rows.Close()
	result := []daemonobs.Instance{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var value daemonobs.Instance
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, normalizeError(rows.Err())
}

func (r *SQLRepository) ListDaemonEvents(ctx context.Context, id string, limit int, before int64) ([]daemonobs.Event, error) {
	if _, err := r.GetDaemon(ctx, id); err != nil {
		return nil, err
	}
	query := "SELECT payload, received_at FROM daemon_events WHERE instance_id = ?"
	args := []any{id}
	if before > 0 {
		query += " AND sequence < ?"
		args = append(args, before)
	}
	query += " ORDER BY sequence DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, rebind(r.dialect, query), args...)
	if err != nil {
		return nil, normalizeError(err)
	}
	defer rows.Close()
	result := []daemonobs.Event{}
	for rows.Next() {
		var raw string
		var received scannedTime
		if err := rows.Scan(&raw, &received); err != nil {
			return nil, err
		}
		var value daemonobs.Event
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		value.ReceivedAt = &received.Time
		result = append(result, value)
	}
	return result, normalizeError(rows.Err())
}

func (r *SQLRepository) PruneDaemonObservations(ctx context.Context, now time.Time) error {
	for _, target := range []struct {
		query  string
		cutoff time.Time
	}{
		{"DELETE FROM daemon_events WHERE (instance_id, sequence) IN (SELECT instance_id, sequence FROM daemon_events WHERE received_at < ? ORDER BY received_at LIMIT 500)", now.Add(-30 * 24 * time.Hour)},
		{"DELETE FROM daemon_instances WHERE id IN (SELECT id FROM daemon_instances WHERE last_report_at < ? ORDER BY last_report_at LIMIT 100)", now.Add(-90 * 24 * time.Hour)},
	} {
		for {
			var deleted int64
			err := r.withWriteTransaction(ctx, func(store *sqlStore) error {
				result, err := store.tx.ExecContext(ctx, rebind(r.dialect, target.query), databaseTime(target.cutoff))
				if err != nil {
					return normalizeError(err)
				}
				deleted, err = result.RowsAffected()
				return err
			})
			if err != nil {
				return fmt.Errorf("prune daemon observations: %w", err)
			}
			if deleted == 0 {
				break
			}
		}
	}
	return nil
}
