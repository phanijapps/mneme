package domain

import (
	"time"

	"github.com/google/uuid"
)

// PromotionProposal is the human-review path for promoting an individual
// memory into a shared space (data-models §3.8). At most one open proposal
// per (candidate, space) — review F10.
type PromotionProposal struct {
	ID                uuid.UUID          `json:"proposal_id" validate:"required"`
	CandidateMemoryID uuid.UUID          `json:"candidate_memory_id" validate:"required"`
	SharedSpaceID     uuid.UUID          `json:"shared_space_id" validate:"required"`
	TargetPath        string             `json:"target_path" validate:"required"` // where in the file tree it lands
	TargetKind        ProposalTargetKind `json:"target_kind" validate:"required"`
	TargetRole        ProposalTargetRole `json:"target_role" validate:"required"`
	Diff              string             `json:"diff" validate:"required,min=1"` // unified diff for review
	Status            ProposalStatus     `json:"status" validate:"required"`
	Note              *string            `json:"note,omitempty"`          // proposer rationale (F7)
	RejectReason      *string            `json:"reject_reason,omitempty"` // required on reject (F7)
	ProposedAt        time.Time          `json:"proposed_at" validate:"required"`
	ReviewedBy        *string            `json:"reviewer,omitempty"`
	ReviewedAt        *time.Time         `json:"resolved_at,omitempty"` // resolution timestamp
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

// Validate checks enums, resolution pairing, and reject-reason pairing,
// mirroring the promotion_proposals CHECK constraints.
func (p *PromotionProposal) Validate() error {
	if !p.Status.Valid() {
		return NewValidationError("PromotionProposal.Status", p.Status.String(),
			"draft|in_review|merged|rejected")
	}
	if !p.TargetKind.Valid() {
		return NewValidationError("PromotionProposal.TargetKind", p.TargetKind.String(),
			"spec|rule|agent_doc|memory_doc")
	}
	if !p.TargetRole.Valid() {
		return NewValidationError("PromotionProposal.TargetRole", p.TargetRole.String(),
			"procedural|semantic|episodic")
	}
	if p.TargetPath == "" {
		return NewValidationError("PromotionProposal.TargetPath", p.TargetPath, "non-empty")
	}
	if p.Diff == "" {
		return NewValidationError("PromotionProposal.Diff", "", "non-empty")
	}
	// (status IN (merged, rejected)) = (resolved_at IS NOT NULL)
	if p.Status.Terminal() != (p.ReviewedAt != nil) {
		return NewValidationError("PromotionProposal.ReviewedAt", "<nil?>",
			"resolved_at set iff status is merged|rejected")
	}
	// (status = rejected) = (reject_reason IS NOT NULL)
	if (p.Status == ProposalStatusRejected) != (p.RejectReason != nil) {
		return NewValidationError("PromotionProposal.RejectReason", "<nil?>",
			"reject_reason set iff status is rejected")
	}
	return nil
}

// CanTransition reports whether moving from one status to another is legal.
// Open proposals (draft/in_review) may move to any other open or terminal
// status; terminal statuses are final.
func CanTransition(from, to ProposalStatus) bool {
	if from.Terminal() {
		return false // PROPOSAL_ALREADY_RESOLVED
	}
	return to.Valid() && to != from || (to == from && !to.Terminal())
}

// Approve marks the proposal merged, recording reviewer and resolution time.
// Returns ErrInvalidState (PROPOSAL_ALREADY_RESOLVED) on a terminal proposal.
func (p *PromotionProposal) Approve(reviewer string, at time.Time) error {
	if p.Status.Terminal() {
		return &Error{Code: CodeProposalAlreadyResolved,
			Message: "proposal " + p.ID.String() + " is already resolved",
			Details: map[string]any{"status": p.Status}}
	}
	p.Status = ProposalStatusMerged
	p.ReviewedBy = &reviewer
	p.ReviewedAt = &at
	p.UpdatedAt = at
	return nil
}

// Reject marks the proposal rejected; reason is a mandatory audit trail
// (review F7). Returns ErrInvalidState on a terminal proposal.
func (p *PromotionProposal) Reject(reviewer, reason string, at time.Time) error {
	if p.Status.Terminal() {
		return &Error{Code: CodeProposalAlreadyResolved,
			Message: "proposal " + p.ID.String() + " is already resolved",
			Details: map[string]any{"status": p.Status}}
	}
	if reason == "" {
		return NewValidationError("PromotionProposal.RejectReason", "", "required on reject")
	}
	p.Status = ProposalStatusRejected
	p.ReviewedBy = &reviewer
	p.ReviewedAt = &at
	p.RejectReason = &reason
	p.UpdatedAt = at
	return nil
}
