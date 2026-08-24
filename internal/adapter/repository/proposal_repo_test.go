//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phanijapps/mneme/internal/domain"
)

func TestProposalFlow(t *testing.T) {
	ctx := context.Background()
	r := env.prop

	space, err := env.spc.Create(ctx, &domain.SharedMemorySpace{
		Name:      "proposal-space",
		OwnerType: domain.SpaceOwnerUser,
		OwnerID:   "user-1",
		Scope:     "project",
		AccessPolicy: domain.AccessPolicy{
			DefaultAccess: domain.DefaultAccessWrite,
			WritePolicy:   domain.WriteParticipantFree,
			PromotePolicy: domain.PromoteHumanReview,
		},
		StorageBackend: domain.StorageBackend{Kind: domain.BackendFiles},
		SyncState:      domain.SyncState{Status: domain.SyncStatusInSync},
		CreatedAt:      utcNow(),
		UpdatedAt:      utcNow(),
	})
	require.NoError(t, err)

	mem, err := env.mem.Save(ctx, newMemory("candidate memory for promotion"))
	require.NoError(t, err)

	// Create proposal (defaults to in_review)
	p, err := r.Create(ctx, &domain.PromotionProposal{
		CandidateMemoryID: mem.ID,
		SharedSpaceID:     space.ID,
		TargetPath:        "docs/notes/candidate.md",
		TargetKind:        domain.ProposalTargetMemoryDoc,
		TargetRole:        domain.ProposalRoleSemantic,
		Diff:              "--- a\n+++ b\n@@\n+candidate",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.ProposalStatusInReview, p.Status, "new proposals open in in_review")
	assert.False(t, p.ProposedAt.IsZero())

	// ListPending
	pending, err := r.ListPending(ctx, space.ID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, p.ID, pending[0].ID)

	// Approve
	approved, err := r.Approve(ctx, p.ID, "reviewer-alice")
	require.NoError(t, err)
	assert.Equal(t, domain.ProposalStatusMerged, approved.Status, "approve transitions to merged")
	assert.NotNil(t, approved.ReviewedAt)
	require.NotNil(t, approved.ReviewedBy)
	assert.Equal(t, "reviewer-alice", *approved.ReviewedBy)

	// ListPending no longer includes it
	pending, err = r.ListPending(ctx, space.ID)
	require.NoError(t, err)
	assert.Empty(t, pending, "approved proposal leaves the pending queue")
}
