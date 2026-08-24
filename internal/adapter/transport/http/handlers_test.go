package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// ---------------------------------------------------------------------------
// Service mocks
// ---------------------------------------------------------------------------

type fakeMemoryService struct {
	port.MemoryService

	saveCalled int
	savedIn    *domain.Memory
	retSaved   *domain.Memory
	retSaveErr error

	getID  uuid.UUID
	retGet *domain.Memory
	retGetErr error

	retList port.Page[*domain.Memory]

	updateIn  *domain.Memory
	retUpdate *domain.Memory

	deletedID uuid.UUID
	retDelErr error

	linkIn  *domain.MemoryLink
	retLink *domain.MemoryLink
}

func (f *fakeMemoryService) SaveMemory(_ context.Context, m *domain.Memory) (*domain.Memory, error) {
	f.saveCalled++
	f.savedIn = m
	return f.retSaved, f.retSaveErr
}
func (f *fakeMemoryService) GetMemory(_ context.Context, id uuid.UUID) (*domain.Memory, error) {
	f.getID = id
	return f.retGet, f.retGetErr
}
func (f *fakeMemoryService) ListMemories(context.Context, port.MemoryFilter) (port.Page[*domain.Memory], error) {
	return f.retList, nil
}
func (f *fakeMemoryService) UpdateMemory(_ context.Context, m *domain.Memory) (*domain.Memory, error) {
	f.updateIn = m
	return f.retUpdate, nil
}
func (f *fakeMemoryService) DeleteMemory(_ context.Context, id uuid.UUID) error {
	f.deletedID = id
	return f.retDelErr
}
func (f *fakeMemoryService) LinkMemories(_ context.Context, l *domain.MemoryLink) (*domain.MemoryLink, error) {
	f.linkIn = l
	return f.retLink, nil
}

type fakeRecallService struct {
	port.RecallService

	recallIn   *domain.RecallRequest
	retRecall  *domain.RecallResult
	retRecErr  error

	statusID   uuid.UUID
	retReq     *domain.RecallRequest
	retResult  *domain.RecallResult
	retStatErr error
}

func (f *fakeRecallService) Recall(_ context.Context, req *domain.RecallRequest) (*domain.RecallResult, error) {
	f.recallIn = req
	return f.retRecall, f.retRecErr
}
func (f *fakeRecallService) GetRecallStatus(_ context.Context, id uuid.UUID) (*domain.RecallRequest, *domain.RecallResult, error) {
	f.statusID = id
	return f.retReq, f.retResult, f.retStatErr
}

type fakeSessionService struct {
	port.SessionService

	startIn    *domain.AgentSession
	retSession *domain.AgentSession
	retPlan    []domain.InjectionPlanItem
	retStartErr error

	getID     uuid.UUID
	retGetSess *domain.AgentSession
	retGetErr error

	activateSessionID uuid.UUID
	activateMemoryID  uuid.UUID
	retActivate       *domain.AgentSession

	deactivateSessionID uuid.UUID
	deactivateMemoryID  uuid.UUID

	endID   uuid.UUID
	retEndS *domain.AgentSession
	retEndJ *domain.LifecycleJob
}

func (f *fakeSessionService) StartSession(_ context.Context, s *domain.AgentSession) (*domain.AgentSession, []domain.InjectionPlanItem, error) {
	f.startIn = s
	return f.retSession, f.retPlan, f.retStartErr
}
func (f *fakeSessionService) GetSession(_ context.Context, id uuid.UUID) (*domain.AgentSession, error) {
	f.getID = id
	return f.retGetSess, f.retGetErr
}
func (f *fakeSessionService) ActivateMemory(_ context.Context, sid, mid uuid.UUID) (*domain.AgentSession, error) {
	f.activateSessionID = sid
	f.activateMemoryID = mid
	return f.retActivate, nil
}
func (f *fakeSessionService) DeactivateMemory(_ context.Context, sid, mid uuid.UUID) (*domain.AgentSession, error) {
	f.deactivateSessionID = sid
	f.deactivateMemoryID = mid
	return f.retActivate, nil
}
func (f *fakeSessionService) EndSession(_ context.Context, id uuid.UUID, summary *string) (*domain.AgentSession, *domain.LifecycleJob, error) {
	f.endID = id
	return f.retEndS, f.retEndJ, nil
}

type fakeSpaceService struct {
	port.SpaceService

	createIn    *domain.SharedMemorySpace
	retCreated  *domain.SharedMemorySpace
	retCreateErr error

	listType  domain.PrincipalType
	listID    string
	retSpaces []*domain.SharedMemorySpace

	getSpaceID uuid.UUID
	retSpace   *domain.SharedMemorySpace
	retGetErr  error

	promoteIn    *domain.PromotionProposal
	retPromotion *domain.PromotionProposal

	reviewSpaceID uuid.UUID
	retProposals  []*domain.PromotionProposal

	approvePID uuid.UUID
	retApprove *domain.PromotionProposal

	rejectPID uuid.UUID
	retReject *domain.PromotionProposal

	syncID uuid.UUID
	retJob *domain.LifecycleJob
}

func (f *fakeSpaceService) CreateSpace(_ context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	f.createIn = s
	return f.retCreated, f.retCreateErr
}
func (f *fakeSpaceService) ListSpaces(_ context.Context, pt domain.PrincipalType, pid string) ([]*domain.SharedMemorySpace, error) {
	f.listType = pt
	f.listID = pid
	return f.retSpaces, nil
}
func (f *fakeSpaceService) GetSpace(_ context.Context, id uuid.UUID) (*domain.SharedMemorySpace, error) {
	f.getSpaceID = id
	return f.retSpace, f.retGetErr
}
func (f *fakeSpaceService) UpdateSpace(_ context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	return s, nil
}
func (f *fakeSpaceService) PromoteMemory(_ context.Context, p *domain.PromotionProposal) (*domain.PromotionProposal, error) {
	f.promoteIn = p
	return f.retPromotion, nil
}
func (f *fakeSpaceService) ReviewProposals(_ context.Context, spaceID uuid.UUID, _ *domain.ProposalStatus) ([]*domain.PromotionProposal, error) {
	f.reviewSpaceID = spaceID
	return f.retProposals, nil
}
func (f *fakeSpaceService) ApproveProposal(_ context.Context, _, pid uuid.UUID, _ string) (*domain.PromotionProposal, error) {
	f.approvePID = pid
	return f.retApprove, nil
}
func (f *fakeSpaceService) RejectProposal(_ context.Context, _, pid uuid.UUID, _, _ string) (*domain.PromotionProposal, error) {
	f.rejectPID = pid
	return f.retReject, nil
}
func (f *fakeSpaceService) SyncSpace(_ context.Context, id uuid.UUID) (*domain.LifecycleJob, error) {
	f.syncID = id
	return f.retJob, nil
}

type fakeLifecycleService struct {
	port.LifecycleService

	consolidateKind string
	retConsolidate  *domain.LifecycleJob

	decayKind string
	retDecay  *domain.LifecycleJob

	jobID  uuid.UUID
	retJob *domain.LifecycleJob

	statsScope string
	retStats   port.MemoryStats
	retStatErr error
}

func (f *fakeLifecycleService) Consolidate(_ context.Context, kind, _ string) (*domain.LifecycleJob, error) {
	f.consolidateKind = kind
	return f.retConsolidate, nil
}
func (f *fakeLifecycleService) Decay(_ context.Context, kind, _ string) (*domain.LifecycleJob, error) {
	f.decayKind = kind
	return f.retDecay, nil
}
func (f *fakeLifecycleService) GetJob(_ context.Context, id uuid.UUID) (*domain.LifecycleJob, error) {
	f.jobID = id
	return f.retJob, nil
}
func (f *fakeLifecycleService) GetStats(_ context.Context, scope string) (port.MemoryStats, error) {
	f.statsScope = scope
	return f.retStats, f.retStatErr
}

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

type harness struct {
	router   http.Handler
	memories *fakeMemoryService
	recall   *fakeRecallService
	sessions *fakeSessionService
	spaces   *fakeSpaceService
	life     *fakeLifecycleService
}

func newHarness() *harness {
	h := &harness{
		memories: &fakeMemoryService{},
		recall:   &fakeRecallService{},
		sessions: &fakeSessionService{},
		spaces:   &fakeSpaceService{},
		life:     &fakeLifecycleService{},
	}
	h.router = NewRouter(NewHandlers(h.memories, h.recall, h.sessions, h.spaces, h.life))
	return h
}

// do executes a request against the authenticated API surface. Every /api/v1
// route sits behind AuthMiddleware, so a bearer token is always attached.
func (t *harness) do(method, path string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer agent:test-agent")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	t.router.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), dst))
}

func TestRouterChiInternals(t *testing.T) {
	// Guard: the router must be a chi router so URL params resolve in tests.
	_, ok := NewRouter(NewHandlers(nil, nil, nil, nil, nil)).(*chi.Mux)
	assert.True(t, ok)
}

// ---------------------------------------------------------------------------
// POST /api/v1/memories
// ---------------------------------------------------------------------------

func TestCreateMemorySuccess(t *testing.T) {
	h := newHarness()
	sid := uuid.New()
	now := time.Now().UTC()
	h.memories.retSaved = &domain.Memory{
		ID: uuid.New(), Type: domain.MemoryTypeSemantic, Content: "prefers dark mode",
		ContentFormat: domain.ContentFormatMarkdown, Version: 1,
		Origin: domain.OriginAgentObservation, AccessScope: domain.AccessScopeIndividual,
		CreatedAt: now, UpdatedAt: now,
	}

	body := map[string]any{
		"type":      "semantic",
		"content":   "prefers dark mode",
		"tags":      []string{"pref"},
		"provenance": map[string]string{"origin": "agent_observation"},
	}
	rec := h.do(http.MethodPost, "/api/v1/memories", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var saved domain.Memory
	decodeBody(t, rec, &saved)
	assert.Equal(t, "prefers dark mode", saved.Content)
	assert.Equal(t, "/api/v1/memories/"+saved.ID.String(), rec.Header().Get("Location"))
	require.NotNil(t, h.memories.savedIn)
	assert.Equal(t, domain.MemoryTypeSemantic, h.memories.savedIn.Type)
	// F21: agent_id is stamped from the bearer-token principal, not the body.
	require.NotNil(t, h.memories.savedIn.AgentID)
	assert.Equal(t, "test-agent", *h.memories.savedIn.AgentID)
	assert.Equal(t, domain.PrincipalAgent, h.memories.savedIn.OwnerPrincipalType)
	_ = sid
}

func TestCreateMemoryValidationError(t *testing.T) {
	h := newHarness()

	// Missing type and content, plus a bad provenance.origin.
	rec := h.do(http.MethodPost, "/api/v1/memories", map[string]any{
		"provenance": map[string]string{"origin": "agent_observation"},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var env ErrorEnvelope
	decodeBody(t, rec, &env)
	assert.Equal(t, string(domain.CodeValidationErr), env.Error.Code)
	assert.NotEmpty(t, env.Error.RequestID)
	assert.Contains(t, env.Error.DocURL, "VALIDATION_ERROR")
	// The validation-failure details list the offending fields.
	fields, ok := env.Error.Details["fields"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, fields, "Type")
	assert.Contains(t, fields, "Content")
}

func TestCreateMemoryUnknownFieldRejected(t *testing.T) {
	h := newHarness()
	rec := h.do(http.MethodPost, "/api/v1/memories", map[string]any{
		"type": "semantic", "content": "x",
		"provenance":     map[string]string{"origin": "agent_observation"},
		"unknown_field2": 1,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var env ErrorEnvelope
	decodeBody(t, rec, &env)
	assert.Equal(t, string(domain.CodeValidationErr), env.Error.Code)
}

// ---------------------------------------------------------------------------
// GET /api/v1/memories/{id}
// ---------------------------------------------------------------------------

func TestGetMemorySuccess(t *testing.T) {
	h := newHarness()
	id := uuid.New()
	h.memories.retGet = &domain.Memory{
		ID: id, Type: domain.MemoryTypeEpisodic, Content: "fixed login bug",
		Version: 1, Origin: domain.OriginUserInstruction,
		AccessScope: domain.AccessScopeIndividual, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	rec := h.do(http.MethodGet, "/api/v1/memories/"+id.String(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, id, h.memories.getID)

	var m domain.Memory
	decodeBody(t, rec, &m)
	assert.Equal(t, id, m.ID)
	assert.Equal(t, "fixed login bug", m.Content)
}

func TestGetMemoryNotFound(t *testing.T) {
	h := newHarness()
	h.memories.retGetErr = &domain.Error{Code: domain.CodeMemoryNotFound, Message: "no such memory"}

	rec := h.do(http.MethodGet, "/api/v1/memories/"+uuid.NewString(), nil)
	require.Equal(t, http.StatusNotFound, rec.Code)

	var env ErrorEnvelope
	decodeBody(t, rec, &env)
	assert.Equal(t, string(domain.CodeMemoryNotFound), env.Error.Code)
}

func TestGetMemoryInvalidUUID(t *testing.T) {
	h := newHarness()
	rec := h.do(http.MethodGet, "/api/v1/memories/not-a-uuid", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var env ErrorEnvelope
	decodeBody(t, rec, &env)
	assert.Equal(t, string(domain.CodeValidationErr), env.Error.Code)
}

// ---------------------------------------------------------------------------
// POST /api/v1/recall
// ---------------------------------------------------------------------------

func TestRecallSyncSuccess(t *testing.T) {
	h := newHarness()
	sessionID := uuid.New()
	reqID := uuid.New()
	h.recall.retRecall = &domain.RecallResult{
		ResultID: uuid.New(), RequestID: reqID,
		Candidates:    []domain.RecallCandidate{},
		InjectionPlan: []domain.InjectionPlanItem{},
	}

	body := map[string]any{
		"query":   "how is decay handled?",
		"trigger": "user_query",
		"agent_id": "test-agent",
		"session_id": sessionID.String(),
		"retrieval_params": map[string]any{
			"strategies": []string{"vector", "bm25"},
		},
	}
	rec := h.do(http.MethodPost, "/api/v1/recall", body)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp RecallSyncResponse
	decodeBody(t, rec, &resp)
	require.NotNil(t, resp.Request)
	require.NotNil(t, resp.Result)

	in := h.recall.recallIn
	require.NotNil(t, in)
	assert.Equal(t, "how is decay handled?", in.Query)
	assert.Equal(t, domain.TriggerUserQuery, in.Trigger)
	assert.Equal(t, domain.RecallModeSync, in.Mode)
	assert.Equal(t, []domain.StrategyType{domain.StrategyVector, domain.StrategyBM25}, in.RetrievalParams.Strategies)
	assert.Equal(t, 50, in.RetrievalParams.TopK) // default
}

// ---------------------------------------------------------------------------
// POST /api/v1/sessions
// ---------------------------------------------------------------------------

func TestCreateSessionSuccess(t *testing.T) {
	h := newHarness()
	sessID := uuid.New()
	h.sessions.retSession = &domain.AgentSession{
		SessionID: sessID, AgentType: domain.AgentClaudeCode, UserID: "u1",
		ContextWindow: domain.ContextWindow{Model: "sonnet", MaxTokens: 100000, InstructionSlotBudget: 10},
		CreatedAt:     time.Now(),
	}
	h.sessions.retPlan = []domain.InjectionPlanItem{{MemoryID: uuid.New(), Position: 1, SlotCost: 2}}

	body := map[string]any{
		"agent_type": "claude-code",
		"user_id":    "u1",
		"context_window": map[string]any{
			"model": "sonnet", "max_tokens": 100000, "instruction_slot_budget": 10,
		},
	}
	rec := h.do(http.MethodPost, "/api/v1/sessions", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateSessionResponse
	decodeBody(t, rec, &resp)
	require.NotNil(t, resp.Session)
	require.Len(t, resp.BootstrapPlan, 1)
	assert.Equal(t, 1, resp.BootstrapPlan[0].Position)
	require.NotNil(t, h.sessions.startIn)
	assert.Equal(t, domain.AgentClaudeCode, h.sessions.startIn.AgentType)
}

// ---------------------------------------------------------------------------
// POST /api/v1/spaces
// ---------------------------------------------------------------------------

func TestCreateSpaceSuccess(t *testing.T) {
	h := newHarness()
	h.spaces.retCreated = &domain.SharedMemorySpace{
		ID: uuid.New(), Name: "team-memory", OwnerType: domain.SpaceOwnerTeam,
		OwnerID: "team-1", Scope: "org", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	body := map[string]any{
		"name":       "team-memory",
		"owner_type": "team",
		"owner_id":   "team-1",
		"scope":      "org",
		"access_policy": map[string]any{
			"default_access": "read", "write": "owner_approved", "promote": "human_review",
		},
	}
	rec := h.do(http.MethodPost, "/api/v1/spaces", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var created domain.SharedMemorySpace
	decodeBody(t, rec, &created)
	assert.Equal(t, "team-memory", created.Name)
	require.NotNil(t, h.spaces.createIn)
	assert.Equal(t, domain.WriteOwnerApproved, h.spaces.createIn.AccessPolicy.WritePolicy)
}

func TestCreateSpaceValidationError(t *testing.T) {
	h := newHarness()
	rec := h.do(http.MethodPost, "/api/v1/spaces", map[string]any{
		"owner_type": "team", "owner_id": "t", "scope": "s",
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// POST /api/v1/spaces/{id}/memories (promote)
// ---------------------------------------------------------------------------

func TestPromoteMemorySuccess(t *testing.T) {
	h := newHarness()
	spaceID := uuid.New()
	memID := uuid.New()
	h.memories.retGet = &domain.Memory{
		ID: memID, Type: domain.MemoryTypeSemantic, Content: "candidate content",
	}
	h.spaces.retPromotion = &domain.PromotionProposal{
		ID: uuid.New(), CandidateMemoryID: memID, SharedSpaceID: spaceID,
		TargetPath: "specs/x.md", TargetKind: domain.ProposalTargetSpec,
		TargetRole: domain.ProposalRoleSemantic, Diff: "+ line",
		Status:     domain.ProposalStatusInReview, ProposedAt: time.Now(),
	}

	body := map[string]any{
		"memory_id":   memID.String(),
		"target_path": "specs/x.md",
		"target_kind": "spec",
		"target_role": "semantic",
	}
	rec := h.do(http.MethodPost, "/api/v1/spaces/"+spaceID.String()+"/memories", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var p domain.PromotionProposal
	decodeBody(t, rec, &p)
	assert.Equal(t, spaceID, p.SharedSpaceID)
	require.NotNil(t, h.spaces.promoteIn)
	assert.Equal(t, memID, h.spaces.promoteIn.CandidateMemoryID)
	assert.Equal(t, spaceID, h.spaces.promoteIn.SharedSpaceID)
}

// ---------------------------------------------------------------------------
// GET /api/v1/lifecycle/stats
// ---------------------------------------------------------------------------

func TestGetStatsSuccess(t *testing.T) {
	h := newHarness()
	h.life.retStats = port.MemoryStats{
		Scope: "global", GeneratedAt: "2026-01-01T00:00:00Z",
		Counts: port.StatCounts{Total: 5, ByType: map[string]int{"semantic": 5}, ByAccessScope: map[string]int{"individual": 5}},
		Quality: port.StatQuality{AvgConfidence: 0.8},
		Links:   port.StatLinks{Total: 2, ByRelationshipType: map[string]int{"similar_to": 2}},
		Spaces:  port.StatSpaces{Total: 1, PendingProposals: 1},
	}

	rec := h.do(http.MethodGet, "/api/v1/lifecycle/stats", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var stats port.MemoryStats
	decodeBody(t, rec, &stats)
	assert.Equal(t, 5, stats.Counts.Total)
	assert.Equal(t, "global", h.life.statsScope) // default scope
}

// ---------------------------------------------------------------------------
// Auth and error envelope
// ---------------------------------------------------------------------------

func TestAuthMiddlewareRejectsMissingCredentials(t *testing.T) {
	h := newHarness()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/memories", nil)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var env ErrorEnvelope
	decodeBody(t, rec, &env)
	assert.Equal(t, string(domain.CodeUnauthenticated), env.Error.Code)
}

func TestHealthEndpoint(t *testing.T) {
	h := newHarness()
	rec := h.do(http.MethodGet, "/health", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------------
// Session lifecycle handlers
// ---------------------------------------------------------------------------

func TestEndSessionReturnsJobRef(t *testing.T) {
	h := newHarness()
	sessID := uuid.New()
	h.sessions.retEndS = &domain.AgentSession{SessionID: sessID, UserID: "u1", CreatedAt: time.Now()}
	h.sessions.retEndJ = &domain.LifecycleJob{
		JobID: uuid.New(), Kind: domain.JobConsolidation, Status: domain.JobStatusQueued, CreatedAt: time.Now(),
	}

	rec := h.do(http.MethodPost, "/api/v1/sessions/"+sessID.String()+"/end", map[string]any{"summary": "done"})
	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp EndSessionResponse
	decodeBody(t, rec, &resp)
	require.NotNil(t, resp.ConsolidationJob)
	assert.Equal(t, "consolidation", resp.ConsolidationJob.Kind)
	assert.Contains(t, resp.ConsolidationJob.PollURL, "/api/v1/lifecycle/jobs/")
}

func TestConsolidateJobRef(t *testing.T) {
	h := newHarness()
	h.life.retConsolidate = &domain.LifecycleJob{
		JobID: uuid.New(), Kind: domain.JobConsolidation, Status: domain.JobStatusQueued, CreatedAt: time.Now(),
	}
	rec := h.do(http.MethodPost, "/api/v1/lifecycle/consolidate", map[string]any{
		"scope": map[string]any{"kind": "user", "id": "u1"},
	})
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "user", h.life.consolidateKind)

	var resp JobResponse
	decodeBody(t, rec, &resp)
	assert.Equal(t, "consolidation", resp.Job.Kind)
}

func TestListMemoriesFilters(t *testing.T) {
	h := newHarness()
	h.memories.retList = port.Page[*domain.Memory]{Items: []*domain.Memory{}, HasMore: false}

	rec := h.do(http.MethodGet, "/api/v1/memories?type=semantic&tags=a,b&limit=5", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var page PageResponse
	decodeBody(t, rec, &page)
	assert.False(t, page.HasMore)
}

func TestDeleteMemory(t *testing.T) {
	h := newHarness()
	id := uuid.New()
	rec := h.do(http.MethodDelete, "/api/v1/memories/"+id.String(), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, id, h.memories.deletedID)
}

func TestUpdateMemory(t *testing.T) {
	h := newHarness()
	id := uuid.New()
	h.memories.retGet = &domain.Memory{
		ID: id, Type: domain.MemoryTypeSemantic, Content: "old", Version: 2,
		Origin: domain.OriginAgentObservation, AccessScope: domain.AccessScopeIndividual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	h.memories.retUpdate = h.memories.retGet

	newContent := "new content"
	rec := h.do(http.MethodPut, "/api/v1/memories/"+id.String(), map[string]any{
		"expected_version": 2, "content": newContent,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, h.memories.updateIn)
	assert.Equal(t, newContent, h.memories.updateIn.Content)
}
