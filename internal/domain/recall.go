package domain

import (
	"time"

	"github.com/google/uuid"
)

// RecallRequest is the recall-router event consumed by the retrieval engine
// (data-models §3.6). Operational log record, not a core entity.
type RecallRequest struct {
	RequestID       uuid.UUID       `json:"request_id" validate:"required"`
	Query           string          `json:"query" validate:"required,min=1"`
	Context         RecallContext   `json:"context"`
	Trigger         TriggerType     `json:"trigger" validate:"required"`
	AgentID         string          `json:"agent_id" validate:"required"`
	SessionID       uuid.UUID       `json:"session_id" validate:"required"`
	RetrievalParams RetrievalParams `json:"retrieval_params"`
	Mode            RecallMode      `json:"mode"`            // sync | async
	Status          RecallStatus    `json:"status"`          // queued|running|completed|failed
	Error           *Error          `json:"error,omitempty"` // required iff status=failed
	RequestedAt     time.Time       `json:"requested_at"`
}

// RecallContext carries entity anchors and bounds extracted at the router.
type RecallContext struct {
	Entities       []string      `json:"entities,omitempty"`
	TaskSignature  *string       `json:"task_signature,omitempty"`
	TimeBounds     *[2]time.Time `json:"time_bounds,omitempty"`
	MentionedFiles []string      `json:"mentioned_files,omitempty"`
}

// RetrievalParams selects the parallel retrieval paths and budgets.
// top_k defaults to 50, max 200 (review F6).
type RetrievalParams struct {
	Strategies  []StrategyType `json:"strategies" validate:"required,min=1,dive"`
	TopK        int            `json:"top_k" validate:"min=1,max=200"`   // default 50
	Rerank      RerankType     `json:"rerank"`                           // default cross_encoder
	MinScore    float64        `json:"min_score" validate:"min=0,max=1"` // default 0.35
	SlotBudget  *int           `json:"slot_budget,omitempty" validate:"omitempty,min=0"`
	TokenBudget *int           `json:"token_budget,omitempty" validate:"omitempty,min=0"`
}

// Validate checks enums, top_k range, strategy subset, and budget pairing
// (recall_requests CHECK constraints, incl. F6 and RECALL_BUDGET_INVALID).
func (r *RecallRequest) Validate() error {
	if !r.Trigger.Valid() {
		return NewValidationError("RecallRequest.Trigger", r.Trigger.String(),
			"task_context|user_query|temporal|associative|session_start")
	}
	if r.Query == "" {
		return NewValidationError("RecallRequest.Query", "", "non-empty")
	}
	if r.AgentID == "" {
		return NewValidationError("RecallRequest.AgentID", r.AgentID, "non-empty")
	}
	if !r.Mode.Valid() {
		return NewValidationError("RecallRequest.Mode", r.Mode.String(), "sync|async")
	}
	if !r.Status.Valid() {
		return NewValidationError("RecallRequest.Status", r.Status.String(),
			"queued|running|completed|failed")
	}
	if r.Status == RecallStatusFailed && r.Error == nil {
		return NewValidationError("RecallRequest.Error", "<nil?>", "required when status=failed")
	}
	p := r.RetrievalParams
	if len(p.Strategies) < 1 {
		return NewValidationError("RetrievalParams.Strategies", "[]", "at least one strategy")
	}
	for _, s := range p.Strategies {
		if !s.Valid() {
			return NewValidationError("RetrievalParams.Strategies", s.String(),
				"vector|bm25|graph|temporal")
		}
	}
	if p.TopK < 1 || p.TopK > 200 {
		return NewValidationError("RetrievalParams.TopK", intString(p.TopK), "1..200")
	}
	if !p.Rerank.Valid() {
		return NewValidationError("RetrievalParams.Rerank", p.Rerank.String(), "cross_encoder|none")
	}
	if p.MinScore < 0 || p.MinScore > 1 {
		return NewValidationError("RetrievalParams.MinScore", floatString(p.MinScore), "[0,1]")
	}
	if p.SlotBudget != nil && *p.SlotBudget < 0 {
		return NewValidationError("RetrievalParams.SlotBudget", intString(*p.SlotBudget), ">= 0")
	}
	if p.TokenBudget != nil && *p.TokenBudget < 0 {
		return NewValidationError("RetrievalParams.TokenBudget", intString(*p.TokenBudget), ">= 0")
	}
	return nil
}

// RecallCandidate is one scored memory in a recall result, pre/post re-rank.
type RecallCandidate struct {
	MemoryID         uuid.UUID      `json:"memory_id" validate:"required"`
	Score            float64        `json:"score" validate:"min=0,max=1"`
	RerankScore      *float64       `json:"rerank_score,omitempty" validate:"omitempty,min=0,max=1"`
	SourceStrategies []StrategyType `json:"source_strategies" validate:"required,min=1,dive"`
}

// InjectionPlanItem is one entry of the ordered injection plan: what enters
// working memory, in what order, at what slot cost.
type InjectionPlanItem struct {
	MemoryID uuid.UUID `json:"memory_id" validate:"required"`
	Position int       `json:"position" validate:"min=0"`
	SlotCost int       `json:"slot_cost" validate:"min=0"`
}

// RecallResult is the merged, re-ranked answer for one request
// (data-models §3.7). UNIQUE(request_id) — one result per request.
type RecallResult struct {
	ResultID      uuid.UUID           `json:"result_id" validate:"required"`
	RequestID     uuid.UUID           `json:"request_id" validate:"required"`
	Candidates    []RecallCandidate   `json:"candidates" validate:"required,min=0"`
	InjectionPlan []InjectionPlanItem `json:"injection_plan" validate:"required,min=0"`
	SlotsUsed     int                 `json:"slots_used" validate:"min=0"`
	TokensUsed    int                 `json:"tokens_used" validate:"min=0"`
	LatencyMS     *int                `json:"latency_ms,omitempty" validate:"omitempty,min=0"`
	CreatedAt     time.Time           `json:"created_at"`
}
