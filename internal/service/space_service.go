package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// SpaceService implements port.SpaceService.
type SpaceService struct {
	spaces    port.SharedMemorySpaceRepository
	proposals port.PromotionProposalRepository
	memories  port.MemoryRepository
	jobs      port.LifecycleJobRepository
	tx        port.TransactionManager
	now       clock
}

// SpaceServiceOption configures optional SpaceService dependencies.
type SpaceServiceOption func(*SpaceService)

// WithSpaceProposals attaches the promotion proposal repository.
func WithSpaceProposals(r port.PromotionProposalRepository) SpaceServiceOption {
	return func(s *SpaceService) { s.proposals = r }
}

// WithSpaceMemories attaches the memory repository (candidate checks,
// approve-time copy).
func WithSpaceMemories(r port.MemoryRepository) SpaceServiceOption {
	return func(s *SpaceService) { s.memories = r }
}

// WithSpaceJobs attaches the lifecycle job ledger (SyncSpace trigger).
func WithSpaceJobs(r port.LifecycleJobRepository) SpaceServiceOption {
	return func(s *SpaceService) { s.jobs = r }
}

// WithSpaceTx attaches a TransactionManager for multi-step operations.
func WithSpaceTx(tx port.TransactionManager) SpaceServiceOption {
	return func(s *SpaceService) { s.tx = tx }
}

// WithSpaceClock overrides the service clock (tests).
func WithSpaceClock(c clock) SpaceServiceOption {
	return func(s *SpaceService) { s.now = c }
}

// NewSpaceService builds a SpaceService over the space repository.
func NewSpaceService(repo port.SharedMemorySpaceRepository, opts ...SpaceServiceOption) *SpaceService {
	s := &SpaceService{spaces: repo, now: defaultClock}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

var _ port.SpaceService = (*SpaceService)(nil)

// CreateSpace validates and persists a new shared space.
func (s *SpaceService) CreateSpace(ctx context.Context, sp *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	if sp == nil {
		return nil, domain.NewValidationError("SharedMemorySpace", "<nil>", "non-nil")
	}
	if sp.ID == uuid.Nil {
		sp.ID = uuidMustV7()
	}
	now := s.now()
	sp.CreatedAt, sp.UpdatedAt = now, now
	if err := sp.Validate(); err != nil {
		return nil, err
	}
	created, err := s.spaces.Create(ctx, sp)
	if err != nil {
		return nil, fmt.Errorf("create space: %w", err)
	}
	return created, nil
}

// ListSpaces returns spaces readable by a principal (membership-scoped).
func (s *SpaceService) ListSpaces(ctx context.Context, principalType domain.PrincipalType, principalID string) ([]*domain.SharedMemorySpace, error) {
	spaces, err := s.spaces.List(ctx, principalType, principalID)
	if err != nil {
		return nil, fmt.Errorf("list spaces for %s %s: %w", principalType, principalID, err)
	}
	return spaces, nil
}

// GetSpace returns one space by id.
func (s *SpaceService) GetSpace(ctx context.Context, spaceID uuid.UUID) (*domain.SharedMemorySpace, error) {
	sp, err := s.spaces.GetByID(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("get space %s: %w", spaceID, err)
	}
	return sp, nil
}

// UpdateSpace persists attribute changes after re-validation.
func (s *SpaceService) UpdateSpace(ctx context.Context, sp *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error) {
	if sp == nil || sp.ID == uuid.Nil {
		return nil, domain.NewValidationError("SharedMemorySpace.ID", "<nil>", "non-nil id")
	}
	sp.UpdatedAt = s.now()
	if err := sp.Validate(); err != nil {
		return nil, err
	}
	updated, err := s.spaces.Update(ctx, sp)
	if err != nil {
		return nil, fmt.Errorf("update space %s: %w", sp.ID, err)
	}
	return updated, nil
}

// PromoteMemory creates a promotion proposal. Duplicate open proposals per
// (candidate, space) are rejected (review F10); candidates lacking
// consolidation lineage are rejected (PROMOTION_NOT_CONSOLIDATED). Under
// promote_policy=auto the proposal is approved and merged immediately.
func (s *SpaceService) PromoteMemory(ctx context.Context, p *domain.PromotionProposal) (*domain.PromotionProposal, error) {
	if p == nil {
		return nil, domain.NewValidationError("PromotionProposal", "<nil>", "non-nil")
	}
	sp, err := s.spaces.GetByID(ctx, p.SharedSpaceID)
	if err != nil {
		return nil, &domain.Error{
			Code:    domain.CodeSpaceNotFound,
			Message: fmt.Sprintf("space %s not found", p.SharedSpaceID),
		}
	}
	if s.memories != nil {
		candidate, err := s.memories.GetByID(ctx, p.CandidateMemoryID)
		if err != nil {
			return nil, &domain.Error{
				Code:    domain.CodeMemoryNotFound,
				Message: fmt.Sprintf("candidate memory %s not found", p.CandidateMemoryID),
			}
		}
		if candidate.Origin != domain.OriginConsolidation {
			return nil, &domain.Error{
				Code: domain.CodePromotionNotConsolidated,
				Message: fmt.Sprintf("candidate %s has origin %s; only consolidation lineage is promotable",
					p.CandidateMemoryID, candidate.Origin),
				Details: map[string]any{"origin": candidate.Origin},
			}
		}
	}
	if s.proposals != nil {
		pending, err := s.proposals.ListPending(ctx, p.SharedSpaceID)
		if err != nil {
			return nil, fmt.Errorf("list pending proposals for %s: %w", p.SharedSpaceID, err)
		}
		for _, q := range pending {
			if q.CandidateMemoryID == p.CandidateMemoryID {
				return nil, &domain.Error{
					Code: domain.CodeProposalAlreadyResolved,
					Message: fmt.Sprintf("open proposal %s already exists for candidate %s",
						q.ID, p.CandidateMemoryID),
					Details: map[string]any{"existing_proposal_id": q.ID},
				}
			}
		}
	}
	now := s.now()
	p.ID = uuidMustV7()
	p.ProposedAt, p.CreatedAt, p.UpdatedAt = now, now, now
	p.Status = domain.ProposalStatusInReview
	if err := p.Validate(); err != nil {
		return nil, err
	}
	created, err := s.proposals.Create(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("create proposal: %w", err)
	}
	if sp.AccessPolicy.PromotePolicy == domain.PromoteAuto {
		approved, err := s.approve(ctx, sp, created, "auto-promote")
		if err != nil {
			return nil, err
		}
		return approved, nil
	}
	return created, nil
}

// ReviewProposals lists a space's proposals, optionally narrowed by status.
func (s *SpaceService) ReviewProposals(ctx context.Context, spaceID uuid.UUID, status *domain.ProposalStatus) ([]*domain.PromotionProposal, error) {
	proposals, err := s.proposals.ListPending(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("list proposals for space %s: %w", spaceID, err)
	}
	if status == nil {
		return proposals, nil
	}
	filtered := make([]*domain.PromotionProposal, 0, len(proposals))
	for _, p := range proposals {
		if p.Status == *status {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// ApproveProposal merges the proposal and copies the candidate memory into
// the shared space (access_scope=shared, shared_space_id set).
func (s *SpaceService) ApproveProposal(ctx context.Context, spaceID, proposalID uuid.UUID, reviewer string) (*domain.PromotionProposal, error) {
	sp, err := s.spaces.GetByID(ctx, spaceID)
	if err != nil {
		return nil, &domain.Error{
			Code:    domain.CodeSpaceNotFound,
			Message: fmt.Sprintf("space %s not found", spaceID),
		}
	}
	proposal, err := s.proposals.GetByID(ctx, proposalID)
	if err != nil || proposal.SharedSpaceID != spaceID {
		return nil, &domain.Error{
			Code:    domain.CodeProposalNotFound,
			Message: fmt.Sprintf("proposal %s not found in space %s", proposalID, spaceID),
		}
	}
	return s.approve(ctx, sp, proposal, reviewer)
}

// approve resolves a proposal as merged and copies the memory into the space.
func (s *SpaceService) approve(ctx context.Context, sp *domain.SharedMemorySpace, proposal *domain.PromotionProposal, reviewer string) (*domain.PromotionProposal, error) {
	var copyErr error
	run := func(ctx context.Context) error {
		approved, err := s.proposals.Approve(ctx, proposal.ID, reviewer)
		if err != nil {
			return err
		}
		proposal = approved
		if s.memories != nil {
			if _, copyErr = s.copyCandidateIntoSpace(ctx, sp, approved); copyErr != nil {
				return copyErr
			}
		}
		return nil
	}
	if s.tx != nil {
		if err := s.tx.WithTx(ctx, run); err != nil {
			return nil, err
		}
	} else if err := run(ctx); err != nil {
		return nil, err
	}
	return proposal, nil
}

// copyCandidateIntoSpace snapshots the candidate as a shared-space memory.
func (s *SpaceService) copyCandidateIntoSpace(ctx context.Context, sp *domain.SharedMemorySpace, p *domain.PromotionProposal) (*domain.Memory, error) {
	candidate, err := s.memories.GetByID(ctx, p.CandidateMemoryID)
	if err != nil {
		return nil, fmt.Errorf("load candidate %s: %w", p.CandidateMemoryID, err)
	}
	now := s.now()
	shared := *candidate
	shared.ID = uuidMustV7()
	shared.Version = 1
	shared.AccessScope = domain.AccessScopeShared
	shared.SharedSpaceID = &sp.ID
	shared.Origin = domain.OriginConsolidation
	shared.SupersededBy = nil
	shared.DeletedAt = nil
	shared.CreatedAt, shared.UpdatedAt = now, now
	validFrom := now
	shared.ValidFrom, shared.ValidUntil = &validFrom, nil
	return s.memories.Save(ctx, &shared)
}

// RejectProposal rejects a proposal; a reason is mandatory (review F7).
func (s *SpaceService) RejectProposal(ctx context.Context, spaceID, proposalID uuid.UUID, reviewer, reason string) (*domain.PromotionProposal, error) {
	proposal, err := s.proposals.GetByID(ctx, proposalID)
	if err != nil || proposal.SharedSpaceID != spaceID {
		return nil, &domain.Error{
			Code:    domain.CodeProposalNotFound,
			Message: fmt.Sprintf("proposal %s not found in space %s", proposalID, spaceID),
		}
	}
	if reason == "" {
		return nil, domain.NewValidationError("PromotionProposal.RejectReason", "", "required on reject")
	}
	rejected, err := s.proposals.Reject(ctx, proposalID, reviewer, reason)
	if err != nil {
		return nil, fmt.Errorf("reject proposal %s: %w", proposalID, err)
	}
	return rejected, nil
}

// SyncSpace triggers backend synchronization by enqueuing a space_sync job
// (kind=space_sync); the v1 stub returns the queued job ledger entry.
func (s *SpaceService) SyncSpace(ctx context.Context, spaceID uuid.UUID) (*domain.LifecycleJob, error) {
	if _, err := s.spaces.GetByID(ctx, spaceID); err != nil {
		return nil, &domain.Error{
			Code:    domain.CodeSpaceNotFound,
			Message: fmt.Sprintf("space %s not found", spaceID),
		}
	}
	scopeID := spaceID.String()
	job := &domain.LifecycleJob{
		JobID:     uuidMustV7(),
		Kind:      domain.JobSpaceSync,
		Status:    domain.JobStatusQueued,
		ScopeKind: ptr("space"),
		ScopeID:   &scopeID,
		CreatedAt: s.now(),
	}
	if err := job.Validate(); err != nil {
		return nil, err
	}
	if s.jobs == nil {
		return job, nil
	}
	stored, err := s.jobs.Create(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("enqueue sync for space %s: %w", spaceID, err)
	}
	return stored, nil
}
