package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ScienJus/kairos/internal/application"
)

const defaultPageLimit = 50
const maxCursorLength = 2048

type cursorEnvelope[T any] struct {
	Version  int    `json:"v"`
	Kind     string `json:"kind"`
	Position T      `json:"position"`
}

type pageResponse struct {
	Data       any     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

func parsePageRequest[T any](request *http.Request, kind string, valid func(T) bool) (application.PageRequest[T], error) {
	page := application.PageRequest[T]{Limit: defaultPageLimit}
	if raw := request.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > application.MaxPageLimit {
			return page, fmt.Errorf("%w: limit must be between 1 and %d", application.ErrInvalidCommand, application.MaxPageLimit)
		}
		page.Limit = limit
	}
	raw := request.URL.Query().Get("cursor")
	if raw == "" {
		return page, nil
	}
	if len(raw) > maxCursorLength {
		return page, invalidCursor()
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return page, invalidCursor()
	}
	var envelope cursorEnvelope[T]
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Version != 1 || envelope.Kind != kind || !valid(envelope.Position) {
		return page, invalidCursor()
	}
	page.After = &envelope.Position
	return page, nil
}

func encodeCursor[T any](kind string, position T) (string, error) {
	payload, err := json.Marshal(cursorEnvelope[T]{Version: 1, Kind: kind, Position: position})
	if err != nil {
		return "", fmt.Errorf("encode pagination cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func nextPageCursor[T any, C any](page application.Page[T], kind string, position func(T) C) (*string, error) {
	if !page.HasMore || len(page.Items) == 0 {
		return nil, nil
	}
	encoded, err := encodeCursor(kind, position(page.Items[len(page.Items)-1]))
	if err != nil {
		return nil, err
	}
	return &encoded, nil
}

func invalidCursor() error {
	return fmt.Errorf("%w: cursor is invalid or belongs to another collection", application.ErrInvalidCommand)
}
