package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// ProposalRepo implements port.PromotionProposalRepository on PostgreSQL.
type ProposalRepo struct {
	pool *pgxpool.Pool
}

// NewProposalRepo returns a PromotionProposalRepository bound to pool.
func NewProposalRepo(pool *pgxpool.Pool) *ProposalRepo { return &ProposalRepo{pool: pool} }

var _ port.PromotionProposalRepository = (*ProposalRepo)(nil)

const proposalCols = `proposal_id, shared_space_id, candidate_memory_id, target_path,
	target_kind, target_role, diff, status, note, reject_reason,
	proposed_at, resolved_at, reviewer`

// Create inserts an open proposal; the partial unique index (review F10)
// rejects a second open proposal per (candidate, space) pair.
func (r *ProposalRepo) Create(ctx context.Context, p *domain.PromotionProposal) (*domain.PromotionProposal, error) {
	if p.ID == uuid.Nil {
		p.ID = uuid.Must(uuid.NewV7())
	}
	if p.ProposedAt.IsZero() {
		p.ProposedAt = time.Now().UTC()
	}
	if p.Status == "" {
		p.Status = domain.ProposalStatusInReview
	}
	row := querier(ctx, r.pool).QueryRow(ctx, `INSERT INTO promotion_proposals
		(proposal_id, shared_space_id, candidate_memory_id, target_path,
		 target_kind, target_role, diff, status, note, reject_reason,
		 proposed_at, resolved_at, reviewer)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+proposalCols,
		p.ID, p.SharedSpaceID, p.CandidateMemoryID, p.TargetPath, p.TargetKind,
		p.TargetRole, p.Diff, p.Status, p.Note, p.RejectReason, p.ProposedAt,
		p.ReviewedAt, p.ReviewedBy)
	return scanProposal(row)
}

// GetByID fetches one proposal.
func (r *ProposalRepo) GetByID(ctx context.Context, proposalID uuid.UUID) (*domain.PromotionProposal, error) {
	return scanProposal(querier(ctx, r.pool).QueryRow(ctx,
		`SELECT `+proposalCols+` FROM promotion_proposals WHERE proposal_id = $1`, proposalID))
}

// ListPending returns the open (draft/in_review) proposals of a space.
func (r *ProposalRepo) ListPending(ctx context.Context, spaceID uuid.UUID) ([]*domain.PromotionProposal, error) {
	rows, err := querier(ctx, r.pool).Query(ctx, `SELECT `+proposalCols+`
		FROM promotion_proposals
		WHERE shared_space_id = $1 AND status IN ('draft', 'in_review')
		ORDER BY proposed_at DESC`, spaceID)
	if err != nil {
		return nil, mapErr(err, nil)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.PromotionProposal, error) {
		return scanProposal(row)
	})
}

// Approve transitions a proposal to merged (terminal), recording reviewer and
// resolution time; re-resolving a terminal proposal fails (F10).
func (r *ProposalRepo) Approve(ctx context.Context, proposalID uuid.UUID, reviewer string) (*domain.PromotionProposal, error) {
	return r.resolve(ctx, proposalID, "merged", reviewer, nil)
}

// Reject transitions a proposal to rejected (terminal) with a mandatory
// reason (review F7).
func (r *ProposalRepo) Reject(ctx context.Context, proposalID uuid.UUID, reviewer, reason string) (*domain.PromotionProposal, error) {
	return r.resolve(ctx, proposalID, "rejected", reviewer, &reason)
}

// resolve performs the guarded terminal transition.
func (r *ProposalRepo) resolve(ctx context.Context, proposalID uuid.UUID, status, reviewer string, reason *string) (*domain.PromotionProposal, error) {
	row := querier(ctx, r.pool).QueryRow(ctx, `UPDATE promotion_proposals
		SET status = $2, reviewer = $3, resolved_at = now(), reject_reason = $4
		WHERE proposal_id = $1 AND status IN ('draft', 'in_review')
		RETURNING `+proposalCols, proposalID, status, reviewer, reason)
	p, err := scanProposal(row)
	if err != nil {
		if de, ok := err.(*domain.Error); ok && de.Code == domain.CodeProposalNotFound {
			if existing, gerr := r.GetByID(ctx, proposalID); gerr == nil && existing.Status.Terminal() {
				return nil, &domain.Error{Code: domain.CodeProposalAlreadyResolved,
					Message: "proposal " + proposalID.String() + " is already resolved",
					Details: map[string]any{"status": existing.Status}}
			}
			return nil, notFound(domain.CodeProposalNotFound, "proposal")
		}
		return nil, err
	}
	return p, nil
}

func scanProposal(row pgx.Row) (*domain.PromotionProposal, error) {
	var p domain.PromotionProposal
	if err := row.Scan(&p.ID, &p.SharedSpaceID, &p.CandidateMemoryID, &p.TargetPath,
		&p.TargetKind, &p.TargetRole, &p.Diff, &p.Status, &p.Note, &p.RejectReason,
		&p.ProposedAt, &p.ReviewedAt, &p.ReviewedBy); err != nil {
		return nil, mapErr(err, notFound(domain.CodeProposalNotFound, "proposal"))
	}
	// updated_at is not persisted (per DDL §3.8); resolution time is the closest
	// semantic match for the domain field.
	if p.ReviewedAt != nil {
		p.UpdatedAt = *p.ReviewedAt
	} else {
		p.UpdatedAt = p.ProposedAt
	}
	return &p, nil
}
