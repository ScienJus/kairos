package repository

import (
	"context"
	"database/sql"

	"github.com/ScienJus/kairos/internal/domain"
)

// Existing deployments precede structured failure snapshots. Read complete
// event payloads once during migration, without querying JSON domain fields.
// Historical failures use the ordinary failure shape and retain their message.
func backfillWorkItemFailures(ctx context.Context, tx *sql.Tx, d dialect) error {
	rows, err := tx.QueryContext(ctx, rebind(d, "SELECT payload FROM work_items WHERE status = ?"), domain.WorkItemStatusFailed)
	if err != nil {
		return err
	}
	var works []domain.WorkItem
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return err
		}
		work, err := decodeJSON[domain.WorkItem](payload)
		if err != nil {
			rows.Close()
			return err
		}
		works = append(works, work)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, work := range works {
		events, err := tx.QueryContext(ctx, rebind(d, "SELECT payload FROM work_item_events WHERE work_item_id = ? ORDER BY sequence DESC"), work.ID)
		if err != nil {
			return err
		}
		for events.Next() {
			var payload string
			if err := events.Scan(&payload); err != nil {
				events.Close()
				return err
			}
			event, err := decodeJSON[domain.WorkItemEvent](payload)
			if err != nil {
				events.Close()
				return err
			}
			if event.Type != domain.WorkItemEventWorkItemFailed {
				continue
			}
			work.Failure = &domain.WorkItemFailure{Kind: domain.FailureExecution, Message: event.Message}
			break
		}
		err = events.Err()
		events.Close()
		if err != nil {
			return err
		}
		if err := work.Validate(); err != nil {
			return err
		}
		payload, err := encodeJSON(work)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, rebind(d, "UPDATE work_items SET payload = ? WHERE id = ?"), payload, work.ID); err != nil {
			return err
		}
	}
	return nil
}
