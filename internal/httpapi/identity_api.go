package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

type createIdentityRequest struct {
	ID   domain.ActorID   `json:"id"`
	Kind domain.ActorKind `json:"kind"`
	Role string           `json:"role"`
}

type identityResponse struct {
	ID          domain.ActorID   `json:"id"`
	Kind        domain.ActorKind `json:"kind"`
	Role        string           `json:"role"`
	TokenActive bool             `json:"token_active"`
	Version     int64            `json:"version"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

type issuedTokenResponse struct {
	ID    domain.ActorID   `json:"id"`
	Kind  domain.ActorKind `json:"kind"`
	Role  string           `json:"role"`
	Token string           `json:"token"`
}

func (h *Handler) createIdentity(writer http.ResponseWriter, request *http.Request) {
	if !h.authorizeAdmin(writer, request) {
		return
	}
	var body createIdentityRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	issued, err := h.identityManagement.CreateIdentity(request.Context(), domain.ActorRef{Kind: body.Kind, ID: body.ID}, body.Role)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: issuedTokenDTO(issued)})
}

func (h *Handler) listIdentities(writer http.ResponseWriter, request *http.Request) {
	if !h.authorizeAdmin(writer, request) {
		return
	}
	records, err := h.identityManagement.ListIdentities(request.Context())
	if err != nil {
		writeError(writer, err)
		return
	}
	result := make([]identityResponse, 0, len(records))
	for _, record := range records {
		result = append(result, identityDTO(record))
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: result})
}

func (h *Handler) getIdentity(writer http.ResponseWriter, request *http.Request) {
	if !h.authorizeAdmin(writer, request) {
		return
	}
	record, err := h.identityManagement.GetIdentity(request.Context(), identityActor(request))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: identityDTO(record)})
}

func (h *Handler) rotateIdentityToken(writer http.ResponseWriter, request *http.Request) {
	if !h.authorizeAdmin(writer, request) {
		return
	}
	issued, err := h.identityManagement.RotateToken(request.Context(), identityActor(request))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: issuedTokenDTO(issued)})
}

func (h *Handler) revokeIdentityToken(writer http.ResponseWriter, request *http.Request) {
	if !h.authorizeAdmin(writer, request) {
		return
	}
	if err := h.identityManagement.RevokeToken(request.Context(), identityActor(request)); err != nil {
		writeError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *Handler) authorizeAdmin(writer http.ResponseWriter, request *http.Request) bool {
	if !h.hasAdminToken {
		writeError(writer, identity.ErrUnauthenticated)
		return false
	}
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		writeError(writer, identity.ErrUnauthenticated)
		return false
	}
	provided := sha256.Sum256([]byte(parts[1]))
	if subtle.ConstantTimeCompare(provided[:], h.adminTokenHash[:]) != 1 {
		writeError(writer, identity.ErrUnauthenticated)
		return false
	}
	return true
}

func identityActor(request *http.Request) domain.ActorRef {
	return domain.ActorRef{
		Kind: domain.ActorKind(request.PathValue("kind")),
		ID:   domain.ActorID(request.PathValue("actor_id")),
	}
}

func identityDTO(record identity.Record) identityResponse {
	return identityResponse{
		ID: record.Identity.Actor.ID, Kind: record.Identity.Actor.Kind, Role: record.Identity.Role,
		TokenActive: record.TokenActive, Version: record.Version,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func issuedTokenDTO(issued identity.IssuedToken) issuedTokenResponse {
	return issuedTokenResponse{
		ID: issued.Identity.Actor.ID, Kind: issued.Identity.Actor.Kind,
		Role: issued.Identity.Role, Token: issued.Token,
	}
}
