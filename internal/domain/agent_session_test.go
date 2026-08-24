package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentSessionValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*AgentSession)
		wantErr bool
	}{
		{"valid", func(*AgentSession) {}, false},
		{"invalid agent type", func(s *AgentSession) { s.AgentType = AgentType("windsurf") }, true},
		{"missing user", func(s *AgentSession) { s.UserID = "" }, true},
		{"max_tokens zero", func(s *AgentSession) { s.ContextWindow.MaxTokens = 0 }, true},
		{"used_tokens negative", func(s *AgentSession) { s.ContextWindow.UsedTokens = -1 }, true},
		{"slot budget negative", func(s *AgentSession) { s.ContextWindow.InstructionSlotBudget = -1 }, true},
		{"slots over budget", func(s *AgentSession) {
			s.ContextWindow.InstructionSlotBudget = 40
			s.ContextWindow.SlotsUsed = 41
		}, true},
		{"slots at budget", func(s *AgentSession) {
			s.ContextWindow.InstructionSlotBudget = 40
			s.ContextWindow.SlotsUsed = 40
		}, false},
		{"budget zero means unlimited", func(s *AgentSession) {
			s.ContextWindow.InstructionSlotBudget = 0
			s.ContextWindow.SlotsUsed = 100
		}, false},
		{"ended before created", func(s *AgentSession) {
			s.EndedAt = ptrTime(s.CreatedAt.Add(-time.Minute))
		}, true},
		{"ended after created", func(s *AgentSession) {
			s.EndedAt = ptrTime(s.CreatedAt.Add(time.Hour))
		}, false},
		{"missing model", func(s *AgentSession) { s.ContextWindow.Model = "" }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &AgentSession{
				SessionID: uuid.New(),
				AgentType: AgentClaudeCode,
				UserID:    "user-123",
				ContextWindow: ContextWindow{
					Model: "claude-sonnet-4", MaxTokens: 200000,
					UsedTokens: 50000, InstructionSlotBudget: 40, SlotsUsed: 10,
				},
				CreatedAt: time.Now(),
			}
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
