package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidModel is returned when a domain model violates an invariant.
var ErrInvalidModel = errors.New("invalid domain model")

func invalid(field, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalidModel, field, fmt.Sprintf(format, args...))
}

func validateStringSet(field string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return invalid(field, "must not contain empty values")
		}
		if _, ok := seen[value]; ok {
			return invalid(field, "contains duplicate value %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateTimestamps(createdAt, updatedAt time.Time) error {
	if createdAt.IsZero() {
		return invalid("created_at", "is required")
	}
	if updatedAt.IsZero() {
		return invalid("updated_at", "is required")
	}
	if updatedAt.Before(createdAt) {
		return invalid("updated_at", "must not be before created_at")
	}
	return nil
}
