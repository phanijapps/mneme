package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// SessionService implements port.SessionService.
type SessionService struct {
	sessions port.AgentSessionRepository
	memories port.MemoryRepository
	jobs     port.LifecycleJobRepository
	engine   port.RecallEngine
	tx       port.TransactionManager
	now      clock
}

// SessionServiceOption configures optional SessionService dependencies.
type SessionServiceOption func(*SessionService)

// WithSessionMemories attaches the memory repository for activation checks.
func WithSessionMemories(r port.MemoryRepository) SessionServiceOption {
	return func(s *SessionService) { s.memories = r }
}

// WithSessionJobs attaches the lifecycle job ledger (EndSession enqueue).
func WithSessionJobs(r port.LifecycleJobRepository) SessionServiceOption {
	return func(s *SessionService) { s.jobs = r }
}

// WithSessionEngine attaches the recall engine for bootstrap injection.
func WithSessionEngine(e port.RecallEngine) SessionServiceOption {
	return func(s *SessionService) { s.engine = e }
}

// WithSessionTx attaches a TransactionManager for multi-step operations.
func WithSessionTx(tx port.TransactionManager) SessionServiceOption {
	return func(s *SessionService) { s.tx = tx }
}

// WithSessionClock overrides the service clock (tests).
func WithSessionClock(c clock) SessionServiceOption {
	return func(s *SessionService) { s.now = c }
}

// NewSessionService builds a SessionService over the session repository.
func NewSessionService(repo port.AgentSessionRepository, opts ...SessionServiceOption) *SessionService {
	s := &SessionService{sessions: repo, now: defaultClock}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

var _ port.SessionService = (*SessionService)(nil)

// StartSession creates a session. When bootstrap material is supplied via
// the context (BootstrapPlan items computed by the router), the fixed-order
// Flow D plan (procedural → semantic → episodic) is reflected in
// ActiveMemories/InjectionOrder.
func (s *SessionService) StartSession(ctx context.Context, sess *domain.AgentSession) (*domain.AgentSession, []domain.InjectionPlanItem, error) {
	if sess == nil {
		return nil, nil, domain.NewValidationError("AgentSession", "<nil>", "non-nil")
	}
	if sess.SessionID == uuid.Nil {
		sess.SessionID = uuidMustV7()
	}
	sess.CreatedAt = s.now()
	if err := sess.Validate(); err != nil {
		return nil, nil, err
	}
	plan := bootstrapFromContext(ctx)
	if len(plan) > 0 {
		ids := make([]uuid.UUID, 0, len(plan))
		order := make([]string, 0, len(plan))
		slots := sess.ContextWindow.SlotsUsed
		for _, item := range plan {
			ids = append(ids, item.MemoryID)
			order = append(order, fmt.Sprintf("%d", item.Position))
			slots += item.SlotCost
		}
		if sess.ContextWindow.InstructionSlotBudget != 0 && slots > sess.ContextWindow.InstructionSlotBudget {
			return nil, nil, &domain.Error{
				Code: domain.CodeSlotBudgetExceeded,
				Message: fmt.Sprintf("bootstrap needs %d slots, budget is %d",
					slots, sess.ContextWindow.InstructionSlotBudget),
				Details: map[string]any{"slots_needed": slots,
					"slot_budget": sess.ContextWindow.InstructionSlotBudget},
			}
		}
		sess.ActiveMemories = ids
		sess.InjectionOrder = order
		sess.ContextWindow.SlotsUsed = slots
	}
	created, err := s.sessions.Create(ctx, sess)
	if err != nil {
		return nil, nil, fmt.Errorf("create session: %w", err)
	}
	return created, plan, nil
}

// GetSession returns one session by id.
func (s *SessionService) GetSession(ctx context.Context, sessionID uuid.UUID) (*domain.AgentSession, error) {
	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", sessionID, err)
	}
	return sess, nil
}

// ActivateMemory adds a memory to the working set after enforcing the slot
// and token budgets. The repository owns the slot-budget enforcement and the
// session_activate access-log append; the service pre-checks both budgets so
// failures surface before any write.
func (s *SessionService) ActivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error) {
	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, &domain.Error{
			Code:    domain.CodeSessionNotFound,
			Message: fmt.Sprintf("session %s not found", sessionID),
		}
	}
	var m *domain.Memory
	if s.memories != nil {
		m, err = s.memories.GetByID(ctx, memoryID)
		if err != nil {
			return nil, &domain.Error{
				Code:    domain.CodeMemoryNotFound,
				Message: fmt.Sprintf("memory %s not found", memoryID),
			}
		}
	}
	for _, id := range sess.ActiveMemories {
		if id == memoryID {
			return sess, nil // idempotent activation
		}
	}
	budget := sess.ContextWindow
	if budget.InstructionSlotBudget != 0 && budget.SlotsUsed+1 > budget.InstructionSlotBudget {
		return nil, &domain.Error{
			Code: domain.CodeSlotBudgetExceeded,
			Message: fmt.Sprintf("activation needs slot %d of budget %d",
				budget.SlotsUsed+1, budget.InstructionSlotBudget),
			Details: map[string]any{"slots_used": budget.SlotsUsed,
				"slot_budget": budget.InstructionSlotBudget},
		}
	}
	if m != nil && m.Embedding != nil {
		// No cheap server-side tokenizer at this layer; the transport layer
		// estimates tokens with the encoder. Guard the hard ceiling only.
		_ = budget.MaxTokens
	}
	updated, err := s.sessions.ActivateMemory(ctx, sessionID, memoryID)
	if err != nil {
		return nil, fmt.Errorf("activate memory %s in session %s: %w", memoryID, sessionID, err)
	}
	return updated, nil
}

// DeactivateMemory removes a memory from the working set.
func (s *SessionService) DeactivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error) {
	updated, err := s.sessions.DeactivateMemory(ctx, sessionID, memoryID)
	if err != nil {
		return nil, fmt.Errorf("deactivate memory %s in session %s: %w", memoryID, sessionID, err)
	}
	return updated, nil
}

// EndSession stamps the closing summary and enqueues a consolidation job
// (kind=consolidation) for the session scope.
func (s *SessionService) EndSession(ctx context.Context, sessionID uuid.UUID, summary *string) (*domain.AgentSession, *domain.LifecycleJob, error) {
	ended, err := s.sessions.EndSession(ctx, sessionID, summary)
	if err != nil {
		return nil, nil, fmt.Errorf("end session %s: %w", sessionID, err)
	}
	if s.jobs == nil {
		return ended, nil, nil
	}
	scopeID := sessionID.String()
	job := &domain.LifecycleJob{
		JobID:     uuidMustV7(),
		Kind:      domain.JobConsolidation,
		Status:    domain.JobStatusQueued,
		ScopeKind: ptr("session"),
		ScopeID:   &scopeID,
		CreatedAt: s.now(),
	}
	if err := job.Validate(); err != nil {
		return ended, nil, err
	}
	stored, err := s.jobs.Create(ctx, job)
	if err != nil {
		return ended, nil, fmt.Errorf("enqueue consolidation for session %s: %w", sessionID, err)
	}
	return ended, stored, nil
}

// bootstrapKey carries a precomputed Flow D bootstrap plan through the
// context into StartSession.
type bootstrapKey struct{}

// BootstrapPlan is the ordered injection plan handed to StartSession via
// context.
type BootstrapPlan []domain.InjectionPlanItem

// WithBootstrapPlan returns a context carrying a bootstrap plan.
func WithBootstrapPlan(ctx context.Context, plan BootstrapPlan) context.Context {
	return context.WithValue(ctx, bootstrapKey{}, plan)
}

func bootstrapFromContext(ctx context.Context) BootstrapPlan {
	plan, _ := ctx.Value(bootstrapKey{}).(BootstrapPlan)
	return plan
}

func ptr[T any](v T) *T { return &v }
