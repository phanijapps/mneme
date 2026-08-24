package domain

import (
	"time"

	"github.com/google/uuid"
)

// AgentSession is the runtime binding of an agent instance to a context
// window with dual budgets (data-models §3.4). The working tier is the context
// window itself; this record tracks its shape and budget.
type AgentSession struct {
	SessionID      uuid.UUID     `json:"session_id" validate:"required"`
	AgentType      AgentType     `json:"agent_type" validate:"required"`
	UserID         string        `json:"user_id" validate:"required"`
	SharedSpaceID  *uuid.UUID    `json:"shared_space_id,omitempty"`
	ContextWindow  ContextWindow `json:"context_window"`
	ActiveMemories []uuid.UUID   `json:"active_memories,omitempty"` // working-set ids in context
	InjectionOrder []string      `json:"injection_order,omitempty"` // recorded bootstrap order
	CreatedAt      time.Time     `json:"created_at" validate:"required"`
	EndedAt        *time.Time    `json:"ended_at,omitempty"`
	Summary        *string       `json:"summary,omitempty"`
}

// ContextWindow describes the model and the dual budget of a session:
// tokens for content, instruction slots for structured memory blocks.
type ContextWindow struct {
	Model                 string `json:"model" validate:"required"`
	MaxTokens             int    `json:"max_tokens" validate:"min=1"`
	UsedTokens            int    `json:"used_tokens" validate:"min=0"`
	InstructionSlotBudget int    `json:"instruction_slot_budget" validate:"min=0"` // 0 = unlimited
	SlotsUsed             int    `json:"slots_used" validate:"min=0"`
}

// Validate checks enum validity, timing, and budget constraints.
func (s *AgentSession) Validate() error {
	if !s.AgentType.Valid() {
		return NewValidationError("AgentSession.AgentType", s.AgentType.String(),
			"claude-code|codex|cursor|letta|custom")
	}
	if s.UserID == "" {
		return NewValidationError("AgentSession.UserID", s.UserID, "non-empty")
	}
	cw := s.ContextWindow
	if cw.Model == "" {
		return NewValidationError("ContextWindow.Model", cw.Model, "non-empty")
	}
	if cw.MaxTokens < 1 {
		return NewValidationError("ContextWindow.MaxTokens", intString(cw.MaxTokens), ">= 1")
	}
	if cw.UsedTokens < 0 {
		return NewValidationError("ContextWindow.UsedTokens", intString(cw.UsedTokens), ">= 0")
	}
	if cw.InstructionSlotBudget < 0 {
		return NewValidationError("ContextWindow.InstructionSlotBudget",
			intString(cw.InstructionSlotBudget), ">= 0")
	}
	if cw.SlotsUsed < 0 {
		return NewValidationError("ContextWindow.SlotsUsed", intString(cw.SlotsUsed), ">= 0")
	}
	// slots_used <= instruction_slot_budget OR instruction_slot_budget = 0 (unlimited)
	if cw.InstructionSlotBudget != 0 && cw.SlotsUsed > cw.InstructionSlotBudget {
		return NewValidationError("ContextWindow.SlotsUsed", intString(cw.SlotsUsed),
			"<= instruction_slot_budget (or budget = 0)")
	}
	if s.EndedAt != nil && !s.EndedAt.After(s.CreatedAt) {
		return NewValidationError("AgentSession.EndedAt", s.EndedAt.String(), "> created_at")
	}
	return nil
}
