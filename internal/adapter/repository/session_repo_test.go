//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phanijapps/mneme/internal/domain"
)

func TestSessionRepo(t *testing.T) {
	ctx := context.Background()
	r := env.sess

	// Create
	s, err := r.Create(ctx, &domain.AgentSession{
		AgentType: domain.AgentClaudeCode,
		UserID:    "user-1",
		ContextWindow: domain.ContextWindow{
			Model:                 "claude-sonnet-4",
			MaxTokens:             200000,
			InstructionSlotBudget: 4,
		},
	})
	require.NoError(t, err)
	assert.False(t, s.SessionID.String() == "00000000-0000-0000-0000-000000000000")
	assert.Nil(t, s.EndedAt)

	// GetByID
	got, err := r.GetByID(ctx, s.SessionID)
	require.NoError(t, err)
	assert.Equal(t, s.SessionID, got.SessionID)
	assert.Equal(t, domain.AgentClaudeCode, got.AgentType)

	// ActivateMemory
	mem, err := env.mem.Save(ctx, newMemory("fact the session needs in context"))
	require.NoError(t, err)
	active, err := r.ActivateMemory(ctx, s.SessionID, mem.ID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{mem.ID}, active.ActiveMemories)
	assert.Equal(t, 1, active.ContextWindow.SlotsUsed, "activation consumes one slot")

	// EndSession with summary
	summary := "wrapped up repository integration tests"
	ended, err := r.EndSession(ctx, s.SessionID, &summary)
	require.NoError(t, err)
	assert.NotNil(t, ended.EndedAt)
	require.NotNil(t, ended.Summary)
	assert.Equal(t, summary, *ended.Summary)

	// ending twice is rejected
	_, err = r.EndSession(ctx, s.SessionID, nil)
	require.Error(t, err)
	var de *domain.Error
	if assert.ErrorAs(t, err, &de) {
		assert.Equal(t, domain.CodeSessionAlreadyEnded, de.Code)
	}
}
