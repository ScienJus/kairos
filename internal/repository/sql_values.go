package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
)

const sqliteTimestampLayout = "2006-01-02T15:04:05.000000Z"

func databaseTime(value time.Time) string {
	return normalizeTime(value).Format(sqliteTimestampLayout)
}

func normalizeTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := normalizeTime(*value)
	return &normalized
}

func normalizeDefinitionTimes(value domain.DefinitionMetadata) domain.DefinitionMetadata {
	value.CreatedAt = normalizeTime(value.CreatedAt)
	value.UpdatedAt = normalizeTime(value.UpdatedAt)
	return value
}

func normalizeWorkItemTimes(value domain.WorkItem) domain.WorkItem {
	value.CreatedAt = normalizeTime(value.CreatedAt)
	value.UpdatedAt = normalizeTime(value.UpdatedAt)
	value.CompletedAt = normalizeOptionalTime(value.CompletedAt)
	value.CancelledAt = normalizeOptionalTime(value.CancelledAt)
	return value
}

func normalizeTaskTimes(value domain.Task) domain.Task {
	value.CreatedAt = normalizeTime(value.CreatedAt)
	value.UpdatedAt = normalizeTime(value.UpdatedAt)
	value.DecomposedAt = normalizeOptionalTime(value.DecomposedAt)
	value.CompletedAt = normalizeOptionalTime(value.CompletedAt)
	return value
}

func normalizeClaimTimes(value domain.Claim) domain.Claim {
	value.ClaimedAt = normalizeTime(value.ClaimedAt)
	if !value.LastHeartbeatAt.IsZero() {
		value.LastHeartbeatAt = normalizeTime(value.LastHeartbeatAt)
	}
	if !value.LeaseUntil.IsZero() {
		value.LeaseUntil = normalizeTime(value.LeaseUntil)
	}
	value.EndedAt = normalizeOptionalTime(value.EndedAt)
	return value
}

type scannedTime struct {
	time.Time
}

func (value *scannedTime) Scan(source any) error {
	switch source := source.(type) {
	case time.Time:
		value.Time = source.UTC().Truncate(time.Microsecond)
		return nil
	case string:
		return value.parse(source)
	case []byte:
		return value.parse(string(source))
	default:
		return fmt.Errorf("scan timestamp from %T", source)
	}
}

func (value *scannedTime) parse(source string) error {
	parsed, err := time.Parse(time.RFC3339Nano, source)
	if err != nil {
		return fmt.Errorf("parse repository timestamp %q: %w", source, err)
	}
	value.Time = parsed.UTC().Truncate(time.Microsecond)
	return nil
}

type nullableScannedTime struct {
	Time  time.Time
	Valid bool
}

func (value *nullableScannedTime) Scan(source any) error {
	if source == nil {
		value.Time = time.Time{}
		value.Valid = false
		return nil
	}
	var scanned scannedTime
	if err := scanned.Scan(source); err != nil {
		return err
	}
	value.Time = scanned.Time
	value.Valid = true
	return nil
}

func databaseStrings(dialect dialect, values []string) (any, error) {
	if values == nil {
		values = []string{}
	}
	if dialect == dialectPostgres {
		return values, nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode string array: %w", err)
	}
	return string(encoded), nil
}
