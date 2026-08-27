package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type commitError struct{ err error }

func (e commitError) Error() string { return e.err.Error() }
func (e commitError) Unwrap() error { return e.err }

func commitAndReturn(err error) error {
	return commitError{err: err}
}

// replayableCreate persists the first result of a resource-creating API call.
// It is intentionally not used for lifecycle transitions, whose current state
// is the source of truth when a caller retries after losing a response.
func (s *Service) replayableCreate(
	ctx context.Context,
	identity Identity,
	operationID string,
	operation string,
	request any,
	result any,
	update func(WriteStore) error,
) error {
	normalizedOperationID := strings.TrimSpace(operationID)
	var committedErr error
	apply := func(store WriteStore) error {
		err := update(store)
		var marked commitError
		if errors.As(err, &marked) {
			committedErr = marked.err
			return nil
		}
		return err
	}
	if operationID == "" {
		if err := s.repository.Update(ctx, apply); err != nil {
			return err
		}
		return committedErr
	}
	if normalizedOperationID == "" || normalizedOperationID != operationID {
		return invalidCommand("operation id must not have surrounding whitespace")
	}
	requestHash, err := idempotencyRequestHash(request)
	if err != nil {
		return err
	}

	err = s.repository.Update(ctx, func(store WriteStore) error {
		if err := store.LockIdempotencyKey(identity.Actor, operationID); err != nil {
			return fmt.Errorf("lock operation %q: %w", operationID, err)
		}
		record, err := store.GetIdempotencyRecord(identity.Actor, operationID)
		switch {
		case err == nil:
			if record.Status != IdempotencyCompleted || record.Operation != operation || record.RequestHash != requestHash {
				return conflict("operation id %q was already used for another request", operationID)
			}
			if err := json.Unmarshal([]byte(record.Response), result); err != nil {
				return fmt.Errorf("decode idempotent response: %w", err)
			}
			return nil
		case !errors.Is(err, ErrNotFound):
			return fmt.Errorf("get operation %q: %w", operationID, err)
		}

		if err := apply(store); err != nil {
			return err
		}
		if committedErr != nil {
			return nil
		}
		response, err := idempotencyResponse(result)
		if err != nil {
			return err
		}
		return store.CreateIdempotencyRecord(IdempotencyRecord{
			Actor:       identity.Actor,
			OperationID: operationID,
			Operation:   operation,
			Status:      IdempotencyCompleted,
			RequestHash: requestHash,
			Response:    response,
			CreatedAt:   s.clock.Now(),
		})
	})
	if err != nil {
		return err
	}
	return committedErr
}

func idempotencyRequestHash(request any) (string, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encode idempotent request: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func idempotencyResponse(result any) (string, error) {
	response, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode idempotent response: %w", err)
	}
	return string(response), nil
}
