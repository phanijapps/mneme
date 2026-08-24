package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSpace() *SharedMemorySpace {
	now := time.Now().UTC()
	return &SharedMemorySpace{
		ID:        uuid.New(),
		Name:      "team-memory",
		OwnerType: SpaceOwnerTeam,
		OwnerID:   "team-1",
		Scope:     "software-development",
		AccessPolicy: AccessPolicy{
			DefaultAccess: DefaultAccessRead,
			WritePolicy:   WriteOwnerApproved,
			PromotePolicy: PromoteHumanReview,
		},
		StorageBackend: StorageBackend{Kind: BackendFiles},
		SyncState:      SyncState{Status: SyncStatusInSync, PendingProposals: 0},
		LastSyncedAt:   now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func TestSharedMemorySpaceValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SharedMemorySpace)
		wantErr bool
	}{
		{"valid", func(*SharedMemorySpace) {}, false},
		{"missing name", func(s *SharedMemorySpace) { s.Name = "" }, true},
		{"invalid owner type", func(s *SharedMemorySpace) { s.OwnerType = SpaceOwnerType("bot") }, true},
		{"invalid default access", func(s *SharedMemorySpace) {
			s.AccessPolicy.DefaultAccess = DefaultAccess("admin")
		}, true},
		{"invalid write policy", func(s *SharedMemorySpace) {
			s.AccessPolicy.WritePolicy = WritePolicy("anyone")
		}, true},
		{"invalid backend kind", func(s *SharedMemorySpace) {
			s.StorageBackend.Kind = StorageBackendType("quantum")
		}, true},
		{"invalid sync status", func(s *SharedMemorySpace) {
			s.SyncState.Status = SyncStatus("drifted")
		}, true},
		{"negative pending proposals", func(s *SharedMemorySpace) {
			s.SyncState.PendingProposals = -1
		}, true},
		{"policy promote auto + proposal only is invalid", func(s *SharedMemorySpace) {
			s.AccessPolicy.PromotePolicy = PromoteAuto
			s.AccessPolicy.WritePolicy = WriteProposalOnly
		}, true},
		{"policy promote auto + owner approved ok", func(s *SharedMemorySpace) {
			s.AccessPolicy.PromotePolicy = PromoteAuto
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSpace()
			tt.mutate(s)
			err := s.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, CodeValidationErr, err.(*Error).Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSpaceMembershipValidation(t *testing.T) {
	valid := &SpaceMembership{
		SpaceID:       uuid.New(),
		PrincipalType: PrincipalUser,
		PrincipalID:   "user-123",
		AccessLevel:   AccessLevelWrite,
		GrantedAt:     time.Now(),
	}
	require.NoError(t, valid.Validate())

	bad := &SpaceMembership{
		SpaceID:       valid.SpaceID,
		PrincipalType: PrincipalType("robot"),
		PrincipalID:   "x",
		AccessLevel:   AccessLevel("root"),
	}
	require.Error(t, bad.Validate())
}

func TestCanTransition(t *testing.T) {
	assert.True(t, CanTransition(ProposalStatusDraft, ProposalStatusInReview))
	assert.True(t, CanTransition(ProposalStatusInReview, ProposalStatusMerged))
	assert.True(t, CanTransition(ProposalStatusDraft, ProposalStatusRejected))
	assert.False(t, CanTransition(ProposalStatusMerged, ProposalStatusDraft))
	assert.False(t, CanTransition(ProposalStatusRejected, ProposalStatusInReview))
	assert.False(t, CanTransition(ProposalStatusMerged, ProposalStatusRejected))
}

func TestPromotionProposalApproveReject(t *testing.T) {
	newProposal := func() *PromotionProposal {
		return &PromotionProposal{
			ID:                uuid.New(),
			CandidateMemoryID: uuid.New(),
			SharedSpaceID:     uuid.New(),
			TargetPath:        "specs/003.md",
			TargetKind:        ProposalTargetSpec,
			TargetRole:        ProposalRoleSemantic,
			Diff:              "+ new spec content",
			Status:            ProposalStatusInReview,
			ProposedAt:        time.Now(),
		}
	}

	t.Run("approve happy path", func(t *testing.T) {
		p := newProposal()
		require.NoError(t, p.Approve("videogamer", p.ProposedAt.Add(time.Hour)))
		assert.Equal(t, ProposalStatusMerged, p.Status)
		require.NotNil(t, p.ReviewedAt)
		assert.Equal(t, "videogamer", *p.ReviewedBy)
		require.NoError(t, p.Validate()) // pairing CHECKs satisfied
	})

	t.Run("approve twice yields already-resolved", func(t *testing.T) {
		p := newProposal()
		require.NoError(t, p.Approve("a", time.Now()))
		err := p.Approve("b", time.Now())
		require.Error(t, err)
		assert.Equal(t, CodeProposalAlreadyResolved, err.(*Error).Code)
	})

	t.Run("reject requires reason", func(t *testing.T) {
		p := newProposal()
		err := p.Reject("videogamer", "", time.Now())
		require.Error(t, err)
		assert.Equal(t, CodeValidationErr, err.(*Error).Code)
	})

	t.Run("reject happy path", func(t *testing.T) {
		p := newProposal()
		require.NoError(t, p.Reject("videogamer", "superseded by specs/003", time.Now()))
		assert.Equal(t, ProposalStatusRejected, p.Status)
		require.NotNil(t, p.RejectReason)
		require.NoError(t, p.Validate())
	})

	t.Run("reject after merge yields already-resolved", func(t *testing.T) {
		p := newProposal()
		require.NoError(t, p.Approve("a", time.Now()))
		err := p.Reject("b", "late", time.Now())
		require.Error(t, err)
		assert.Equal(t, CodeProposalAlreadyResolved, err.(*Error).Code)
	})

	t.Run("in_review without resolved_at invalid", func(t *testing.T) {
		p := newProposal()
		p.ReviewedAt = ptrTime(time.Now())
		require.Error(t, p.Validate())
	})

	t.Run("rejected without reason invalid", func(t *testing.T) {
		p := newProposal()
		p.Status = ProposalStatusRejected
		p.ReviewedAt = ptrTime(time.Now())
		require.Error(t, p.Validate())
	})

	t.Run("invalid target kind", func(t *testing.T) {
		p := newProposal()
		p.TargetKind = ProposalTargetKind("binary")
		require.Error(t, p.Validate())
	})
}
