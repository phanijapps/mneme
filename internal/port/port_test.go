package port

// Compile-time interface compliance tests: every port interface must be
// satisfiable by a mock implementation. If a method signature drifts, these
// declarations break the build — no runtime test needed for that guarantee.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
)

// -- mock MemoryRepository ---------------------------------------------------

type mockMemoryRepo struct{}

func (m *mockMemoryRepo) Save(ctx context.Context, mem *domain.Memory) (*domain.Memory, error) {
	return mem, nil
}
func (m *mockMemoryRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Memory, error) {
	return &domain.Memory{}, nil
}
func (m *mockMemoryRepo) GetByVersion(ctx context.Context, id uuid.UUID, version int) (*domain.Memory, error) {
	return &domain.Memory{}, nil
}
func (m *mockMemoryRepo) List(ctx context.Context, f MemoryFilter) (Page[*domain.Memory], error) {
	return Page[*domain.Memory]{}, nil
}
func (m *mockMemoryRepo) Update(ctx context.Context, mem *domain.Memory, expectedVersion int) (*domain.Memory, error) {
	return mem, nil
}
func (m *mockMemoryRepo) SoftDelete(ctx context.Context, id uuid.UUID) error { return nil }
func (m *mockMemoryRepo) Purge(ctx context.Context, id uuid.UUID) error      { return nil }
func (m *mockMemoryRepo) SaveLink(ctx context.Context, l *domain.MemoryLink) (*domain.MemoryLink, error) {
	return l, nil
}
func (m *mockMemoryRepo) ListLinks(ctx context.Context, id uuid.UUID, f LinkFilter) ([]*domain.MemoryLink, error) {
	return nil, nil
}

var _ MemoryRepository = (*mockMemoryRepo)(nil)

// -- mock EntityRepository ---------------------------------------------------

type mockEntityRepo struct{}

func (m *mockEntityRepo) Save(ctx context.Context, e *domain.Entity) (*domain.Entity, error) {
	return e, nil
}
func (m *mockEntityRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Entity, error) {
	return &domain.Entity{}, nil
}
func (m *mockEntityRepo) GetByName(ctx context.Context, name string) (*domain.Entity, error) {
	return &domain.Entity{}, nil
}
func (m *mockEntityRepo) ListByMemory(ctx context.Context, memoryID uuid.UUID) ([]*domain.Entity, error) {
	return nil, nil
}
func (m *mockEntityRepo) SaveMemoryEntity(ctx context.Context, me *domain.MemoryEntity) error {
	return nil
}
func (m *mockEntityRepo) SaveFact(ctx context.Context, f *domain.EntityFact) (*domain.EntityFact, error) {
	return f, nil
}
func (m *mockEntityRepo) ListFacts(ctx context.Context, entityID uuid.UUID) ([]*domain.EntityFact, error) {
	return nil, nil
}

var _ EntityRepository = (*mockEntityRepo)(nil)

// -- mock AgentSessionRepository ----------------------------------------------

type mockSessionRepo struct{}

func (m *mockSessionRepo) Create(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, error) {
	return s, nil
}
func (m *mockSessionRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.AgentSession, error) {
	return &domain.AgentSession{}, nil
}
func (m *mockSessionRepo) Update(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, error) {
	return s, nil
}
func (m *mockSessionRepo) EndSession(ctx context.Context, id uuid.UUID, summary *string) (*domain.AgentSession, error) {
	return &domain.AgentSession{}, nil
}
func (m *mockSessionRepo) ActivateMemory(ctx context.Context, sid, mid uuid.UUID) (*domain.AgentSession, error) {
	return &domain.AgentSession{}, nil
}
func (m *mockSessionRepo) DeactivateMemory(ctx context.Context, sid, mid uuid.UUID) (*domain.AgentSession, error) {
	return &domain.AgentSession{}, nil
}

var _ AgentSessionRepository = (*mockSessionRepo)(nil)

// -- mock SharedMemorySpaceRepository -----------------------------------------

type mockSpaceRepo struct{}

func (m *mockSpaceRepo) Create(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	return s, nil
}
func (m *mockSpaceRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.SharedMemorySpace, error) {
	return &domain.SharedMemorySpace{}, nil
}
func (m *mockSpaceRepo) List(ctx context.Context, pt domain.PrincipalType, pid string) ([]*domain.SharedMemorySpace, error) {
	return nil, nil
}
func (m *mockSpaceRepo) Update(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	return s, nil
}
func (m *mockSpaceRepo) ListMemberships(ctx context.Context, id uuid.UUID) ([]*domain.SpaceMembership, error) {
	return nil, nil
}
func (m *mockSpaceRepo) AddMembership(ctx context.Context, mbr *domain.SpaceMembership) error {
	return nil
}
func (m *mockSpaceRepo) RemoveMembership(ctx context.Context, id uuid.UUID, pt domain.PrincipalType, pid string) error {
	return nil
}

var _ SharedMemorySpaceRepository = (*mockSpaceRepo)(nil)

// -- mock PromotionProposalRepository -----------------------------------------

type mockProposalRepo struct{}

func (m *mockProposalRepo) Create(ctx context.Context, p *domain.PromotionProposal) (*domain.PromotionProposal, error) {
	return p, nil
}
func (m *mockProposalRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.PromotionProposal, error) {
	return &domain.PromotionProposal{}, nil
}
func (m *mockProposalRepo) ListPending(ctx context.Context, spaceID uuid.UUID) ([]*domain.PromotionProposal, error) {
	return nil, nil
}
func (m *mockProposalRepo) Approve(ctx context.Context, id uuid.UUID, reviewer string) (*domain.PromotionProposal, error) {
	return &domain.PromotionProposal{}, nil
}
func (m *mockProposalRepo) Reject(ctx context.Context, id uuid.UUID, reviewer, reason string) (*domain.PromotionProposal, error) {
	return &domain.PromotionProposal{}, nil
}

var _ PromotionProposalRepository = (*mockProposalRepo)(nil)

// -- mock Recall repositories --------------------------------------------------

type mockRecallRequestRepo struct{}

func (m *mockRecallRequestRepo) Create(ctx context.Context, r *domain.RecallRequest) (*domain.RecallRequest, error) {
	return r, nil
}
func (m *mockRecallRequestRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.RecallRequest, error) {
	return &domain.RecallRequest{}, nil
}
func (m *mockRecallRequestRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.RecallStatus, failure *domain.Error) error {
	return nil
}

var _ RecallRequestRepository = (*mockRecallRequestRepo)(nil)

type mockRecallResultRepo struct{}

func (m *mockRecallResultRepo) Save(ctx context.Context, r *domain.RecallResult) (*domain.RecallResult, error) {
	return r, nil
}
func (m *mockRecallResultRepo) ListByRequest(ctx context.Context, id uuid.UUID) ([]*domain.RecallResult, error) {
	return nil, nil
}

var _ RecallResultRepository = (*mockRecallResultRepo)(nil)

// -- mock Embedding repositories ------------------------------------------------

type mockEmbeddingModelRepo struct{}

func (m *mockEmbeddingModelRepo) Save(ctx context.Context, mdl *domain.EmbeddingModel) (*domain.EmbeddingModel, error) {
	return mdl, nil
}
func (m *mockEmbeddingModelRepo) GetByID(ctx context.Context, modelID string) (*domain.EmbeddingModel, error) {
	return &domain.EmbeddingModel{}, nil
}
func (m *mockEmbeddingModelRepo) List(ctx context.Context, activeOnly bool) ([]*domain.EmbeddingModel, error) {
	return nil, nil
}

var _ EmbeddingModelRepository = (*mockEmbeddingModelRepo)(nil)

type mockMemoryEmbeddingRepo struct{}

func (m *mockMemoryEmbeddingRepo) Save(ctx context.Context, e *domain.MemoryEmbedding) error {
	return nil
}
func (m *mockMemoryEmbeddingRepo) GetByMemoryID(ctx context.Context, id uuid.UUID) ([]*domain.MemoryEmbedding, error) {
	return nil, nil
}
func (m *mockMemoryEmbeddingRepo) ListByModel(ctx context.Context, modelID string, limit int) ([]*domain.MemoryEmbedding, error) {
	return nil, nil
}

var _ MemoryEmbeddingRepository = (*mockMemoryEmbeddingRepo)(nil)

// -- mock LifecycleJobRepository -------------------------------------------------

type mockLifecycleJobRepo struct{}

func (m *mockLifecycleJobRepo) Create(ctx context.Context, j *domain.LifecycleJob) (*domain.LifecycleJob, error) {
	return j, nil
}
func (m *mockLifecycleJobRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.LifecycleJob, error) {
	return &domain.LifecycleJob{}, nil
}
func (m *mockLifecycleJobRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.JobStatus, result any, failure *domain.Error) error {
	return nil
}
func (m *mockLifecycleJobRepo) ListPending(ctx context.Context, kind *domain.JobKind, limit int) ([]*domain.LifecycleJob, error) {
	return nil, nil
}

var _ LifecycleJobRepository = (*mockLifecycleJobRepo)(nil)

// -- mock MemoryAccessLogRepository ----------------------------------------------

type mockAccessLogRepo struct{}

func (m *mockAccessLogRepo) Append(ctx context.Context, entry *domain.MemoryAccessLog) error {
	return nil
}
func (m *mockAccessLogRepo) ListByMemory(ctx context.Context, id uuid.UUID, limit int) ([]*domain.MemoryAccessLog, error) {
	return nil, nil
}

var _ MemoryAccessLogRepository = (*mockAccessLogRepo)(nil)

// -- mock engine / encoder / services / tx ----------------------------------------

type mockRecallEngine struct{}

func (m *mockRecallEngine) Recall(ctx context.Context, req *domain.RecallRequest) (*domain.RecallResult, error) {
	return &domain.RecallResult{}, nil
}

var _ RecallEngine = (*mockRecallEngine)(nil)

type mockEncoder struct{}

func (m *mockEncoder) Encode(ctx context.Context, content string, opts EncodeOptions) (EncodedMemory, error) {
	return EncodedMemory{}, nil
}
func (m *mockEncoder) EncodeBatch(ctx context.Context, contents []string, opts EncodeOptions) ([]EncodedMemory, error) {
	return nil, nil
}
func (m *mockEncoder) ListModels(ctx context.Context) ([]*domain.EmbeddingModel, error) {
	return nil, nil
}

var _ MemoryEncoder = (*mockEncoder)(nil)

type mockMemoryService struct{ mockMemoryRepo }

var _ MemoryService = (*mockMemoryService)(nil)

func (m *mockMemoryService) SaveMemory(ctx context.Context, mem *domain.Memory) (*domain.Memory, error) {
	return mem, nil
}
func (m *mockMemoryService) GetMemory(ctx context.Context, id uuid.UUID) (*domain.Memory, error) {
	return &domain.Memory{}, nil
}
func (m *mockMemoryService) ListMemories(ctx context.Context, f MemoryFilter) (Page[*domain.Memory], error) {
	return Page[*domain.Memory]{}, nil
}
func (m *mockMemoryService) UpdateMemory(ctx context.Context, mem *domain.Memory) (*domain.Memory, error) {
	return mem, nil
}
func (m *mockMemoryService) DeleteMemory(ctx context.Context, id uuid.UUID) error { return nil }
func (m *mockMemoryService) LinkMemories(ctx context.Context, l *domain.MemoryLink) (*domain.MemoryLink, error) {
	return l, nil
}

type mockRecallService struct{}

func (m *mockRecallService) Recall(ctx context.Context, req *domain.RecallRequest) (*domain.RecallResult, error) {
	return &domain.RecallResult{}, nil
}
func (m *mockRecallService) GetRecallStatus(ctx context.Context, id uuid.UUID) (*domain.RecallRequest, *domain.RecallResult, error) {
	return &domain.RecallRequest{}, &domain.RecallResult{}, nil
}

var _ RecallService = (*mockRecallService)(nil)

type mockSessionService struct{}

func (m *mockSessionService) StartSession(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, []domain.InjectionPlanItem, error) {
	return s, nil, nil
}
func (m *mockSessionService) GetSession(ctx context.Context, id uuid.UUID) (*domain.AgentSession, error) {
	return &domain.AgentSession{}, nil
}
func (m *mockSessionService) ActivateMemory(ctx context.Context, sid, mid uuid.UUID) (*domain.AgentSession, error) {
	return &domain.AgentSession{}, nil
}
func (m *mockSessionService) DeactivateMemory(ctx context.Context, sid, mid uuid.UUID) (*domain.AgentSession, error) {
	return &domain.AgentSession{}, nil
}
func (m *mockSessionService) EndSession(ctx context.Context, id uuid.UUID, summary *string) (*domain.AgentSession, *domain.LifecycleJob, error) {
	return &domain.AgentSession{}, &domain.LifecycleJob{}, nil
}

var _ SessionService = (*mockSessionService)(nil)

type mockSpaceService struct{}

func (m *mockSpaceService) CreateSpace(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	return s, nil
}
func (m *mockSpaceService) ListSpaces(ctx context.Context, pt domain.PrincipalType, pid string) ([]*domain.SharedMemorySpace, error) {
	return nil, nil
}
func (m *mockSpaceService) GetSpace(ctx context.Context, id uuid.UUID) (*domain.SharedMemorySpace, error) {
	return &domain.SharedMemorySpace{}, nil
}
func (m *mockSpaceService) UpdateSpace(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	return s, nil
}
func (m *mockSpaceService) PromoteMemory(ctx context.Context, p *domain.PromotionProposal) (*domain.PromotionProposal, error) {
	return p, nil
}
func (m *mockSpaceService) ReviewProposals(ctx context.Context, spaceID uuid.UUID, status *domain.ProposalStatus) ([]*domain.PromotionProposal, error) {
	return nil, nil
}
func (m *mockSpaceService) ApproveProposal(ctx context.Context, spaceID, proposalID uuid.UUID, reviewer string) (*domain.PromotionProposal, error) {
	return &domain.PromotionProposal{}, nil
}
func (m *mockSpaceService) RejectProposal(ctx context.Context, spaceID, proposalID uuid.UUID, reviewer, reason string) (*domain.PromotionProposal, error) {
	return &domain.PromotionProposal{}, nil
}
func (m *mockSpaceService) SyncSpace(ctx context.Context, spaceID uuid.UUID) (*domain.LifecycleJob, error) {
	return &domain.LifecycleJob{}, nil
}

var _ SpaceService = (*mockSpaceService)(nil)

type mockLifecycleService struct{}

func (m *mockLifecycleService) Consolidate(ctx context.Context, scopeKind, scopeID string) (*domain.LifecycleJob, error) {
	return &domain.LifecycleJob{}, nil
}
func (m *mockLifecycleService) Decay(ctx context.Context, scopeKind, scopeID string) (*domain.LifecycleJob, error) {
	return &domain.LifecycleJob{}, nil
}
func (m *mockLifecycleService) GetJob(ctx context.Context, id uuid.UUID) (*domain.LifecycleJob, error) {
	return &domain.LifecycleJob{}, nil
}
func (m *mockLifecycleService) GetStats(ctx context.Context, scope string) (MemoryStats, error) {
	return MemoryStats{}, nil
}

var _ LifecycleService = (*mockLifecycleService)(nil)

type mockTxManager struct{}

func (m *mockTxManager) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

var _ TransactionManager = (*mockTxManager)(nil)

// -- behavioral tests -------------------------------------------------------------

func TestInterfacesSatisfied(t *testing.T) {
	// The var _ declarations above carry the compile-time guarantee; this
	// runtime check exercises the mocks once so coverage and unused-symbol
	// linters stay quiet without asserting adapter behavior.
	ctx := context.Background()
	if _, err := (&mockMemoryService{}).SaveMemory(ctx, &domain.Memory{}); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	if err := (&mockTxManager{}).WithTx(ctx, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if f := NewMemoryFilter(WithTextQuery("x"), WithPagination(10, "")); f.Limit != 10 {
		t.Fatalf("filter defaults wrong: %+v", f)
	}
}
