//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phanijapps/mneme/internal/domain"
)

func TestSpaceRepoCRUD(t *testing.T) {
	ctx := context.Background()
	r := env.spc

	// Create
	now := utcNow()
	desc := "team knowledge base"
	in := &domain.SharedMemorySpace{
		Name:        "eng-shared",
		Description: &desc,
		OwnerType:   domain.SpaceOwnerTeam,
		OwnerID:     "team-eng",
		Scope:       "project",
		AccessPolicy: domain.AccessPolicy{
			DefaultAccess: domain.DefaultAccessRead,
			WritePolicy:   domain.WriteOwnerApproved,
			PromotePolicy: domain.PromoteHumanReview,
		},
		StorageBackend: domain.StorageBackend{Kind: domain.BackendHybrid},
		SyncState:      domain.SyncState{Status: domain.SyncStatusInSync},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	saved, err := r.Create(ctx, in)
	require.NoError(t, err)
	assert.False(t, saved.ID.String() == "00000000-0000-0000-0000-000000000000")
	assert.Equal(t, "eng-shared", saved.Name)
	assert.Equal(t, domain.BackendHybrid, saved.StorageBackend.Kind)

	// GetByID
	got, err := r.GetByID(ctx, saved.ID)
	require.NoError(t, err)
	assert.Equal(t, saved.ID, got.ID)
	assert.Equal(t, domain.DefaultAccessRead, got.AccessPolicy.DefaultAccess)

	// List by owner principal
	list, err := r.List(ctx, domain.PrincipalUser, "no-such-user")
	require.NoError(t, err)
	assert.Empty(t, list, "unrelated principal sees no spaces")

	// AddMembership
	mem := &domain.SpaceMembership{
		SpaceID:       saved.ID,
		PrincipalType: domain.PrincipalAgent,
		PrincipalID:   "agent-7",
		AccessLevel:   domain.AccessLevelWrite,
		GrantedAt:     utcNow(),
	}
	require.NoError(t, r.AddMembership(ctx, mem))

	// ListMemberships
	members, err := r.ListMemberships(ctx, saved.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "agent-7", members[0].PrincipalID)
	assert.Equal(t, domain.AccessLevelWrite, members[0].AccessLevel)
}
