// Package httpapi exposes the Kairos application service over HTTP.
package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/ScienJus/kairos/internal/identity"
)

const maxRequestBodyBytes = 1 << 20
const DefaultMaxArtifactUploadBytes int64 = 16 << 20
const artifactMultipartOverheadBytes int64 = 1 << 20
const maxConfiguredArtifactUploadBytes int64 = (1<<63 - 1) - artifactMultipartOverheadBytes

// AuthenticationMode identifies how work requests resolve their actor.
type AuthenticationMode string

const (
	AuthenticationModeTrusted       AuthenticationMode = "trusted"
	AuthenticationModeAuthenticated AuthenticationMode = "authenticated"
)

// Options configures HTTP transport behavior.
type Options struct {
	MaxArtifactUploadBytes int64
	AuthenticationMode     AuthenticationMode
}

// Handler serves the versioned Kairos HTTP API.
type Handler struct {
	service                *application.Service
	identity               identity.Resolver
	identityManagement     *identity.Service
	adminTokenHash         [32]byte
	hasAdminToken          bool
	maxArtifactUploadBytes int64
	authenticationMode     AuthenticationMode
	mux                    *http.ServeMux
}

// New creates an HTTP handler backed by the application service.
func New(service *application.Service, resolver identity.Resolver, options ...Options) (*Handler, error) {
	if service == nil {
		return nil, errors.New("application service is required")
	}
	if resolver == nil {
		return nil, errors.New("identity resolver is required")
	}
	configured, err := httpOptions(options)
	if err != nil {
		return nil, err
	}
	handler := &Handler{
		service: service, identity: identity.WithExecutorAuthenticator(resolver, service), maxArtifactUploadBytes: configured.MaxArtifactUploadBytes,
		authenticationMode: configured.AuthenticationMode, mux: http.NewServeMux(),
	}
	handler.routes()
	return handler, nil
}

// NewWithIdentityManagement creates an API that also exposes administrator-
// protected identity and token lifecycle endpoints.
func NewWithIdentityManagement(
	service *application.Service,
	resolver identity.Resolver,
	identityService *identity.Service,
	adminToken string,
	options ...Options,
) (*Handler, error) {
	if service == nil || resolver == nil || identityService == nil {
		return nil, errors.New("application service, identity resolver and identity service are required")
	}
	if len(adminToken) < 32 || adminToken != strings.TrimSpace(adminToken) {
		return nil, errors.New("admin token must be trimmed and at least 32 characters")
	}
	configured, err := httpOptions(options)
	if err != nil {
		return nil, err
	}
	handler := &Handler{
		service: service, identity: identity.WithExecutorAuthenticator(resolver, service), identityManagement: identityService,
		adminTokenHash: sha256.Sum256([]byte(adminToken)), hasAdminToken: true,
		maxArtifactUploadBytes: configured.MaxArtifactUploadBytes,
		authenticationMode:     configured.AuthenticationMode, mux: http.NewServeMux(),
	}
	handler.routes()
	return handler, nil
}

func httpOptions(values []Options) (Options, error) {
	if len(values) > 1 {
		return Options{}, errors.New("HTTP options must be provided at most once")
	}
	if len(values) == 0 {
		return Options{MaxArtifactUploadBytes: DefaultMaxArtifactUploadBytes, AuthenticationMode: AuthenticationModeTrusted}, nil
	}
	configured := values[0]
	if configured.MaxArtifactUploadBytes == 0 {
		configured.MaxArtifactUploadBytes = DefaultMaxArtifactUploadBytes
	}
	if configured.MaxArtifactUploadBytes < 0 || configured.MaxArtifactUploadBytes > maxConfiguredArtifactUploadBytes {
		return Options{}, errors.New("max Artifact upload bytes must be positive and leave room for multipart overhead")
	}
	if configured.AuthenticationMode == "" {
		configured.AuthenticationMode = AuthenticationModeTrusted
	}
	if configured.AuthenticationMode != AuthenticationModeTrusted && configured.AuthenticationMode != AuthenticationModeAuthenticated {
		return Options{}, errors.New("authentication mode must be trusted or authenticated")
	}
	return configured, nil
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	h.mux.ServeHTTP(writer, request)
}

func (h *Handler) routes() {
	h.mux.HandleFunc("GET /healthz", h.health)
	h.mux.HandleFunc("GET /api/v1/auth/config", h.getAuthenticationConfig)
	h.mux.HandleFunc("GET /api/v1/session", h.getSession)
	if h.identityManagement != nil {
		h.mux.HandleFunc("GET /api/v1/identities", h.listIdentities)
		h.mux.HandleFunc("POST /api/v1/identities", h.createIdentity)
		h.mux.HandleFunc("GET /api/v1/identities/{kind}/{actor_id}", h.getIdentity)
		h.mux.HandleFunc("POST /api/v1/identities/{kind}/{actor_id}/token", h.rotateIdentityToken)
		h.mux.HandleFunc("DELETE /api/v1/identities/{kind}/{actor_id}/token", h.revokeIdentityToken)
	}
	h.mux.HandleFunc("GET /api/v1/definitions/workflows", h.listWorkflowDefinitions)
	h.mux.HandleFunc("GET /api/v1/definitions/workflows/{definition_id}", h.getLatestWorkflowDefinition)
	h.mux.HandleFunc("GET /api/v1/definitions/workflows/{definition_id}/versions", h.listWorkflowDefinitionVersions)
	h.mux.HandleFunc("POST /api/v1/definitions/workflows/{definition_id}/versions", h.createWorkflowDefinition)
	h.mux.HandleFunc("GET /api/v1/definitions/workflows/{definition_id}/versions/{version}", h.getWorkflowDefinition)
	h.mux.HandleFunc("GET /api/v1/definitions/blackboards", h.listBlackboardDefinitions)
	h.mux.HandleFunc("GET /api/v1/definitions/blackboards/{definition_id}", h.getLatestBlackboardDefinition)
	h.mux.HandleFunc("GET /api/v1/definitions/blackboards/{definition_id}/versions", h.listBlackboardDefinitionVersions)
	h.mux.HandleFunc("POST /api/v1/definitions/blackboards/{definition_id}/versions", h.createBlackboardDefinition)
	h.mux.HandleFunc("GET /api/v1/definitions/blackboards/{definition_id}/versions/{version}", h.getBlackboardDefinition)
	h.mux.HandleFunc("GET /api/v1/work", h.findWork)
	h.mux.HandleFunc("GET /api/v1/human-attention", h.listHumanAttention)
	h.mux.HandleFunc("GET /api/v1/work-items", h.listWorkItems)
	h.mux.HandleFunc("POST /api/v1/work-items", h.createWorkItem)
	h.mux.HandleFunc("GET /api/v1/work-items/{work_item_id}/context", h.getWorkItemContext)
	h.mux.HandleFunc("GET /api/v1/work-items/{work_item_id}/artifacts", h.listArtifacts)
	h.mux.HandleFunc("POST /api/v1/work-items/{work_item_id}/coordination-claims", h.claimWorkCandidate)
	h.mux.HandleFunc("DELETE /api/v1/work-items/{work_item_id}/coordination-claims/{claim_id}", h.releaseCoordinationClaim)
	h.mux.HandleFunc("POST /api/v1/work-items/{work_item_id}/coordination-claims/{claim_id}/heartbeat", h.heartbeatCoordinationClaim)
	h.mux.HandleFunc("POST /api/v1/work-items/{work_item_id}/tasks", h.createBlackboardTask)
	h.mux.HandleFunc("POST /api/v1/work-items/{work_item_id}/relations", h.addBlackboardRelation)
	h.mux.HandleFunc("POST /api/v1/work-items/{work_item_id}/completion", h.submitBlackboardCompletion)
	h.mux.HandleFunc("POST /api/v1/work-items/{work_item_id}/acceptance", h.acceptBlackboardCompletion)
	h.mux.HandleFunc("POST /api/v1/work-items/{work_item_id}/cancellation", h.cancelWorkItem)
	h.mux.HandleFunc("GET /api/v1/tasks/{task_id}/context", h.getTaskContext)
	h.mux.HandleFunc("GET /api/v1/tasks/{task_id}", h.getTaskDetail)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/claims", h.claimTask)
	h.mux.HandleFunc("DELETE /api/v1/tasks/{task_id}/claims/{claim_id}", h.releaseClaim)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/claims/{claim_id}/heartbeat", h.heartbeatClaim)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/submissions", h.submitTask)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/artifacts", h.createArtifact)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/artifact-uploads", h.uploadArtifact)
	h.mux.HandleFunc("GET /api/v1/artifacts/{artifact_id}/content", h.getArtifactContent)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/failures", h.failTask)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/skip", h.skipBlackboardTask)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/decomposition", h.decomposeBlackboardTask)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/children", h.addBlackboardChildTask)
	h.mux.HandleFunc("POST /api/v1/tasks/{task_id}/reviews/{review_id}/decision", h.decideReview)
}

func (h *Handler) getAuthenticationConfig(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, dataResponse{Data: struct {
		Mode AuthenticationMode `json:"mode"`
	}{Mode: h.authenticationMode}})
}

func (h *Handler) getSession(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	if err := actor.Validate(); err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: struct {
		ID   domain.ActorID   `json:"id"`
		Kind domain.ActorKind `json:"kind"`
		Role string           `json:"role"`
	}{ID: actor.Actor.ID, Kind: actor.Actor.Kind, Role: actor.Role}})
}

func (h *Handler) getTaskDetail(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	detail, err := h.service.GetTaskDetail(request.Context(), application.GetTaskDetailQuery{TaskID: domain.TaskID(request.PathValue("task_id")), Identity: actor})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: detail})
}

func (h *Handler) listHumanAttention(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	pageRequest, err := parsePageRequest(request, "human_attention", func(cursor application.HumanAttentionCursor) bool {
		return (cursor.Priority == 0 || cursor.Priority == 1) && !cursor.UpdatedAt.IsZero() && strings.TrimSpace(string(cursor.WorkItemID)) != ""
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	page, err := h.service.ListHumanAttention(request.Context(), actor, pageRequest)
	if err != nil {
		writeError(writer, err)
		return
	}
	nextCursor, err := nextPageCursor(page, "human_attention", func(item application.HumanAttentionItem) application.HumanAttentionCursor {
		return item.Cursor()
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, pageResponse{Data: page.Items, NextCursor: nextCursor})
}

func (h *Handler) listWorkItems(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	statuses := make([]domain.WorkItemStatus, 0, len(request.URL.Query()["status"]))
	for _, status := range request.URL.Query()["status"] {
		statuses = append(statuses, domain.WorkItemStatus(status))
	}
	modes := make([]domain.CoordinationMode, 0, len(request.URL.Query()["mode"]))
	for _, mode := range request.URL.Query()["mode"] {
		modes = append(modes, domain.CoordinationMode(mode))
	}
	pageRequest, err := parsePageRequest(request, "work_items", func(cursor application.WorkItemCursor) bool {
		return !cursor.UpdatedAt.IsZero() && strings.TrimSpace(string(cursor.ID)) != ""
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	page, err := h.service.ListWorkItems(request.Context(), application.ListWorkItemsQuery{
		Identity: actor, Statuses: statuses, Modes: modes, Tags: request.URL.Query()["tag"], Page: pageRequest,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	nextCursor, err := nextPageCursor(page, "work_items", func(item domain.WorkItem) application.WorkItemCursor {
		return application.WorkItemCursor{UpdatedAt: item.UpdatedAt, ID: item.ID}
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, pageResponse{Data: page.Items, NextCursor: nextCursor})
}

func (h *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

type definitionMetadataRequest struct {
	BaseVersion       *int64   `json:"base_version"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	AgentInstructions string   `json:"agent_instructions"`
	SuggestedTags     []string `json:"suggested_tags"`
}

func (r definitionMetadataRequest) command() application.DefinitionMetadataCommand {
	return application.DefinitionMetadataCommand{
		Name: r.Name, Description: r.Description,
		AgentInstructions: r.AgentInstructions, SuggestedTags: r.SuggestedTags,
	}
}

type workflowDefinitionRequest struct {
	definitionMetadataRequest
	Graph workflowGraphRequest `json:"graph"`
}

type workflowGraphRequest struct {
	StartTaskIDs      []domain.WorkflowTaskID         `json:"start_task_ids"`
	Tasks             []workflowTaskDefinitionRequest `json:"tasks"`
	Relations         []workflowRelationRequest       `json:"relations"`
	MaxTaskExecutions int                             `json:"max_task_executions"`
}

type workflowTaskDefinitionRequest struct {
	ID                 domain.WorkflowTaskID       `json:"id"`
	Title              string                      `json:"title"`
	Description        string                      `json:"description"`
	AcceptanceCriteria string                      `json:"acceptance_criteria"`
	Executor           domain.ExecutorRequirement  `json:"executor"`
	AllowedRoles       []string                    `json:"allowed_roles"`
	Execution          domain.ExecutionPolicy      `json:"execution"`
	ReviewPolicy       domain.ReviewPolicy         `json:"review_policy"`
	DefaultTags        []string                    `json:"default_tags"`
	Artifacts          []artifactDefinitionRequest `json:"artifacts"`
}

type artifactDefinitionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type workflowRelationRequest struct {
	ID            domain.WorkflowRelationID `json:"id"`
	FromTaskID    domain.WorkflowTaskID     `json:"from_task_id"`
	ToTaskID      domain.WorkflowTaskID     `json:"to_task_id"`
	Label         string                    `json:"label"`
	AgentGuidance string                    `json:"agent_guidance"`
}

func (r workflowGraphRequest) domainGraph() domain.WorkflowGraph {
	tasks := make([]domain.WorkflowTaskDefinition, 0, len(r.Tasks))
	for _, task := range r.Tasks {
		artifacts := make([]domain.ArtifactDefinition, 0, len(task.Artifacts))
		for _, artifact := range task.Artifacts {
			artifacts = append(artifacts, domain.ArtifactDefinition{Name: strings.TrimSpace(artifact.Name), Description: strings.TrimSpace(artifact.Description)})
		}
		tasks = append(tasks, domain.WorkflowTaskDefinition{
			ID: task.ID, Title: task.Title, Description: task.Description,
			AcceptanceCriteria: task.AcceptanceCriteria, Executor: task.Executor,
			AllowedRoles: task.AllowedRoles, Execution: task.Execution,
			ReviewPolicy: task.ReviewPolicy, DefaultTags: task.DefaultTags, Artifacts: artifacts,
		})
	}
	relations := make([]domain.WorkflowRelationDefinition, 0, len(r.Relations))
	for _, relation := range r.Relations {
		relations = append(relations, domain.WorkflowRelationDefinition{
			ID: relation.ID, FromTaskID: relation.FromTaskID, ToTaskID: relation.ToTaskID,
			Label: strings.TrimSpace(relation.Label), AgentGuidance: strings.TrimSpace(relation.AgentGuidance),
		})
	}
	return domain.WorkflowGraph{
		StartTaskIDs: r.StartTaskIDs, Tasks: tasks, Relations: relations,
		MaxTaskExecutions: r.MaxTaskExecutions,
	}
}

func (h *Handler) createWorkflowDefinition(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body workflowDefinitionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	metadata := body.definitionMetadataRequest.command()
	metadata.ID = domain.DefinitionID(request.PathValue("definition_id"))
	definition, err := h.service.CreateWorkflowDefinition(request.Context(), application.CreateWorkflowDefinitionCommand{
		Identity: actor, BaseVersion: body.BaseVersion, Metadata: metadata,
		Graph: body.Graph.domainGraph(),
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: definition})
}

func (h *Handler) createBlackboardDefinition(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body definitionMetadataRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	metadata := body.command()
	metadata.ID = domain.DefinitionID(request.PathValue("definition_id"))
	definition, err := h.service.CreateBlackboardDefinition(request.Context(), application.CreateBlackboardDefinitionCommand{
		Identity: actor, BaseVersion: body.BaseVersion, Metadata: metadata,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: definition})
}

func (h *Handler) listWorkflowDefinitions(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	pageRequest, err := parsePageRequest(request, "workflow_definition_catalog", func(cursor application.DefinitionCatalogCursor) bool {
		return strings.TrimSpace(string(cursor.ID)) != ""
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	page, err := h.service.ListWorkflowDefinitionCatalog(request.Context(), actor, application.DefinitionCatalogFilter{
		Page: pageRequest,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	nextCursor, err := nextPageCursor(page, "workflow_definition_catalog", func(item domain.WorkflowDefinition) application.DefinitionCatalogCursor {
		return application.DefinitionCatalogCursor{ID: item.ID}
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, pageResponse{Data: page.Items, NextCursor: nextCursor})
}

func (h *Handler) listBlackboardDefinitions(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	pageRequest, err := parsePageRequest(request, "blackboard_definition_catalog", func(cursor application.DefinitionCatalogCursor) bool {
		return strings.TrimSpace(string(cursor.ID)) != ""
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	page, err := h.service.ListBlackboardDefinitionCatalog(request.Context(), actor, application.DefinitionCatalogFilter{
		Page: pageRequest,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	nextCursor, err := nextPageCursor(page, "blackboard_definition_catalog", func(item domain.BlackboardDefinition) application.DefinitionCatalogCursor {
		return application.DefinitionCatalogCursor{ID: item.ID}
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, pageResponse{Data: page.Items, NextCursor: nextCursor})
}

func (h *Handler) getLatestWorkflowDefinition(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	definition, err := h.service.GetLatestWorkflowDefinition(request.Context(), actor, domain.DefinitionID(request.PathValue("definition_id")))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: definition})
}

func (h *Handler) getLatestBlackboardDefinition(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	definition, err := h.service.GetLatestBlackboardDefinition(request.Context(), actor, domain.DefinitionID(request.PathValue("definition_id")))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: definition})
}

func (h *Handler) listWorkflowDefinitionVersions(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	pageRequest, err := parsePageRequest(request, "workflow_definition_versions", validDefinitionVersionCursor)
	if err != nil {
		writeError(writer, err)
		return
	}
	page, err := h.service.ListWorkflowDefinitionVersions(request.Context(), actor, application.DefinitionVersionFilter{
		ID: domain.DefinitionID(request.PathValue("definition_id")), Page: pageRequest,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	nextCursor, err := nextPageCursor(page, "workflow_definition_versions", func(item domain.WorkflowDefinition) application.DefinitionVersionCursor {
		return application.DefinitionVersionCursor{Version: item.Version}
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, pageResponse{Data: page.Items, NextCursor: nextCursor})
}

func (h *Handler) listBlackboardDefinitionVersions(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	pageRequest, err := parsePageRequest(request, "blackboard_definition_versions", validDefinitionVersionCursor)
	if err != nil {
		writeError(writer, err)
		return
	}
	page, err := h.service.ListBlackboardDefinitionVersions(request.Context(), actor, application.DefinitionVersionFilter{
		ID: domain.DefinitionID(request.PathValue("definition_id")), Page: pageRequest,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	nextCursor, err := nextPageCursor(page, "blackboard_definition_versions", func(item domain.BlackboardDefinition) application.DefinitionVersionCursor {
		return application.DefinitionVersionCursor{Version: item.Version}
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, pageResponse{Data: page.Items, NextCursor: nextCursor})
}

func validDefinitionVersionCursor(cursor application.DefinitionVersionCursor) bool {
	return cursor.Version > 0
}

func (h *Handler) getWorkflowDefinition(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	version, ok := definitionVersion(writer, request)
	if !ok {
		return
	}
	definition, err := h.service.GetWorkflowDefinition(request.Context(), application.GetDefinitionQuery{
		Identity: actor, ID: domain.DefinitionID(request.PathValue("definition_id")), Version: version,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: definition})
}

func (h *Handler) getBlackboardDefinition(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	version, ok := definitionVersion(writer, request)
	if !ok {
		return
	}
	definition, err := h.service.GetBlackboardDefinition(request.Context(), application.GetDefinitionQuery{
		Identity: actor, ID: domain.DefinitionID(request.PathValue("definition_id")), Version: version,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: definition})
}

func definitionVersion(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	version, err := strconv.ParseInt(request.PathValue("version"), 10, 64)
	if err != nil || version <= 0 {
		writeError(writer, fmt.Errorf("%w: definition version must be a positive integer", application.ErrInvalidCommand))
		return 0, false
	}
	return version, true
}

func (h *Handler) findWork(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	limit := 0
	if raw := request.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeError(writer, fmt.Errorf("%w: limit must be an integer", application.ErrInvalidCommand))
			return
		}
		limit = value
	}
	candidates, err := h.service.FindWork(request.Context(), application.FindWorkQuery{
		Identity: actor,
		Tags:     request.URL.Query()["tag"],
		Limit:    limit,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	if candidates == nil {
		candidates = []application.WorkCandidate{}
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: candidates})
}

type createWorkItemRequest struct {
	DefinitionID       domain.DefinitionID           `json:"definition_id"`
	Mode               domain.CoordinationMode       `json:"mode"`
	Title              string                        `json:"title"`
	Goal               string                        `json:"goal"`
	Context            string                        `json:"context"`
	Constraints        string                        `json:"constraints"`
	AcceptanceCriteria string                        `json:"acceptance_criteria"`
	AcceptanceMode     domain.WorkItemAcceptanceMode `json:"acceptance_mode"`
	Tags               []string                      `json:"tags"`
}

func (h *Handler) createWorkItem(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body createWorkItemRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	created, err := h.service.CreateWorkItem(request.Context(), application.CreateWorkItemCommand{
		Definition: domain.DefinitionBinding{ID: body.DefinitionID, Mode: body.Mode},
		Identity:   actor, OperationID: operationID(request),
		Title: body.Title, Goal: body.Goal, Context: body.Context,
		Constraints: body.Constraints, AcceptanceCriteria: body.AcceptanceCriteria, AcceptanceMode: body.AcceptanceMode, Tags: body.Tags,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: created})
}

func (h *Handler) getWorkItemContext(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	result, err := h.service.GetWorkItemExecutionContext(request.Context(), application.GetWorkItemExecutionContextQuery{
		WorkItemID: domain.WorkItemID(request.PathValue("work_item_id")),
		Identity:   actor,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: result})
}

type createBlackboardTaskRequest struct {
	CoordinationClaimID domain.CoordinationClaimID `json:"coordination_claim_id"`
	Title               string                     `json:"title"`
	Description         string                     `json:"description"`
	AcceptanceCriteria  string                     `json:"acceptance_criteria"`
	Executor            domain.ExecutorRequirement `json:"executor"`
	AllowedRoles        []string                   `json:"allowed_roles"`
	Tags                []string                   `json:"tags"`
}

func (r createBlackboardTaskRequest) spec() application.BlackboardTaskSpec {
	return application.BlackboardTaskSpec{
		Title: r.Title, Description: r.Description, AcceptanceCriteria: r.AcceptanceCriteria,
		Executor: r.Executor, AllowedRoles: r.AllowedRoles, Tags: r.Tags,
	}
}

func (h *Handler) createBlackboardTask(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body createBlackboardTaskRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	created, err := h.service.CreateBlackboardTask(request.Context(), application.CreateBlackboardTaskCommand{
		WorkItemID:          domain.WorkItemID(request.PathValue("work_item_id")),
		CoordinationClaimID: body.CoordinationClaimID, Identity: actor, OperationID: operationID(request),
		Title: body.Title, Description: body.Description, AcceptanceCriteria: body.AcceptanceCriteria,
		Executor: body.Executor, AllowedRoles: body.AllowedRoles, Tags: body.Tags,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: created})
}

type addBlackboardRelationRequest struct {
	FromTaskID domain.TaskID `json:"from_task_id"`
	ToTaskID   domain.TaskID `json:"to_task_id"`
}

func (h *Handler) addBlackboardRelation(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body addBlackboardRelationRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	relation, err := h.service.AddBlackboardRelation(request.Context(), application.AddBlackboardRelationCommand{
		WorkItemID: domain.WorkItemID(request.PathValue("work_item_id")), FromTaskID: body.FromTaskID,
		ToTaskID: body.ToTaskID, Identity: actor,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: relation})
}

type submitBlackboardCompletionRequest struct {
	CoordinationClaimID domain.CoordinationClaimID `json:"coordination_claim_id"`
	Result              string                     `json:"result"`
}

func (h *Handler) submitBlackboardCompletion(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body submitBlackboardCompletionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	workItem, err := h.service.SubmitBlackboardCompletion(request.Context(), application.SubmitBlackboardCompletionCommand{
		WorkItemID: domain.WorkItemID(request.PathValue("work_item_id")), CoordinationClaimID: body.CoordinationClaimID, Identity: actor,
		Result: body.Result,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: workItem})
}

func (h *Handler) acceptBlackboardCompletion(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body struct {
		CoordinationClaimID domain.CoordinationClaimID `json:"coordination_claim_id"`
	}
	if request.ContentLength != 0 && !decodeRequest(writer, request, &body) {
		return
	}
	workItem, err := h.service.AcceptBlackboardCompletion(request.Context(), application.AcceptBlackboardCompletionCommand{
		WorkItemID: domain.WorkItemID(request.PathValue("work_item_id")), CoordinationClaimID: body.CoordinationClaimID, Identity: actor,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: workItem})
}

func (h *Handler) claimWorkCandidate(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body struct {
		Kind          application.WorkCandidateKind `json:"kind"`
		LeaseSeconds  int64                         `json:"lease_seconds"`
		ExecutorToken string                        `json:"executor_token"`
	}
	if !decodeRequest(writer, request, &body) {
		return
	}
	claim, err := h.service.ClaimWorkCandidate(request.Context(), application.ClaimWorkCandidateCommand{
		WorkItemID: domain.WorkItemID(request.PathValue("work_item_id")), Kind: body.Kind, Identity: actor,
		OperationID: operationID(request), LeaseSeconds: body.LeaseSeconds, ExecutorToken: body.ExecutorToken,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: claim})
}

func (h *Handler) heartbeatCoordinationClaim(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body struct {
		LeaseSeconds int64 `json:"lease_seconds"`
	}
	if request.ContentLength != 0 && !decodeRequest(writer, request, &body) {
		return
	}
	claim, err := h.service.HeartbeatCoordinationClaim(request.Context(), application.HeartbeatCoordinationClaimCommand{
		WorkItemID: domain.WorkItemID(request.PathValue("work_item_id")),
		ClaimID:    domain.CoordinationClaimID(request.PathValue("claim_id")), Identity: actor, LeaseSeconds: body.LeaseSeconds,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: claim})
}

func (h *Handler) releaseCoordinationClaim(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	err := h.service.ReleaseCoordinationClaim(request.Context(), application.ReleaseCoordinationClaimCommand{
		WorkItemID: domain.WorkItemID(request.PathValue("work_item_id")),
		ClaimID:    domain.CoordinationClaimID(request.PathValue("claim_id")), Identity: actor,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

type cancelWorkItemRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) cancelWorkItem(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body cancelWorkItemRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	workItem, err := h.service.CancelWorkItem(request.Context(), application.CancelWorkItemCommand{
		WorkItemID: domain.WorkItemID(request.PathValue("work_item_id")), Identity: actor,
		Reason: body.Reason,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: workItem})
}

func (h *Handler) getTaskContext(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	context, err := h.service.GetTaskExecutionContext(request.Context(), application.GetTaskExecutionContextQuery{
		TaskID: domain.TaskID(request.PathValue("task_id")), Identity: actor,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: context})
}

func (h *Handler) claimTask(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body struct {
		LeaseSeconds  int64  `json:"lease_seconds"`
		ExecutorToken string `json:"executor_token"`
	}
	if request.ContentLength != 0 && !decodeRequest(writer, request, &body) {
		return
	}
	claim, err := h.service.ClaimTask(request.Context(), application.ClaimTaskCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), Identity: actor, OperationID: operationID(request), LeaseSeconds: body.LeaseSeconds, ExecutorToken: body.ExecutorToken,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: claim})
}

func (h *Handler) releaseClaim(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if request.ContentLength != 0 && !decodeRequest(writer, request, &body) {
		return
	}
	err := h.service.ReleaseClaim(request.Context(), application.ReleaseClaimCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), ClaimID: domain.ClaimID(request.PathValue("claim_id")),
		Identity: actor, Reason: body.Reason,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *Handler) heartbeatClaim(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body struct {
		LeaseSeconds int64 `json:"lease_seconds"`
	}
	if request.ContentLength != 0 && !decodeRequest(writer, request, &body) {
		return
	}
	claim, err := h.service.HeartbeatClaim(request.Context(), application.HeartbeatClaimCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), ClaimID: domain.ClaimID(request.PathValue("claim_id")), Identity: actor, LeaseSeconds: body.LeaseSeconds,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: claim})
}

type submitTaskRequest struct {
	ClaimID       domain.ClaimID             `json:"claim_id"`
	Result        string                     `json:"result"`
	ArtifactIDs   []domain.ArtifactID        `json:"artifact_ids"`
	RequestReview bool                       `json:"request_review"`
	Transition    *workflowTransitionRequest `json:"transition"`
}

type workflowTransitionRequest struct {
	ChoiceGroupID        domain.WorkflowChoiceGroupID `json:"choice_group_id"`
	SkipOptionalTaskIDs  []domain.WorkflowTaskID      `json:"skip_optional_task_ids"`
	ReviewSkippedTaskIDs []domain.WorkflowTaskID      `json:"review_skipped_task_ids"`
	Reason               string                       `json:"reason"`
}

func (r *workflowTransitionRequest) command() *application.WorkflowTransitionCommand {
	if r == nil {
		return nil
	}
	return &application.WorkflowTransitionCommand{
		ChoiceGroupID: r.ChoiceGroupID, SkipOptionalTaskIDs: r.SkipOptionalTaskIDs,
		ReviewSkippedTaskIDs: r.ReviewSkippedTaskIDs, Reason: r.Reason,
	}
}

func (h *Handler) submitTask(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body submitTaskRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	submission, err := h.service.SubmitTask(request.Context(), application.SubmitTaskCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), ClaimID: body.ClaimID,
		Identity: actor, Result: body.Result,
		ArtifactIDs:   body.ArtifactIDs,
		RequestReview: body.RequestReview, Transition: body.Transition.command(),
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: submission})
}

type createArtifactRequest struct {
	ClaimID domain.ClaimID `json:"claim_id"`
	Name    string         `json:"name"`
	URI     string         `json:"uri"`
}

func (h *Handler) createArtifact(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body createArtifactRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	artifact, err := h.service.CreateArtifact(request.Context(), application.CreateArtifactCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), ClaimID: body.ClaimID,
		Identity: actor, OperationID: operationID(request), Name: body.Name, URI: body.URI,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: artifact})
}

func (h *Handler) uploadArtifact(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, h.maxArtifactUploadBytes+artifactMultipartOverheadBytes)
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(writer, http.StatusRequestEntityTooLarge, errorResponse{Error: apiError{
				Code: "artifact_too_large", Message: fmt.Sprintf("artifact upload exceeds %d bytes", h.maxArtifactUploadBytes),
			}})
			return
		}
		writeError(writer, application.ErrInvalidCommand)
		return
	}
	defer request.MultipartForm.RemoveAll()
	file, header, err := request.FormFile("file")
	if err != nil {
		writeError(writer, application.ErrInvalidCommand)
		return
	}
	defer file.Close()
	if header.Size > h.maxArtifactUploadBytes {
		writeJSON(writer, http.StatusRequestEntityTooLarge, errorResponse{Error: apiError{
			Code: "artifact_too_large", Message: fmt.Sprintf("artifact file exceeds %d bytes", h.maxArtifactUploadBytes),
		}})
		return
	}
	artifact, err := h.service.UploadArtifact(request.Context(), application.UploadArtifactCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), ClaimID: domain.ClaimID(request.FormValue("claim_id")),
		Identity: actor, OperationID: operationID(request), Name: request.FormValue("name"),
	}, file)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: artifact})
}

func (h *Handler) listArtifacts(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	pageRequest, err := parsePageRequest(request, "artifacts", func(cursor application.ArtifactCursor) bool {
		return !cursor.CreatedAt.IsZero() && strings.TrimSpace(string(cursor.ID)) != ""
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	page, err := h.service.ListArtifacts(request.Context(), domain.WorkItemID(request.PathValue("work_item_id")), actor, pageRequest)
	if err != nil {
		writeError(writer, err)
		return
	}
	nextCursor, err := nextPageCursor(page, "artifacts", func(item domain.Artifact) application.ArtifactCursor {
		return application.ArtifactCursor{CreatedAt: item.CreatedAt, ID: item.ID}
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, pageResponse{Data: page.Items, NextCursor: nextCursor})
}

func (h *Handler) getArtifactContent(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	artifact, content, err := h.service.OpenArtifact(request.Context(), domain.ArtifactID(request.PathValue("artifact_id")), actor)
	if err != nil {
		writeError(writer, err)
		return
	}
	defer content.Close()
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": artifact.Name}))
	if _, err := io.Copy(writer, content); err != nil {
		return
	}
}

type failTaskRequest struct {
	ClaimID     domain.ClaimID           `json:"claim_id"`
	Action      domain.TaskFailureAction `json:"action"`
	Reason      string                   `json:"reason"`
	RetryPrompt string                   `json:"retry_prompt"`
}

func (h *Handler) failTask(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body failTaskRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	failure, err := h.service.FailTask(request.Context(), application.FailTaskCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), ClaimID: body.ClaimID,
		Identity: actor, Action: body.Action,
		Reason: body.Reason, RetryPrompt: body.RetryPrompt,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: failure})
}

type skipBlackboardTaskRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) skipBlackboardTask(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body skipBlackboardTaskRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	task, err := h.service.SkipBlackboardTask(request.Context(), application.SkipBlackboardTaskCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), Identity: actor,
		Reason: body.Reason,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: task})
}

type decomposeBlackboardTaskRequest struct {
	ClaimID  domain.ClaimID                `json:"claim_id"`
	Children []createBlackboardTaskRequest `json:"children"`
}

func (h *Handler) decomposeBlackboardTask(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body decomposeBlackboardTaskRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	children := make([]application.BlackboardTaskSpec, 0, len(body.Children))
	for _, child := range body.Children {
		children = append(children, child.spec())
	}
	decomposition, err := h.service.DecomposeBlackboardTask(request.Context(), application.DecomposeBlackboardTaskCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), ClaimID: body.ClaimID,
		Identity: actor, OperationID: operationID(request), Children: children,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: decomposition})
}

func (h *Handler) addBlackboardChildTask(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body createBlackboardTaskRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	child, err := h.service.AddBlackboardChildTask(request.Context(), application.AddBlackboardChildTaskCommand{
		ParentTaskID: domain.TaskID(request.PathValue("task_id")), Identity: actor,
		OperationID: operationID(request), Task: body.spec(),
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, dataResponse{Data: child})
}

type decideReviewRequest struct {
	Decision domain.ReviewStatus `json:"decision"`
	Feedback string              `json:"feedback"`
}

func (h *Handler) decideReview(writer http.ResponseWriter, request *http.Request) {
	actor, ok := h.resolveIdentity(writer, request)
	if !ok {
		return
	}
	var body decideReviewRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	review, err := h.service.DecideReview(request.Context(), application.DecideReviewCommand{
		TaskID: domain.TaskID(request.PathValue("task_id")), ReviewID: domain.ReviewID(request.PathValue("review_id")),
		Identity: actor, Decision: body.Decision, Feedback: body.Feedback,
	})
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, dataResponse{Data: review})
}

func (h *Handler) resolveIdentity(writer http.ResponseWriter, request *http.Request) (identity.Identity, bool) {
	actor, err := h.identity.Resolve(request)
	if err != nil {
		writeError(writer, err)
		return identity.Identity{}, false
	}
	return actor, true
}

func operationID(request *http.Request) string {
	return strings.TrimSpace(request.Header.Get("Idempotency-Key"))
}

type dataResponse struct {
	Data any `json:"data"`
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeRequest(writer http.ResponseWriter, request *http.Request, target any) bool {
	reader := http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(writer, fmt.Errorf("%w: invalid JSON body: %v", application.ErrInvalidCommand, err))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, fmt.Errorf("%w: body must contain one JSON object", application.ErrInvalidCommand))
		return false
	}
	return true
}

func writeError(writer http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	switch {
	case errors.Is(err, identity.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, "unauthenticated"
	case errors.Is(err, identity.ErrInvalid), errors.Is(err, application.ErrInvalidCommand), errors.Is(err, domain.ErrInvalidModel):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, identity.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, identity.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, application.ErrForbidden):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, application.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, application.ErrWorkItemCancelled):
		status, code = http.StatusConflict, "work_item_cancelled"
	case errors.Is(err, application.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = "internal server error"
	}
	writeJSON(writer, status, errorResponse{Error: apiError{Code: code, Message: message}})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
