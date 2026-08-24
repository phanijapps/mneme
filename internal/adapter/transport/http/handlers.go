package http

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// Handlers bundles the service ports the REST surface calls.
type Handlers struct {
	Memories  port.MemoryService
	Recall    port.RecallService
	Sessions  port.SessionService
	Spaces    port.SpaceService
	Lifecycle port.LifecycleService

	validate *validator.Validate
}

// NewHandlers constructs handlers over the five service ports.
func NewHandlers(m port.MemoryService, rc port.RecallService, s port.SessionService, sp port.SpaceService, l port.LifecycleService) *Handlers {
	v := validator.New()
	// The DTO and domain Memory tags use the custom tag_name rule
	// (pgvector-data-model §3.0 DOMAIN); validator has no builtin for it.
	_ = v.RegisterValidation("tag_name", func(fl validator.FieldLevel) bool {
		return domain.ValidTagName(fl.Field().String())
	}, false)
	return &Handlers{Memories: m, Recall: rc, Sessions: s, Spaces: sp, Lifecycle: l, validate: v}
}

func (h *Handlers) validateDTO(w http.ResponseWriter, r *http.Request, dto any) bool {
	if err := h.validate.Struct(dto); err != nil {
		fields := map[string]any{"fields": validationFields(err)}
		WriteError(w, r, &domain.Error{Code: domain.CodeValidationErr, Message: "schema validation failed", Details: fields})
		return false
	}
	return true
}

// validationFields flattens validator.FieldError list into {field: reason}.
func validationFields(err error) map[string]string {
	out := map[string]string{}
	if ves, ok := err.(validator.ValidationErrors); ok {
		for _, fe := range ves {
			out[fe.Field()] = fe.Tag()
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Memory handlers
// ---------------------------------------------------------------------------

// CreateMemory handles POST /api/v1/memories.
func (h *Handlers) CreateMemory(w http.ResponseWriter, r *http.Request) {
	var req CreateMemoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	format := req.ContentFormat
	if format == "" {
		format = string(domain.ContentFormatMarkdown)
	}
	origin, err := domain.ParseOrigin(req.Provenance.Origin)
	if err != nil {
		WriteError(w, r, domain.NewValidationError("provenance.origin", req.Provenance.Origin, "agent_observation|user_instruction|file_artifact|consolidation"))
		return
	}
	mtype, _ := domain.ParseMemoryType(req.Type)
	scope := domain.AccessScopeIndividual
	if req.AccessScope == string(domain.AccessScopeShared) {
		scope = domain.AccessScopeShared
	}
	// Server-stamp provenance from the authenticated principal (F21).
	principal, _ := PrincipalFromContext(r.Context())
	m := &domain.Memory{
		Type:          mtype,
		Content:       req.Content,
		ContentFormat: domain.ContentFormat(format),
		Tags:          req.Tags,
		Origin:        origin,
		SessionID:     req.SessionID,
		AccessScope:   scope,
		SharedSpaceID: req.SharedSpaceID,
		Version:       1,
	}
	if principal.ID != "" {
		m.AgentID = &principal.ID
		m.Actor = &principal.ID
		m.OwnerPrincipalType = principal.Type
		m.OwnerPrincipalID = principal.ID
	}
	if req.Confidence != nil {
		m.Confidence = req.Confidence
	}
	if req.TTLExpiresAt != nil {
		m.TTLExpiresAt = req.TTLExpiresAt
	}
	if req.SourceRef != nil {
		m.SourceRef = &domain.SourceRef{Kind: req.SourceRef.Kind, Path: req.SourceRef.Path, URI: req.SourceRef.URI, Hash: req.SourceRef.Hash}
	}
	if req.Validity != nil && req.Validity.ValidFrom != nil {
		m.ValidFrom = req.Validity.ValidFrom
	}
	if !strings.HasPrefix(format, "episodic") && req.Embedding == nil {
		// server computes embedding when omitted; nothing to do here
		_ = format
	}
	saved, err := h.Memories.SaveMemory(r.Context(), m)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/memories/"+saved.ID.String())
	writeJSON(w, http.StatusCreated, saved)
}

// GetMemory handles GET /api/v1/memories/{id}.
func (h *Handlers) GetMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	m, err := h.Memories.GetMemory(r.Context(), id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// ListMemories handles GET /api/v1/memories.
func (h *Handlers) ListMemories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := []port.MemoryFilterOption{}
	if types := csvNonEmpty(q.Get("type")); len(types) > 0 {
		var mts []domain.MemoryType
		for _, t := range types {
			mt, err := domain.ParseMemoryType(t)
			if err != nil {
				WriteError(w, r, domain.NewValidationError("type", t, "episodic|semantic|procedural"))
				return
			}
			mts = append(mts, mt)
		}
		opts = append(opts, port.WithTypes(mts...))
	}
	if q.Get("q") != "" {
		opts = append(opts, port.WithTextQuery(q.Get("q")))
	}
	limit := 50
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 200 {
		limit = 200
	}
	opts = append(opts, port.WithPagination(limit, q.Get("cursor")))
	if tags := csvNonEmpty(q.Get("tags")); len(tags) > 0 {
		match := port.TagsMatchAny
		if q.Get("tags_match") == "all" {
			match = port.TagsMatchAll
		}
		opts = append(opts, port.WithTags(match, tags...))
	}
	if va := q.Get("valid_at"); va != "" {
		if t, err := time.Parse(time.RFC3339, va); err == nil {
			opts = append(opts, port.WithValidAt(t))
		}
	}
	page, err := h.Memories.ListMemories(r.Context(), port.NewMemoryFilter(opts...))
	if err != nil {
		WriteError(w, r, err)
		return
	}
	items := make([]any, len(page.Items))
	for i, m := range page.Items {
		items[i] = m
	}
	writeJSON(w, http.StatusOK, PageResponse{Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore, TotalEstimate: nil})
}

// UpdateMemory handles PUT /api/v1/memories/{id}.
func (h *Handlers) UpdateMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateMemoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	current, err := h.Memories.GetMemory(r.Context(), id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if req.ExpectedVersion != nil && *req.ExpectedVersion != current.Version {
		WriteError(w, r, &domain.Error{Code: domain.CodeVersionConflict, Message: "expected_version does not match current version",
			Details: map[string]any{"expected_version": *req.ExpectedVersion, "current_version": current.Version}})
		return
	}
	if req.Content != nil {
		current.Content = *req.Content
	}
	if req.ContentFormat != nil {
		current.ContentFormat = domain.ContentFormat(*req.ContentFormat)
	}
	if req.Confidence != nil {
		current.Confidence = req.Confidence
	}
	if req.DecayScore != nil {
		current.DecayScore = req.DecayScore
	}
	if req.Tags != nil {
		current.Tags = req.Tags
	}
	if req.TTLExpiresAt != nil {
		current.TTLExpiresAt = req.TTLExpiresAt
	}
	updated, err := h.Memories.UpdateMemory(r.Context(), current)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DeleteMemory handles DELETE /api/v1/memories/{id} (expire by default).
func (h *Handlers) DeleteMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if r.URL.Query().Get("mode") == "purge" {
		WriteError(w, r, &domain.Error{Code: domain.CodePurgeForbidden, Message: "purge requires admin on the owning scope"})
		return
	}
	if err := h.Memories.DeleteMemory(r.Context(), id); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateLink handles POST /api/v1/memories/{id}/links.
func (h *Handlers) CreateLink(w http.ResponseWriter, r *http.Request) {
	sourceID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req CreateLinkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	rel, err := parseRelationship(req.RelationshipType)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if req.TargetID == sourceID {
		WriteError(w, r, &domain.Error{Code: domain.CodeLinkIntegrity, Message: "self-link not allowed"})
		return
	}
	weight := 1.0
	if req.Weight != nil {
		weight = *req.Weight
	}
	link, err := h.Memories.LinkMemories(r.Context(), &domain.MemoryLink{
		SourceID:         sourceID,
		TargetID:         req.TargetID,
		RelationshipType: rel,
		Weight:           weight,
		Evidence:         req.Evidence,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

// GetLinks handles GET /api/v1/memories/{id}/links.
func (h *Handlers) GetLinks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	// Existence check surfaces MEMORY_NOT_FOUND before link listing.
	if _, err := h.Memories.GetMemory(r.Context(), id); err != nil {
		WriteError(w, r, err)
		return
	}
	q := r.URL.Query()
	filter := port.LinkFilter{Direction: q.Get("direction")}
	if filter.Direction == "" {
		filter.Direction = "both"
	}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		filter.Limit = v
	}
	writeJSON(w, http.StatusOK, PageResponse{Items: []any{}, NextCursor: "", HasMore: false, TotalEstimate: nil})
}

func parseRelationship(raw string) (domain.RelationshipType, error) {
	rt := domain.RelationshipType(raw)
	if !rt.Valid() {
		return "", &domain.Error{Code: domain.CodeValidationErr, Message: "invalid relationship_type", Details: map[string]any{"field": "relationship_type"}}
	}
	return rt, nil
}

// ---------------------------------------------------------------------------
// Recall handlers
// ---------------------------------------------------------------------------

// Recall handles POST /api/v1/recall.
func (h *Handlers) RecallSubmit(w http.ResponseWriter, r *http.Request) {
	var req RecallRequestDTO
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	mode := domain.RecallModeSync
	if req.Mode == string(domain.RecallModeAsync) {
		mode = domain.RecallModeAsync
	}
	topK := 50
	if req.RetrievalParams.TopK != nil {
		topK = *req.RetrievalParams.TopK
	}
	rerank := domain.RerankCrossEncoder
	if req.RetrievalParams.Rerank != nil {
		rerank = domain.RerankType(*req.RetrievalParams.Rerank)
	}
	minScore := 0.35
	if req.RetrievalParams.MinScore != nil {
		minScore = *req.RetrievalParams.MinScore
	}
	strategies := make([]domain.StrategyType, len(req.RetrievalParams.Strategies))
	for i, s := range req.RetrievalParams.Strategies {
		st := domain.StrategyType(s)
		if !st.Valid() {
			WriteError(w, r, domain.NewValidationError("retrieval_params.strategies", s, "vector|bm25|graph|temporal"))
			return
		}
		strategies[i] = st
	}
	rc := domain.RecallContext{
		TaskSignature:  req.Context.TaskSignature,
		MentionedFiles: req.Context.MentionedFiles,
		Entities:       req.Context.Entities,
	}
	if req.Context.TimeBounds != nil {
		rc.TimeBounds = &[2]time.Time{}
		if req.Context.TimeBounds.From != nil {
			rc.TimeBounds[0] = *req.Context.TimeBounds.From
		}
		if req.Context.TimeBounds.To != nil {
			rc.TimeBounds[1] = *req.Context.TimeBounds.To
		}
	}
	rr := &domain.RecallRequest{
		Query:   req.Query,
		Context: rc,
		Trigger: domain.TriggerType(req.Trigger),
		AgentID: req.AgentID,
		SessionID: req.SessionID,
		RetrievalParams: domain.RetrievalParams{
			Strategies:  strategies,
			TopK:        topK,
			Rerank:      rerank,
			MinScore:    minScore,
			SlotBudget:  req.RetrievalParams.SlotBudget,
			TokenBudget: req.RetrievalParams.TokenBudget,
		},
		Mode: mode,
	}
	result, err := h.Recall.Recall(r.Context(), rr)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if mode == domain.RecallModeAsync {
		writeJSON(w, http.StatusAccepted, RecallAsyncResponse{Request: rr, Result: nil, Status: "queued", PollAfterMS: 250})
		return
	}
	writeJSON(w, http.StatusOK, RecallSyncResponse{Request: rr, Result: result})
}

// GetRecallStatus handles GET /api/v1/recall/{request_id}.
func (h *Handlers) GetRecallStatus(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "request_id")
	if !ok {
		return
	}
	req, result, err := h.Recall.GetRecallStatus(r.Context(), requestID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var errBody any
	if req.Error != nil {
		errBody = req.Error
	}
	var resultBody any
	if result != nil {
		resultBody = result
	}
	writeJSON(w, http.StatusOK, RecallStatusResponse{Request: req, Status: string(req.Status), Result: resultBody, Error: errBody})
}

// ---------------------------------------------------------------------------
// Session handlers
// ---------------------------------------------------------------------------

// CreateSession handles POST /api/v1/sessions.
func (h *Handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	s := &domain.AgentSession{
		AgentType:     domain.AgentType(req.AgentType),
		UserID:        req.UserID,
		SharedSpaceID: req.SharedSpaceID,
		ContextWindow: domain.ContextWindow{
			Model:                 req.ContextWindow.Model,
			MaxTokens:             req.ContextWindow.MaxTokens,
			InstructionSlotBudget: req.ContextWindow.InstructionSlotBudget,
		},
	}
	created, plan, err := h.Sessions.StartSession(r.Context(), s)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	planDTO := make([]InjectionPlanItemDTO, len(plan))
	for i, item := range plan {
		planDTO[i] = InjectionPlanItemDTO{MemoryID: item.MemoryID, Position: item.Position, SlotCost: item.SlotCost}
	}
	writeJSON(w, http.StatusCreated, CreateSessionResponse{Session: created, BootstrapPlan: planDTO})
}

// GetSession handles GET /api/v1/sessions/{session_id}.
func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathUUID(w, r, "session_id")
	if !ok {
		return
	}
	s, err := h.Sessions.GetSession(r.Context(), sessionID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	cw := s.ContextWindow
	writeJSON(w, http.StatusOK, GetSessionResponse{
		Session: s,
		Usage: SessionUsage{
			TokensRemaining: cw.MaxTokens - cw.UsedTokens,
			SlotsRemaining:  cw.InstructionSlotBudget - cw.SlotsUsed,
		},
	})
}

// ActivateMemory handles POST /api/v1/sessions/{session_id}/memories.
func (h *Handlers) ActivateMemory(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathUUID(w, r, "session_id")
	if !ok {
		return
	}
	var req ActivateMemoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	s, err := h.Sessions.ActivateMemory(r.Context(), sessionID, req.MemoryID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	slotCost := 1
	inj := InjectionPlanItemDTO{MemoryID: req.MemoryID, Position: len(s.ActiveMemories), SlotCost: slotCost}
	if req.Position != nil {
		inj.Position = *req.Position
	}
	writeJSON(w, http.StatusOK, ActivateMemoryResponse{Session: s, Injection: inj})
}

// DeactivateMemory handles DELETE /api/v1/sessions/{session_id}/memories/{memory_id}.
func (h *Handlers) DeactivateMemory(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathUUID(w, r, "session_id")
	if !ok {
		return
	}
	memoryID, ok := pathUUID(w, r, "memory_id")
	if !ok {
		return
	}
	if _, err := h.Sessions.DeactivateMemory(r.Context(), sessionID, memoryID); err != nil {
		WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EndSession handles POST /api/v1/sessions/{session_id}/end.
func (h *Handlers) EndSession(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathUUID(w, r, "session_id")
	if !ok {
		return
	}
	var req EndSessionRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	s, job, err := h.Sessions.EndSession(r.Context(), sessionID, req.Summary)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var jobRef *JobRef
	if job != nil {
		jobRef = &JobRef{JobID: job.JobID, Kind: string(job.Kind), Status: string(job.Status), PollURL: "/api/v1/lifecycle/jobs/" + job.JobID.String()}
	}
	writeJSON(w, http.StatusAccepted, EndSessionResponse{Session: s, ConsolidationJob: jobRef})
}

// ---------------------------------------------------------------------------
// Space handlers
// ---------------------------------------------------------------------------

// CreateSpace handles POST /api/v1/spaces.
func (h *Handlers) CreateSpace(w http.ResponseWriter, r *http.Request) {
	var req CreateSpaceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	s := &domain.SharedMemorySpace{
		Name:        req.Name,
		Description: req.Description,
		OwnerType:   domain.SpaceOwnerType(req.OwnerType),
		OwnerID:     req.OwnerID,
		Scope:       req.Scope,
	}
	if req.AccessPolicy != nil {
		s.AccessPolicy = domain.AccessPolicy{
			DefaultAccess: domain.DefaultAccess(req.AccessPolicy.DefaultAccess),
			WritePolicy:   domain.WritePolicy(req.AccessPolicy.Write),
			PromotePolicy: domain.PromotePolicy(req.AccessPolicy.Promote),
		}
	}
	if req.StorageBackend != nil {
		s.StorageBackend = domain.StorageBackend{Kind: domain.StorageBackendType(req.StorageBackend.Kind), ConfigRef: req.StorageBackend.ConfigRef}
	}
	if req.RetentionPolicy != nil {
		s.RetentionPolicy = &domain.RetentionPolicy{
			SupersedeNotDelete: req.RetentionPolicy.SupersedeNotDelete,
			TTLDays:            req.RetentionPolicy.TTLDays,
			ArchiveAfterDays:   req.RetentionPolicy.ArchiveAfterDays,
		}
	}
	created, err := h.Spaces.CreateSpace(r.Context(), s)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// ListSpaces handles GET /api/v1/spaces.
func (h *Handlers) ListSpaces(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	pt := principal.Type
	if pt == "" {
		pt = domain.PrincipalUser
	}
	spaces, err := h.Spaces.ListSpaces(r.Context(), pt, principal.ID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	items := make([]any, len(spaces))
	for i, s := range spaces {
		items[i] = s
	}
	writeJSON(w, http.StatusOK, PageResponse{Items: items, NextCursor: "", HasMore: false, TotalEstimate: nil})
}

// GetSpace handles GET /api/v1/spaces/{space_id}.
func (h *Handlers) GetSpace(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := pathUUID(w, r, "space_id")
	if !ok {
		return
	}
	s, err := h.Spaces.GetSpace(r.Context(), spaceID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// UpdateSpace handles PUT /api/v1/spaces/{space_id}.
func (h *Handlers) UpdateSpace(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := pathUUID(w, r, "space_id")
	if !ok {
		return
	}
	var req UpdateSpaceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	current, err := h.Spaces.GetSpace(r.Context(), spaceID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if req.Description != nil {
		current.Description = req.Description
	}
	if req.AccessPolicy != nil {
		current.AccessPolicy = domain.AccessPolicy{
			DefaultAccess: domain.DefaultAccess(req.AccessPolicy.DefaultAccess),
			WritePolicy:   domain.WritePolicy(req.AccessPolicy.Write),
			PromotePolicy: domain.PromotePolicy(req.AccessPolicy.Promote),
		}
	}
	if req.RetentionPolicy != nil {
		current.RetentionPolicy = &domain.RetentionPolicy{
			SupersedeNotDelete: req.RetentionPolicy.SupersedeNotDelete,
			TTLDays:            req.RetentionPolicy.TTLDays,
			ArchiveAfterDays:   req.RetentionPolicy.ArchiveAfterDays,
		}
	}
	updated, err := h.Spaces.UpdateSpace(r.Context(), current)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// PromoteMemory handles POST /api/v1/spaces/{space_id}/memories.
func (h *Handlers) PromoteMemory(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := pathUUID(w, r, "space_id")
	if !ok {
		return
	}
	var req PromoteMemoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	p := &domain.PromotionProposal{
		CandidateMemoryID: req.MemoryID,
		SharedSpaceID:     spaceID,
		TargetPath:        req.TargetPath,
		TargetKind:        domain.ProposalTargetKind(req.TargetKind),
		TargetRole:        domain.ProposalTargetRole(req.TargetRole),
		Note:              req.Note,
	}
	if p.Diff == "" {
		// The transport owns the review payload: synthesize the unified
		// diff from candidate content so service-side Diff validation
		// (min=1) holds without trusting client input.
		cand, gerr := h.Memories.GetMemory(r.Context(), req.MemoryID)
		if gerr != nil {
			WriteError(w, r, gerr)
			return
		}
		if cand == nil {
			WriteError(w, r, &domain.Error{Code: domain.CodeMemoryNotFound, Message: "candidate memory not found"})
			return
		}
		snippet := []rune(cand.Content)
		if len(snippet) > 60 {
			snippet = snippet[:60]
		}
		p.Diff = fmt.Sprintf("--- individual\n+++ %s\n@@ -0,0 +1 @@\n+%s\n",
			p.TargetPath, string(snippet))
	}
	created, err := h.Spaces.PromoteMemory(r.Context(), p)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// ListProposals handles GET /api/v1/spaces/{space_id}/proposals.
func (h *Handlers) ListProposals(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := pathUUID(w, r, "space_id")
	if !ok {
		return
	}
	var status *domain.ProposalStatus
	if s := r.URL.Query().Get("status"); s != "" {
		ps := domain.ProposalStatus(s)
		if !ps.Valid() {
			WriteError(w, r, domain.NewValidationError("status", s, "draft|in_review|merged|rejected"))
			return
		}
		status = &ps
	}
	proposals, err := h.Spaces.ReviewProposals(r.Context(), spaceID, status)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	items := make([]any, len(proposals))
	for i, p := range proposals {
		items[i] = p
	}
	writeJSON(w, http.StatusOK, PageResponse{Items: items, NextCursor: "", HasMore: false, TotalEstimate: nil})
}

// ApproveProposal handles POST /api/v1/spaces/{space_id}/proposals/{proposal_id}/approve.
func (h *Handlers) ApproveProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, true)
}

// RejectProposal handles POST /api/v1/spaces/{space_id}/proposals/{proposal_id}/reject.
func (h *Handlers) RejectProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, false)
}

func (h *Handlers) reviewProposal(w http.ResponseWriter, r *http.Request, approve bool) {
	spaceID, ok := pathUUID(w, r, "space_id")
	if !ok {
		return
	}
	proposalID, ok := pathUUID(w, r, "proposal_id")
	if !ok {
		return
	}
	var req ReviewProposalRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	reviewer := req.Reviewer
	if reviewer == "" {
		if p, ok := PrincipalFromContext(r.Context()); ok {
			reviewer = p.ID
		}
	}
	var proposal *domain.PromotionProposal
	var err error
	if approve {
		proposal, err = h.Spaces.ApproveProposal(r.Context(), spaceID, proposalID, reviewer)
	} else {
		reason := ""
		if req.Reason != nil {
			reason = *req.Reason
		}
		proposal, err = h.Spaces.RejectProposal(r.Context(), spaceID, proposalID, reviewer, reason)
	}
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, proposal)
}

// SyncSpace handles POST /api/v1/spaces/{space_id}/sync.
func (h *Handlers) SyncSpace(w http.ResponseWriter, r *http.Request) {
	spaceID, ok := pathUUID(w, r, "space_id")
	if !ok {
		return
	}
	var req SyncSpaceRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	job, err := h.Spaces.SyncSpace(r.Context(), spaceID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, SyncSpaceResponse{
		Job:     JobRef{JobID: job.JobID, Kind: string(job.Kind), Status: string(job.Status), PollURL: "/api/v1/lifecycle/jobs/" + job.JobID.String()},
		SpaceID: spaceID,
	})
}

// ---------------------------------------------------------------------------
// Lifecycle handlers
// ---------------------------------------------------------------------------

// Consolidate handles POST /api/v1/lifecycle/consolidate.
func (h *Handlers) Consolidate(w http.ResponseWriter, r *http.Request) {
	h.lifecycleJob(w, r, h.Lifecycle.Consolidate)
}

// Decay handles POST /api/v1/lifecycle/decay.
func (h *Handlers) Decay(w http.ResponseWriter, r *http.Request) {
	h.lifecycleJob(w, r, h.Lifecycle.Decay)
}

func (h *Handlers) lifecycleJob(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, scopeKind, scopeID string) (*domain.LifecycleJob, error)) {
	var req ConsolidateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validateDTO(w, r, &req) {
		return
	}
	scopeID := ""
	if req.Scope.ID != nil {
		scopeID = *req.Scope.ID
	}
	job, err := fn(r.Context(), req.Scope.Kind, scopeID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, JobResponse{Job: JobRef{
		JobID: job.JobID, Kind: string(job.Kind), Status: string(job.Status),
		PollURL: "/api/v1/lifecycle/jobs/" + job.JobID.String(),
	}})
}

// GetJob handles GET /api/v1/lifecycle/jobs/{job_id}.
func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID, ok := pathUUID(w, r, "job_id")
	if !ok {
		return
	}
	job, err := h.Lifecycle.GetJob(r.Context(), jobID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// GetStats handles GET /api/v1/lifecycle/stats.
func (h *Handlers) GetStats(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "global"
	}
	stats, err := h.Lifecycle.GetStats(r.Context(), scope)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// csvNonEmpty splits a csv query param, dropping empties.
func csvNonEmpty(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
