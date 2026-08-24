package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockMemoryRepo struct {
	saved   []*domain.Memory
	byID    map[uuid.UUID]*domain.Memory
	updates []struct {
		m      *domain.Memory
		expect int
	}
	deleted   []uuid.UUID
	links     []*domain.MemoryLink
	getErr    error
	saveErr   error
	updateErr error
}

func newMockMemoryRepo() *mockMemoryRepo {
	return &mockMemoryRepo{byID: map[uuid.UUID]*domain.Memory{}}
}

func (r *mockMemoryRepo) Save(ctx context.Context, m *domain.Memory) (*domain.Memory, error) {
	if r.saveErr != nil {
		return nil, r.saveErr
	}
	stored := *m
	r.saved = append(r.saved, &stored)
	r.byID[m.ID] = &stored
	return &stored, nil
}

func (r *mockMemoryRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Memory, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	m, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return m, nil
}

func (r *mockMemoryRepo) GetByVersion(ctx context.Context, id uuid.UUID, version int) (*domain.Memory, error) {
	m, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.Version != version {
		return nil, domain.ErrNotFound
	}
	return m, nil
}

func (r *mockMemoryRepo) List(ctx context.Context, filter port.MemoryFilter) (port.Page[*domain.Memory], error) {
	return port.Page[*domain.Memory]{Items: r.saved}, nil
}

func (r *mockMemoryRepo) Update(ctx context.Context, m *domain.Memory, expectedVersion int) (*domain.Memory, error) {
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	current, ok := r.byID[m.ID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if current.Version != expectedVersion {
		return nil, domain.ErrVersionConflict
	}
	stored := *m
	r.byID[m.ID] = &stored
	r.updates = append(r.updates, struct {
		m      *domain.Memory
		expect int
	}{&stored, expectedVersion})
	return &stored, nil
}

func (r *mockMemoryRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func (r *mockMemoryRepo) Purge(ctx context.Context, id uuid.UUID) error { return nil }

func (r *mockMemoryRepo) SaveLink(ctx context.Context, l *domain.MemoryLink) (*domain.MemoryLink, error) {
	stored := *l
	r.links = append(r.links, &stored)
	return &stored, nil
}

func (r *mockMemoryRepo) ListLinks(ctx context.Context, memoryID uuid.UUID, opts port.LinkFilter) ([]*domain.MemoryLink, error) {
	return r.links, nil
}

type mockAccessLog struct {
	entries []*domain.MemoryAccessLog
}

func (l *mockAccessLog) Append(ctx context.Context, e *domain.MemoryAccessLog) error {
	l.entries = append(l.entries, e)
	return nil
}
func (l *mockAccessLog) ListByMemory(ctx context.Context, memoryID uuid.UUID, limit int) ([]*domain.MemoryAccessLog, error) {
	return l.entries, nil
}

type mockEncoder struct {
	vector   pgvector.Vector
	infer    domain.MemoryType
	entities []domain.Entity
	err      error
	calls    []string
}

func (e *mockEncoder) Encode(ctx context.Context, content string, opts port.EncodeOptions) (port.EncodedMemory, error) {
	e.calls = append(e.calls, content)
	if e.err != nil {
		return port.EncodedMemory{}, e.err
	}
	return port.EncodedMemory{Vector: e.vector, InferredType: e.infer, Entities: e.entities}, nil
}

func (e *mockEncoder) EncodeBatch(ctx context.Context, contents []string, opts port.EncodeOptions) ([]port.EncodedMemory, error) {
	out := make([]port.EncodedMemory, len(contents))
	for i := range contents {
		em, err := e.Encode(ctx, contents[i], opts)
		if err != nil {
			return nil, err
		}
		out[i] = em
	}
	return out, nil
}

func (e *mockEncoder) ListModels(ctx context.Context) ([]*domain.EmbeddingModel, error) {
	return nil, nil
}

type mockSessionRepo struct {
	created     []*domain.AgentSession
	byID        map[uuid.UUID]*domain.AgentSession
	activated   []uuid.UUID
	deactivated []uuid.UUID
	getErr      error
	endSummary  *string
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{byID: map[uuid.UUID]*domain.AgentSession{}}
}

func (r *mockSessionRepo) Create(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, error) {
	stored := *s
	r.created = append(r.created, &stored)
	r.byID[s.SessionID] = &stored
	return &stored, nil
}

func (r *mockSessionRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.AgentSession, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	s, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s, nil
}

func (r *mockSessionRepo) Update(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, error) {
	stored := *s
	r.byID[s.SessionID] = &stored
	return &stored, nil
}

func (r *mockSessionRepo) EndSession(ctx context.Context, id uuid.UUID, summary *string) (*domain.AgentSession, error) {
	s, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if s.EndedAt != nil {
		return nil, domain.ErrInvalidState
	}
	stored := *s
	end := s.CreatedAt.Add(time.Minute)
	stored.EndedAt = &end
	stored.Summary = summary
	r.byID[id] = &stored
	r.endSummary = summary
	return &stored, nil
}

func (r *mockSessionRepo) ActivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error) {
	s, ok := r.byID[sessionID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cw := s.ContextWindow
	if cw.InstructionSlotBudget != 0 && cw.SlotsUsed+1 > cw.InstructionSlotBudget {
		return nil, domain.ErrSlotBudgetExceeded
	}
	stored := *s
	stored.ActiveMemories = append(stored.ActiveMemories, memoryID)
	stored.ContextWindow.SlotsUsed++
	r.byID[sessionID] = &stored
	r.activated = append(r.activated, memoryID)
	return &stored, nil
}

func (r *mockSessionRepo) DeactivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error) {
	s, ok := r.byID[sessionID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	stored := *s
	kept := stored.ActiveMemories[:0]
	for _, id := range stored.ActiveMemories {
		if id != memoryID {
			kept = append(kept, id)
		}
	}
	stored.ActiveMemories = kept
	r.byID[sessionID] = &stored
	r.deactivated = append(r.deactivated, memoryID)
	return &stored, nil
}

type mockSpaceRepo struct {
	created map[uuid.UUID]*domain.SharedMemorySpace
	getErr  error
}

func newMockSpaceRepo() *mockSpaceRepo {
	return &mockSpaceRepo{created: map[uuid.UUID]*domain.SharedMemorySpace{}}
}

func (r *mockSpaceRepo) Create(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	stored := *s
	r.created[s.ID] = &stored
	return &stored, nil
}

func (r *mockSpaceRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.SharedMemorySpace, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	s, ok := r.created[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s, nil
}

func (r *mockSpaceRepo) List(ctx context.Context, pt domain.PrincipalType, pid string) ([]*domain.SharedMemorySpace, error) {
	out := make([]*domain.SharedMemorySpace, 0, len(r.created))
	for _, s := range r.created {
		out = append(out, s)
	}
	return out, nil
}

func (r *mockSpaceRepo) Update(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	stored := *s
	r.created[s.ID] = &stored
	return &stored, nil
}

func (r *mockSpaceRepo) ListMemberships(ctx context.Context, spaceID uuid.UUID) ([]*domain.SpaceMembership, error) {
	return nil, nil
}
func (r *mockSpaceRepo) AddMembership(ctx context.Context, m *domain.SpaceMembership) error {
	return nil
}
func (r *mockSpaceRepo) RemoveMembership(ctx context.Context, spaceID uuid.UUID, pt domain.PrincipalType, pid string) error {
	return nil
}

type mockProposalRepo struct {
	created  []*domain.PromotionProposal
	byID     map[uuid.UUID]*domain.PromotionProposal
	approved map[uuid.UUID]string
	rejected map[uuid.UUID]string
}

func newMockProposalRepo() *mockProposalRepo {
	return &mockProposalRepo{byID: map[uuid.UUID]*domain.PromotionProposal{}, approved: map[uuid.UUID]string{}, rejected: map[uuid.UUID]string{}}
}

func (r *mockProposalRepo) Create(ctx context.Context, p *domain.PromotionProposal) (*domain.PromotionProposal, error) {
	stored := *p
	r.created = append(r.created, &stored)
	r.byID[p.ID] = &stored
	return &stored, nil
}

func (r *mockProposalRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.PromotionProposal, error) {
	p, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

func (r *mockProposalRepo) ListPending(ctx context.Context, spaceID uuid.UUID) ([]*domain.PromotionProposal, error) {
	var out []*domain.PromotionProposal
	for _, p := range r.created {
		if p.SharedSpaceID == spaceID && !p.Status.Terminal() {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *mockProposalRepo) Approve(ctx context.Context, id uuid.UUID, reviewer string) (*domain.PromotionProposal, error) {
	p, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	stored := *p
	if err := stored.Approve(reviewer, timeNowUTC()); err != nil {
		return nil, err
	}
	r.byID[id] = &stored
	r.approved[id] = reviewer
	return &stored, nil
}

func (r *mockProposalRepo) Reject(ctx context.Context, id uuid.UUID, reviewer, reason string) (*domain.PromotionProposal, error) {
	p, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	stored := *p
	if err := stored.Reject(reviewer, reason, timeNowUTC()); err != nil {
		return nil, err
	}
	r.byID[id] = &stored
	r.rejected[id] = reason
	return &stored, nil
}

type mockRecallRequestRepo struct {
	created  []*domain.RecallRequest
	byID     map[uuid.UUID]*domain.RecallRequest
	statuses []domain.RecallStatus
	getErr   error
}

func newMockRecallRequestRepo() *mockRecallRequestRepo {
	return &mockRecallRequestRepo{byID: map[uuid.UUID]*domain.RecallRequest{}}
}

func (r *mockRecallRequestRepo) Create(ctx context.Context, req *domain.RecallRequest) (*domain.RecallRequest, error) {
	stored := *req
	r.created = append(r.created, &stored)
	r.byID[req.RequestID] = &stored
	return &stored, nil
}

func (r *mockRecallRequestRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecallRequest, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	req, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return req, nil
}

func (r *mockRecallRequestRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.RecallStatus, failure *domain.Error) error {
	r.statuses = append(r.statuses, status)
	if req, ok := r.byID[id]; ok {
		stored := *req
		stored.Status = status
		stored.Error = failure
		r.byID[id] = &stored
	}
	return nil
}

type mockRecallResultRepo struct {
	saved map[uuid.UUID]*domain.RecallResult
}

func newMockRecallResultRepo() *mockRecallResultRepo {
	return &mockRecallResultRepo{saved: map[uuid.UUID]*domain.RecallResult{}}
}

func (r *mockRecallResultRepo) Save(ctx context.Context, res *domain.RecallResult) (*domain.RecallResult, error) {
	stored := *res
	r.saved[res.RequestID] = &stored
	return &stored, nil
}

func (r *mockRecallResultRepo) ListByRequest(ctx context.Context, requestID uuid.UUID) ([]*domain.RecallResult, error) {
	if res, ok := r.saved[requestID]; ok {
		return []*domain.RecallResult{res}, nil
	}
	return nil, nil
}

type mockRecallEngine struct {
	calls  []*domain.RecallRequest
	result *domain.RecallResult
	err    error
}

func (e *mockRecallEngine) Recall(ctx context.Context, req *domain.RecallRequest) (*domain.RecallResult, error) {
	e.calls = append(e.calls, req)
	if e.err != nil {
		return nil, e.err
	}
	if e.result != nil {
		return e.result, nil
	}
	return &domain.RecallResult{
		ResultID:  uuidMust(),
		RequestID: req.RequestID,
		SlotsUsed: 1, TokensUsed: 10,
	}, nil
}

type mockJobRepo struct {
	created []*domain.LifecycleJob
	byID    map[uuid.UUID]*domain.LifecycleJob
}

func newMockJobRepo() *mockJobRepo {
	return &mockJobRepo{byID: map[uuid.UUID]*domain.LifecycleJob{}}
}

func (r *mockJobRepo) Create(ctx context.Context, j *domain.LifecycleJob) (*domain.LifecycleJob, error) {
	stored := *j
	r.created = append(r.created, &stored)
	r.byID[j.JobID] = &stored
	return &stored, nil
}

func (r *mockJobRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.LifecycleJob, error) {
	j, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return j, nil
}

func (r *mockJobRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.JobStatus, result any, failure *domain.Error) error {
	return nil
}

func (r *mockJobRepo) ListPending(ctx context.Context, kind *domain.JobKind, limit int) ([]*domain.LifecycleJob, error) {
	return nil, nil
}

type mockStatsProvider struct {
	stats port.MemoryStats
	err   error
	calls []string
}

func (p *mockStatsProvider) Stats(ctx context.Context, scope string) (port.MemoryStats, error) {
	p.calls = append(p.calls, scope)
	return p.stats, p.err
}

type mockTx struct {
	calls int
	err   error
}

func (t *mockTx) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	t.calls++
	if t.err != nil {
		return t.err
	}
	return fn(ctx)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func timeNowUTC() time.Time { return time.Now().UTC() }

func validMemory() *domain.Memory {
	return &domain.Memory{
		Type: domain.MemoryTypeSemantic, Content: "deploy via runbook v2",
		ContentFormat: domain.ContentFormatPlain, Origin: domain.OriginUserInstruction,
		OwnerPrincipalType: domain.PrincipalUser, OwnerPrincipalID: "u1",
		AccessScope: domain.AccessScopeIndividual, Version: 1,
	}
}

func validSpace(policy domain.PromotePolicy) *domain.SharedMemorySpace {
	return &domain.SharedMemorySpace{
		ID: uuidMust(), Name: "team-brain", OwnerType: domain.SpaceOwnerUser,
		OwnerID: "u1", Scope: "team",
		AccessPolicy: domain.AccessPolicy{
			DefaultAccess: domain.DefaultAccessRead,
			WritePolicy:   domain.WriteOwnerApproved, PromotePolicy: policy,
		},
		StorageBackend: domain.StorageBackend{Kind: domain.BackendHybrid},
		SyncState:      domain.SyncState{Status: domain.SyncStatusInSync},
	}
}

func validProposal(spaceID, candidateID uuid.UUID) *domain.PromotionProposal {
	return &domain.PromotionProposal{
		CandidateMemoryID: candidateID, SharedSpaceID: spaceID,
		TargetPath: "rules/deploy.md", TargetKind: domain.ProposalTargetRule,
		TargetRole: domain.ProposalRoleProcedural, Diff: "+ use runbook v2",
	}
}

func validSession(slots int) *domain.AgentSession {
	return &domain.AgentSession{
		AgentType: domain.AgentClaudeCode, UserID: "u1",
		ContextWindow: domain.ContextWindow{Model: "gpt-x", MaxTokens: 8000, InstructionSlotBudget: slots},
	}
}

func uuidMust() uuid.UUID { return uuidMustV7() }

// ---------------------------------------------------------------------------
// MemoryService tests
// ---------------------------------------------------------------------------

func TestSaveMemory(t *testing.T) {
	repo := newMockMemoryRepo()
	enc := &mockEncoder{vector: pgvector.NewVector([]float32{0.1, 0.2}), infer: domain.MemoryTypeSemantic}
	svc := NewMemoryService(repo, WithEncoder(enc))
	m, err := svc.SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, m.ID)
	assert.Equal(t, 1, m.Version)
	require.NotNil(t, m.Embedding)
	assert.Equal(t, []float32{0.1, 0.2}, m.Embedding.Slice())
	require.NotNil(t, m.DecayScore)
	assert.InDelta(t, 1.0, *m.DecayScore, 1e-9)
	assert.NotNil(t, m.ValidFrom)
	assert.Len(t, repo.saved, 1)
}

func TestSaveMemoryServerStampsProvenance(t *testing.T) {
	repo := newMockMemoryRepo()
	svc := NewMemoryService(repo)
	fake := "client-forged"
	m := validMemory()
	m.AgentID, m.Actor = &fake, &fake
	ctx := WithPrincipal(context.Background(), Principal{AgentID: "srv-agent", Actor: "alice"})
	stored, err := svc.SaveMemory(ctx, m)
	require.NoError(t, err)
	require.NotNil(t, stored.AgentID)
	assert.Equal(t, "srv-agent", *stored.AgentID)
	require.NotNil(t, stored.Actor)
	assert.Equal(t, "alice", *stored.Actor)
}

func TestGetMemory(t *testing.T) {
	repo := newMockMemoryRepo()
	logRepo := &mockAccessLog{}
	svc := NewMemoryService(repo, WithAccessLog(logRepo))
	saved, err := svc.SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	got, err := svc.GetMemory(context.Background(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, saved.ID, got.ID)
	require.Len(t, logRepo.entries, 1)
	assert.Equal(t, domain.AccessTypeDirectGet, logRepo.entries[0].AccessType)
}

func TestListMemories(t *testing.T) {
	repo := newMockMemoryRepo()
	svc := NewMemoryService(repo)
	_, err := svc.SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	page, err := svc.ListMemories(context.Background(), port.NewMemoryFilter())
	require.NoError(t, err)
	assert.Len(t, page.Items, 1)
}

func TestUpdateMemoryMetadata(t *testing.T) {
	repo := newMockMemoryRepo()
	svc := NewMemoryService(repo)
	saved, err := svc.SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	saved.Tags = []string{"runbook"}
	updated, err := svc.UpdateMemory(context.Background(), saved)
	require.NoError(t, err)
	assert.Equal(t, []string{"runbook"}, updated.Tags)
	assert.Equal(t, 1, updated.Version) // metadata-only: same version
}

func TestUpdateMemoryContentSupersession(t *testing.T) {
	repo := newMockMemoryRepo()
	svc := NewMemoryService(repo)
	saved, err := svc.SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	saved.Content = "deploy via runbook v3"
	updated, err := svc.UpdateMemory(context.Background(), saved)
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Version) // content change: next version row
	assert.Equal(t, "deploy via runbook v3", updated.Content)
	require.Len(t, repo.updates, 1)
	assert.Equal(t, 1, repo.updates[0].expect) // optimistic concurrency guard
}

func TestDeleteMemoryExpire(t *testing.T) {
	repo := newMockMemoryRepo()
	svc := NewMemoryService(repo)
	saved, err := svc.SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	require.NoError(t, svc.DeleteMemory(context.Background(), saved.ID))
	assert.Equal(t, []uuid.UUID{saved.ID}, repo.deleted)
}

func TestCreateLink(t *testing.T) {
	repo := newMockMemoryRepo()
	svc := NewMemoryService(repo)
	a, err := svc.SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	b, err := svc.SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	link, err := svc.LinkMemories(context.Background(), &domain.MemoryLink{
		SourceID: a.ID, TargetID: b.ID,
		RelationshipType: domain.RelationshipSimilarTo, Weight: 0.7,
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, link.ID)
}

func TestCreateLinkIntegrity(t *testing.T) {
	repo := newMockMemoryRepo()
	svc := NewMemoryService(repo)
	_, err := svc.LinkMemories(context.Background(), &domain.MemoryLink{
		SourceID: uuidMust(), TargetID: uuidMust(),
		RelationshipType: domain.RelationshipSimilarTo, Weight: 1,
	})
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeLinkIntegrity, de.Code)
}

// ---------------------------------------------------------------------------
// RecallService tests
// ---------------------------------------------------------------------------

func recallRequest(sessionID uuid.UUID) *domain.RecallRequest {
	return &domain.RecallRequest{
		Query: "how do we deploy", AgentID: "a1", SessionID: sessionID,
		Trigger: domain.TriggerUserQuery, Mode: domain.RecallModeSync,
		RetrievalParams: domain.RetrievalParams{
			Strategies: []domain.StrategyType{domain.StrategyVector},
			TopK:       10, Rerank: domain.RerankCrossEncoder, MinScore: 0.35,
		},
	}
}

func TestRecallSync(t *testing.T) {
	sessions := newMockSessionRepo()
	sess, err := sessions.Create(context.Background(), validSession(8))
	require.NoError(t, err)
	engine := &mockRecallEngine{}
	requests, results := newMockRecallRequestRepo(), newMockRecallResultRepo()
	svc := NewRecallService(engine, requests, results, WithRecallSessions(sessions))
	res, err := svc.Recall(context.Background(), recallRequest(sess.SessionID))
	require.NoError(t, err)
	assert.Equal(t, domain.RecallStatusCompleted, requests.byID[res.RequestID].Status)
	assert.Len(t, engine.calls, 1)
	_, ok := results.saved[res.RequestID]
	assert.True(t, ok)
}

func TestRecallAsync(t *testing.T) {
	sessions := newMockSessionRepo()
	sess, _ := sessions.Create(context.Background(), validSession(8))
	requests, results := newMockRecallRequestRepo(), newMockRecallResultRepo()
	svc := NewRecallService(&mockRecallEngine{}, requests, results, WithRecallSessions(sessions))
	req := recallRequest(sess.SessionID)
	req.Mode = domain.RecallModeAsync
	res, err := svc.Recall(context.Background(), req)
	require.NoError(t, err)
	assert.Nil(t, res) // no inline result
	stored := requests.byID[req.RequestID]
	require.NotNil(t, stored)
	assert.Equal(t, domain.RecallStatusQueued, stored.Status)
}

func TestGetRecallStatus(t *testing.T) {
	sessions := newMockSessionRepo()
	sess, _ := sessions.Create(context.Background(), validSession(8))
	requests, results := newMockRecallRequestRepo(), newMockRecallResultRepo()
	svc := NewRecallService(&mockRecallEngine{}, requests, results, WithRecallSessions(sessions))
	sent := recallRequest(sess.SessionID)
	_, err := svc.Recall(context.Background(), sent)
	require.NoError(t, err)
	gotReq, gotRes, err := svc.GetRecallStatus(context.Background(), sent.RequestID)
	require.NoError(t, err)
	require.NotNil(t, gotReq)
	assert.Equal(t, domain.RecallStatusCompleted, gotReq.Status)
	require.NotNil(t, gotRes)
	assert.Equal(t, sent.RequestID, gotRes.RequestID)
}

func TestGetRecallStatusNotFound(t *testing.T) {
	svc := NewRecallService(&mockRecallEngine{}, newMockRecallRequestRepo(), newMockRecallResultRepo())
	_, _, err := svc.GetRecallStatus(context.Background(), uuidMust())
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeRecallNotFound, de.Code)
}

func TestRecallSessionNotFound(t *testing.T) {
	svc := NewRecallService(&mockRecallEngine{}, newMockRecallRequestRepo(), newMockRecallResultRepo(),
		WithRecallSessions(newMockSessionRepo()))
	_, err := svc.Recall(context.Background(), recallRequest(uuidMust()))
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeSessionNotFound, de.Code)
}

func TestRecallBudgetInvalid(t *testing.T) {
	sessions := newMockSessionRepo()
	sess, _ := sessions.Create(context.Background(), validSession(4))
	svc := NewRecallService(&mockRecallEngine{}, newMockRecallRequestRepo(), newMockRecallResultRepo(),
		WithRecallSessions(sessions))
	req := recallRequest(sess.SessionID)
	budget := 9
	req.RetrievalParams.SlotBudget = &budget
	_, err := svc.Recall(context.Background(), req)
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeRecallBudgetInvalid, de.Code)
}

// ---------------------------------------------------------------------------
// SessionService tests
// ---------------------------------------------------------------------------

func TestStartSessionWithoutBootstrap(t *testing.T) {
	svc := NewSessionService(newMockSessionRepo())
	created, plan, err := svc.StartSession(context.Background(), validSession(8))
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, created.SessionID)
	assert.Empty(t, plan)
	assert.Empty(t, created.ActiveMemories)
}

func TestStartSessionWithBootstrap(t *testing.T) {
	repo := newMockSessionRepo()
	svc := NewSessionService(repo)
	m1, m2 := uuidMust(), uuidMust()
	plan := BootstrapPlan{
		{MemoryID: m1, Position: 0, SlotCost: 1},
		{MemoryID: m2, Position: 1, SlotCost: 1},
	}
	created, got, err := svc.StartSession(WithBootstrapPlan(context.Background(), plan), validSession(4))
	require.NoError(t, err)
	assert.Equal(t, []domain.InjectionPlanItem(plan), got)
	assert.Equal(t, []uuid.UUID{m1, m2}, created.ActiveMemories)
	assert.Equal(t, 2, created.ContextWindow.SlotsUsed)
}

func TestStartSessionBootstrapBudgetExceeded(t *testing.T) {
	svc := NewSessionService(newMockSessionRepo())
	plan := BootstrapPlan{{MemoryID: uuidMust(), Position: 0, SlotCost: 1}, {MemoryID: uuidMust(), Position: 1, SlotCost: 1}}
	_, _, err := svc.StartSession(WithBootstrapPlan(context.Background(), plan), validSession(1))
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeSlotBudgetExceeded, de.Code)
}

func TestGetSession(t *testing.T) {
	repo := newMockSessionRepo()
	svc := NewSessionService(repo)
	created, _, err := svc.StartSession(context.Background(), validSession(8))
	require.NoError(t, err)
	got, err := svc.GetSession(context.Background(), created.SessionID)
	require.NoError(t, err)
	assert.Equal(t, created.SessionID, got.SessionID)
}

func TestActivateMemory(t *testing.T) {
	sessions := newMockSessionRepo()
	memories := newMockMemoryRepo()
	svc := NewSessionService(sessions, WithSessionMemories(memories))
	created, _, err := svc.StartSession(context.Background(), validSession(2))
	require.NoError(t, err)
	m, err := NewMemoryService(memories).SaveMemory(context.Background(), validMemory())
	require.NoError(t, err)
	updated, err := svc.ActivateMemory(context.Background(), created.SessionID, m.ID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{m.ID}, updated.ActiveMemories)
}

func TestActivateMemoryBudgetExceeded(t *testing.T) {
	sessions := newMockSessionRepo()
	memories := newMockMemoryRepo()
	svc := NewSessionService(sessions, WithSessionMemories(memories))
	created, _, err := svc.StartSession(context.Background(), validSession(1))
	require.NoError(t, err)
	m, _ := NewMemoryService(memories).SaveMemory(context.Background(), validMemory())
	m2, _ := NewMemoryService(memories).SaveMemory(context.Background(), validMemory())
	_, err = svc.ActivateMemory(context.Background(), created.SessionID, m.ID)
	require.NoError(t, err)
	_, err = svc.ActivateMemory(context.Background(), created.SessionID, m2.ID)
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeSlotBudgetExceeded, de.Code)
}

func TestActivateMemoryIdempotent(t *testing.T) {
	sessions := newMockSessionRepo()
	memories := newMockMemoryRepo()
	svc := NewSessionService(sessions, WithSessionMemories(memories))
	created, _, _ := svc.StartSession(context.Background(), validSession(2))
	m, _ := NewMemoryService(memories).SaveMemory(context.Background(), validMemory())
	_, err := svc.ActivateMemory(context.Background(), created.SessionID, m.ID)
	require.NoError(t, err)
	_, err = svc.ActivateMemory(context.Background(), created.SessionID, m.ID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{m.ID}, sessions.byID[created.SessionID].ActiveMemories)
}

func TestDeactivateMemory(t *testing.T) {
	sessions := newMockSessionRepo()
	memories := newMockMemoryRepo()
	svc := NewSessionService(sessions, WithSessionMemories(memories))
	created, _, _ := svc.StartSession(context.Background(), validSession(2))
	m, _ := NewMemoryService(memories).SaveMemory(context.Background(), validMemory())
	_, err := svc.ActivateMemory(context.Background(), created.SessionID, m.ID)
	require.NoError(t, err)
	updated, err := svc.DeactivateMemory(context.Background(), created.SessionID, m.ID)
	require.NoError(t, err)
	assert.Empty(t, updated.ActiveMemories)
}

func TestEndSessionEnqueuesConsolidation(t *testing.T) {
	sessions := newMockSessionRepo()
	jobs := newMockJobRepo()
	svc := NewSessionService(sessions, WithSessionJobs(jobs))
	created, _, err := svc.StartSession(context.Background(), validSession(8))
	require.NoError(t, err)
	summary := "shipped runbook v2"
	ended, job, err := svc.EndSession(context.Background(), created.SessionID, &summary)
	require.NoError(t, err)
	require.NotNil(t, ended.EndedAt)
	require.NotNil(t, job)
	assert.Equal(t, domain.JobConsolidation, job.Kind)
	assert.Equal(t, domain.JobStatusQueued, job.Status)
	require.Len(t, jobs.created, 1)
	require.NotNil(t, ended.Summary)
	assert.Equal(t, summary, *ended.Summary)
}

// ---------------------------------------------------------------------------
// SpaceService tests
// ---------------------------------------------------------------------------

func TestCreateSpace(t *testing.T) {
	svc := NewSpaceService(newMockSpaceRepo())
	sp, err := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, sp.ID)
}

func TestListSpaces(t *testing.T) {
	svc := NewSpaceService(newMockSpaceRepo())
	_, err := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	require.NoError(t, err)
	spaces, err := svc.ListSpaces(context.Background(), domain.PrincipalUser, "u1")
	require.NoError(t, err)
	assert.Len(t, spaces, 1)
}

func TestPromoteMemory(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, err := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	require.NoError(t, err)
	candidate := validMemory()
	candidate.Origin = domain.OriginConsolidation
	saved, err := NewMemoryService(memories).SaveMemory(context.Background(), candidate)
	require.NoError(t, err)
	p, err := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.ProposalStatusInReview, p.Status)
}

func TestPromoteMemoryDuplicatePrevented(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	candidate := validMemory()
	candidate.Origin = domain.OriginConsolidation
	saved, _ := NewMemoryService(memories).SaveMemory(context.Background(), candidate)
	_, err := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	require.NoError(t, err)
	_, err = svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeProposalAlreadyResolved, de.Code)
}

func TestPromoteMemoryNotConsolidated(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	saved, _ := NewMemoryService(memories).SaveMemory(context.Background(), validMemory()) // origin=user_instruction
	_, err := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodePromotionNotConsolidated, de.Code)
}

func TestPromoteMemoryAutoApproves(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteAuto))
	candidate := validMemory()
	candidate.Origin = domain.OriginConsolidation
	saved, _ := NewMemoryService(memories).SaveMemory(context.Background(), candidate)
	p, err := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.ProposalStatusMerged, p.Status)
	// approve copies the memory into the shared space
	copied := false
	for _, m := range memories.saved {
		if m.AccessScope == domain.AccessScopeShared && m.SharedSpaceID != nil && *m.SharedSpaceID == sp.ID {
			copied = true
		}
	}
	assert.True(t, copied, "auto-promote should copy candidate into space")
}

func TestReviewProposals(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	candidate := validMemory()
	candidate.Origin = domain.OriginConsolidation
	saved, _ := NewMemoryService(memories).SaveMemory(context.Background(), candidate)
	_, err := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	require.NoError(t, err)
	inReview := domain.ProposalStatusInReview
	list, err := svc.ReviewProposals(context.Background(), sp.ID, &inReview)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestApproveProposal(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	candidate := validMemory()
	candidate.Origin = domain.OriginConsolidation
	saved, _ := NewMemoryService(memories).SaveMemory(context.Background(), candidate)
	p, _ := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	approved, err := svc.ApproveProposal(context.Background(), sp.ID, p.ID, "reviewer-1")
	require.NoError(t, err)
	assert.Equal(t, domain.ProposalStatusMerged, approved.Status)
	require.NotNil(t, approved.ReviewedBy)
	assert.Equal(t, "reviewer-1", *approved.ReviewedBy)
}

func TestApproveProposalAlreadyResolved(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	candidate := validMemory()
	candidate.Origin = domain.OriginConsolidation
	saved, _ := NewMemoryService(memories).SaveMemory(context.Background(), candidate)
	p, _ := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	_, err := svc.ApproveProposal(context.Background(), sp.ID, p.ID, "reviewer-1")
	require.NoError(t, err)
	_, err = svc.ApproveProposal(context.Background(), sp.ID, p.ID, "reviewer-1")
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeProposalAlreadyResolved, de.Code)
}

func TestRejectProposal(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	candidate := validMemory()
	candidate.Origin = domain.OriginConsolidation
	saved, _ := NewMemoryService(memories).SaveMemory(context.Background(), candidate)
	p, _ := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	rejected, err := svc.RejectProposal(context.Background(), sp.ID, p.ID, "reviewer-1", "stale runbook")
	require.NoError(t, err)
	assert.Equal(t, domain.ProposalStatusRejected, rejected.Status)
	require.NotNil(t, rejected.RejectReason)
	assert.Equal(t, "stale runbook", *rejected.RejectReason)
}

func TestRejectProposalRequiresReason(t *testing.T) {
	spaces := newMockSpaceRepo()
	proposals := newMockProposalRepo()
	memories := newMockMemoryRepo()
	svc := NewSpaceService(spaces, WithSpaceProposals(proposals), WithSpaceMemories(memories))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	candidate := validMemory()
	candidate.Origin = domain.OriginConsolidation
	saved, _ := NewMemoryService(memories).SaveMemory(context.Background(), candidate)
	p, _ := svc.PromoteMemory(context.Background(), validProposal(sp.ID, saved.ID))
	_, err := svc.RejectProposal(context.Background(), sp.ID, p.ID, "reviewer-1", "")
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeValidationErr, de.Code)
}

func TestSyncSpace(t *testing.T) {
	spaces := newMockSpaceRepo()
	jobs := newMockJobRepo()
	svc := NewSpaceService(spaces, WithSpaceJobs(jobs))
	sp, _ := svc.CreateSpace(context.Background(), validSpace(domain.PromoteHumanReview))
	job, err := svc.SyncSpace(context.Background(), sp.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobSpaceSync, job.Kind)
	assert.Equal(t, domain.JobStatusQueued, job.Status)
	require.Len(t, jobs.created, 1)
}

func TestSyncSpaceNotFound(t *testing.T) {
	svc := NewSpaceService(newMockSpaceRepo(), WithSpaceJobs(newMockJobRepo()))
	_, err := svc.SyncSpace(context.Background(), uuidMust())
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeSpaceNotFound, de.Code)
}

// ---------------------------------------------------------------------------
// LifecycleService tests
// ---------------------------------------------------------------------------

func TestConsolidate(t *testing.T) {
	jobs := newMockJobRepo()
	svc := NewLifecycleService(jobs)
	job, err := svc.Consolidate(context.Background(), "user", "u1")
	require.NoError(t, err)
	assert.Equal(t, domain.JobConsolidation, job.Kind)
	assert.Equal(t, domain.JobStatusQueued, job.Status)
	require.Len(t, jobs.created, 1)
}

func TestDecay(t *testing.T) {
	jobs := newMockJobRepo()
	svc := NewLifecycleService(jobs)
	job, err := svc.Decay(context.Background(), "global", "")
	require.NoError(t, err)
	assert.Equal(t, domain.JobDecay, job.Kind)
	assert.Equal(t, domain.JobStatusQueued, job.Status)
}

func TestGetJob(t *testing.T) {
	jobs := newMockJobRepo()
	svc := NewLifecycleService(jobs)
	created, err := svc.Consolidate(context.Background(), "global", "")
	require.NoError(t, err)
	got, err := svc.GetJob(context.Background(), created.JobID)
	require.NoError(t, err)
	assert.Equal(t, created.JobID, got.JobID)
}

func TestLifecycleScopeInvalid(t *testing.T) {
	svc := NewLifecycleService(newMockJobRepo())
	_, err := svc.Consolidate(context.Background(), "galactic", "")
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeValidationErr, de.Code)
}

func TestGetStats(t *testing.T) {
	stats := &mockStatsProvider{stats: port.MemoryStats{Counts: port.StatCounts{Total: 5}}}
	svc := NewLifecycleService(newMockJobRepo(), WithStatsProvider(stats))
	got, err := svc.GetStats(context.Background(), "user:u1")
	require.NoError(t, err)
	assert.Equal(t, 5, got.Counts.Total)
	assert.Equal(t, "user:u1", got.Scope)
	assert.Equal(t, []string{"user:u1"}, stats.calls)
}

func TestGetStatsNoProvider(t *testing.T) {
	svc := NewLifecycleService(newMockJobRepo())
	_, err := svc.GetStats(context.Background(), "global")
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.CodeInternal, de.Code)
}

// Compile-time interface guards for the mocks.
var (
	_ port.MemoryRepository            = (*mockMemoryRepo)(nil)
	_ port.MemoryAccessLogRepository   = (*mockAccessLog)(nil)
	_ port.MemoryEncoder               = (*mockEncoder)(nil)
	_ port.AgentSessionRepository      = (*mockSessionRepo)(nil)
	_ port.SharedMemorySpaceRepository = (*mockSpaceRepo)(nil)
	_ port.PromotionProposalRepository = (*mockProposalRepo)(nil)
	_ port.RecallRequestRepository     = (*mockRecallRequestRepo)(nil)
	_ port.RecallResultRepository      = (*mockRecallResultRepo)(nil)
	_ port.RecallEngine                = (*mockRecallEngine)(nil)
	_ port.LifecycleJobRepository      = (*mockJobRepo)(nil)
	_ port.TransactionManager          = (*mockTx)(nil)
)
