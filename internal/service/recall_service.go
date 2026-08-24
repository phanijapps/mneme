package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// RecallService implements port.RecallService.
type RecallService struct {
	engine   port.RecallEngine
	requests port.RecallRequestRepository
	results  port.RecallResultRepository
	sessions port.AgentSessionRepository
	tx       port.TransactionManager
	now      clock
}

// RecallServiceOption configures optional RecallService dependencies.
type RecallServiceOption func(*RecallService)

// WithRecallSessions attaches the session repository for the
// session-exists and budget preconditions of the recall flow.
func WithRecallSessions(r port.AgentSessionRepository) RecallServiceOption {
	return func(s *RecallService) { s.sessions = r }
}

// WithRecallTx attaches a TransactionManager around result persistence.
func WithRecallTx(tx port.TransactionManager) RecallServiceOption {
	return func(s *RecallService) { s.tx = tx }
}

// WithRecallClock overrides the service clock (tests).
func WithRecallClock(c clock) RecallServiceOption {
	return func(s *RecallService) { s.now = c }
}

// NewRecallService builds a RecallService over the recall engine and its
// operational log repositories.
func NewRecallService(engine port.RecallEngine, requests port.RecallRequestRepository, results port.RecallResultRepository, opts ...RecallServiceOption) *RecallService {
	s := &RecallService{engine: engine, requests: requests, results: results, now: defaultClock}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

var _ port.RecallService = (*RecallService)(nil)

// Recall executes one request. Sync mode runs the engine inline and persists
// the result; async mode queues the request and the result is fetched via
// GetRecallStatus.
func (s *RecallService) Recall(ctx context.Context, req *domain.RecallRequest) (*domain.RecallResult, error) {
	if req == nil {
		return nil, domain.NewValidationError("RecallRequest", "<nil>", "non-nil")
	}
	if req.RequestID == uuid.Nil {
		req.RequestID = uuidMustV7()
	}
	req.RequestedAt = s.now()
	if req.Mode == "" {
		req.Mode = domain.RecallModeSync
	}
	if req.RetrievalParams.TopK == 0 {
		req.RetrievalParams.TopK = 50 // review F6 default
	}
	if req.RetrievalParams.Rerank == "" {
		req.RetrievalParams.Rerank = domain.RerankCrossEncoder
	}
	if s.sessions != nil {
		sess, err := s.sessions.GetByID(ctx, req.SessionID)
		if err != nil {
			return nil, &domain.Error{
				Code:    domain.CodeSessionNotFound,
				Message: fmt.Sprintf("session %s not found", req.SessionID),
				Details: map[string]any{"session_id": req.SessionID},
			}
		}
		p := req.RetrievalParams
		if p.SlotBudget != nil && sess.ContextWindow.InstructionSlotBudget != 0 &&
			*p.SlotBudget > sess.ContextWindow.InstructionSlotBudget {
			return nil, &domain.Error{
				Code: domain.CodeRecallBudgetInvalid,
				Message: fmt.Sprintf("slot_budget %d exceeds session budget %d",
					*p.SlotBudget, sess.ContextWindow.InstructionSlotBudget),
				Details: map[string]any{"slot_budget": *p.SlotBudget,
					"session_slot_budget": sess.ContextWindow.InstructionSlotBudget},
			}
		}
		if p.TokenBudget != nil && sess.ContextWindow.MaxTokens != 0 &&
			*p.TokenBudget > sess.ContextWindow.MaxTokens {
			return nil, &domain.Error{
				Code: domain.CodeRecallBudgetInvalid,
				Message: fmt.Sprintf("token_budget %d exceeds session budget %d",
					*p.TokenBudget, sess.ContextWindow.MaxTokens),
				Details: map[string]any{"token_budget": *p.TokenBudget,
					"session_token_budget": sess.ContextWindow.MaxTokens},
			}
		}
	}

	if req.Mode == domain.RecallModeAsync {
		req.Status = domain.RecallStatusQueued
		if err := req.Validate(); err != nil {
			return nil, err
		}
		if _, err := s.requests.Create(ctx, req); err != nil {
			return nil, fmt.Errorf("queue recall request %s: %w", req.RequestID, err)
		}
		return nil, nil // result arrives via GetRecallStatus
	}

	req.Status = domain.RecallStatusRunning
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.requests.Create(ctx, req); err != nil {
		return nil, fmt.Errorf("log recall request %s: %w", req.RequestID, err)
	}
	result, err := s.engine.Recall(ctx, req)
	if err != nil {
		_ = s.requests.UpdateStatus(ctx, req.RequestID, domain.RecallStatusFailed, domain.FromError(err))
		return nil, fmt.Errorf("recall engine: %w", err)
	}
	persist := func(ctx context.Context) error {
		if _, err := s.results.Save(ctx, result); err != nil {
			return fmt.Errorf("save recall result %s: %w", req.RequestID, err)
		}
		return s.requests.UpdateStatus(ctx, req.RequestID, domain.RecallStatusCompleted, nil)
	}
	if s.tx != nil {
		if err := s.tx.WithTx(ctx, persist); err != nil {
			return nil, err
		}
	} else if err := persist(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// GetRecallStatus polls a request; result is non-nil once completed.
func (s *RecallService) GetRecallStatus(ctx context.Context, requestID uuid.UUID) (*domain.RecallRequest, *domain.RecallResult, error) {
	req, err := s.requests.GetByID(ctx, requestID)
	if err != nil {
		return nil, nil, &domain.Error{
			Code:    domain.CodeRecallNotFound,
			Message: fmt.Sprintf("recall request %s not found", requestID),
		}
	}
	if req.Status != domain.RecallStatusCompleted {
		return req, nil, nil
	}
	results, err := s.results.ListByRequest(ctx, requestID)
	if err != nil {
		return req, nil, fmt.Errorf("list results for %s: %w", requestID, err)
	}
	if len(results) == 0 {
		return req, nil, nil
	}
	return req, results[0], nil
}
