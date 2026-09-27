package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/daemonobs"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

func (h *Handler) daemonActor(writer http.ResponseWriter, request *http.Request, kind domain.ActorKind) (identity.Identity, bool) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return actor, false
	}
	if err := actor.Validate(); err != nil {
		writeError(writer, err)
		return actor, false
	}
	if actor.Actor.Kind != kind {
		writeError(writer, application.ErrForbidden)
		return actor, false
	}
	if h.daemonObs == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: apiError{Code: "unavailable", Message: "daemon observations are unavailable"}})
		return actor, false
	}
	return actor, true
}

func (h *Handler) registerDaemonInstance(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.daemonActor(writer, request, domain.ActorAgent)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 64<<10)
	var body daemonobs.Registration
	if !decodeRequest(writer, request, &body) {
		return
	}
	value, created, err := h.daemonObs.Register(request.Context(), string(actor.Actor.ID), body)
	if err != nil {
		writeDaemonError(writer, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, status, dataResponse{Data: value})
}

func (h *Handler) reportDaemonInstance(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.daemonActor(writer, request, domain.ActorAgent)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 64<<10)
	var body daemonobs.Report
	if !decodeRequest(writer, request, &body) {
		return
	}
	err := h.daemonObs.Report(request.Context(), string(actor.Actor.ID), request.PathValue("instance_id"), body)
	if err != nil {
		writeDaemonError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getDaemonInstance(writer http.ResponseWriter, request *http.Request) {
	if _, ok := h.daemonActor(writer, request, domain.ActorHuman); !ok {
		return
	}
	value, err := h.daemonObs.Get(request.Context(), request.PathValue("instance_id"))
	if err != nil {
		writeDaemonError(writer, err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, dataResponse{Data: value})
}

func (h *Handler) listDaemonInstances(writer http.ResponseWriter, request *http.Request) {
	if _, ok := h.daemonActor(writer, request, domain.ActorHuman); !ok {
		return
	}
	page, err := parsePageRequest(request, "daemon_instances", func(value daemonobs.InstanceCursor) bool {
		return !value.LastReportAt.IsZero() && value.ID != ""
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	includeHistory := false
	if raw := request.URL.Query().Get("include_history"); raw != "" {
		includeHistory, err = strconv.ParseBool(raw)
		if err != nil {
			writeError(writer, fmt.Errorf("%w: include_history must be a boolean", application.ErrInvalidCommand))
			return
		}
	}
	values, err := h.daemonObs.List(request.Context(), daemonobs.ListFilter{
		AgentID: strings.TrimSpace(request.URL.Query().Get("agent_id")), IncludeHistory: includeHistory,
		Limit: page.Limit, Before: page.After,
	})
	if err != nil {
		writeDaemonError(writer, err)
		return
	}
	var next *string
	if len(values) > page.Limit {
		values = values[:page.Limit]
		cursor := daemonobs.InstanceCursor{LastReportAt: values[len(values)-1].LastReportAt, ID: values[len(values)-1].ID}
		encoded, err := encodeCursor("daemon_instances", cursor)
		if err != nil {
			writeError(writer, err)
			return
		}
		next = &encoded
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, struct {
		Data       []daemonobs.Instance `json:"data"`
		NextCursor *string              `json:"next_cursor"`
		AsOf       time.Time            `json:"as_of"`
	}{Data: values, NextCursor: next, AsOf: time.Now().UTC().Truncate(time.Microsecond)})
}

func (h *Handler) listDaemonEvents(writer http.ResponseWriter, request *http.Request) {
	if _, ok := h.daemonActor(writer, request, domain.ActorHuman); !ok {
		return
	}
	page, err := parsePageRequest(request, "daemon_events", func(value int64) bool { return value > 0 })
	if err != nil {
		writeError(writer, err)
		return
	}
	before := int64(0)
	if page.After != nil {
		before = *page.After
	}
	values, err := h.daemonObs.Events(request.Context(), request.PathValue("instance_id"), page.Limit, before)
	if err != nil {
		writeDaemonError(writer, err)
		return
	}
	var next *string
	if len(values) > page.Limit {
		values = values[:page.Limit]
		encoded, err := encodeCursor("daemon_events", values[len(values)-1].Sequence)
		if err != nil {
			writeError(writer, err)
			return
		}
		next = &encoded
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, pageResponse{Data: values, NextCursor: next})
}

func writeDaemonError(writer http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal_error"
	switch {
	case errors.Is(err, daemonobs.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, daemonobs.ErrForbidden):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, daemonobs.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, daemonobs.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = "internal server error"
	}
	writeJSON(writer, status, errorResponse{Error: apiError{Code: code, Message: message}})
}
