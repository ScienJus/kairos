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

func (s *Service) idempotentUpdate(
	ctx context.Context,
	identity Identity,
	operationID string,
	operation string,
	request any,
	result any,
	update func(WriteStore) error,
) error {
	normalizedOperationID := strings.TrimSpace(operationID)
	if operationID == "" {
		return s.repository.Update(ctx, update)
	}
	if normalizedOperationID == "" || normalizedOperationID != operationID {
		return invalidCommand("operation id must not have surrounding whitespace")
	}
	requestPayload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode idempotent request: %w", err)
	}
	digest := sha256.Sum256(requestPayload)
	requestHash := hex.EncodeToString(digest[:])

	return s.repository.Update(ctx, func(store WriteStore) error {
		if err := store.LockIdempotencyKey(identity.Actor, operationID); err != nil {
			return fmt.Errorf("lock operation %q: %w", operationID, err)
		}
		record, err := store.GetIdempotencyRecord(identity.Actor, operationID)
		switch {
		case err == nil:
			if record.Operation != operation || record.RequestHash != requestHash {
				return conflict("operation id %q was already used for another request", operationID)
			}
			if err := json.Unmarshal([]byte(record.Response), result); err != nil {
				return fmt.Errorf("decode idempotent response: %w", err)
			}
			return nil
		case !errors.Is(err, ErrNotFound):
			return fmt.Errorf("get operation %q: %w", operationID, err)
		}

		if err := update(store); err != nil {
			return err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode idempotent response: %w", err)
		}
		return store.CreateIdempotencyRecord(IdempotencyRecord{
			Actor:       identity.Actor,
			OperationID: operationID,
			Operation:   operation,
			RequestHash: requestHash,
			Response:    string(response),
			CreatedAt:   s.clock.Now(),
		})
	})
}
