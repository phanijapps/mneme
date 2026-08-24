package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// StatsProvider computes MemoryStats for a scope. LifecycleService depends
// on it rather than the raw memory repository so stat aggregation stays a
// separate concern (adapters implement it against SQL aggregates).
type StatsProvider interface {
	Stats(ctx context.Context, scope string) (port.MemoryStats, error)
}

// LifecycleService implements port.LifecycleService.
type LifecycleService struct {
	jobs     port.LifecycleJobRepository
	memories port.MemoryRepository
	stats    StatsProvider
	tx       port.TransactionManager
	now      clock
}

// LifecycleServiceOption configures optional LifecycleService dependencies.
type LifecycleServiceOption func(*LifecycleService)

// WithLifecycleMemories attaches the memory repository for passes that
// need candidate scans.
func WithLifecycleMemories(r port.MemoryRepository) LifecycleServiceOption {
	return func(s *LifecycleService) { s.memories = r }
}

// WithStatsProvider attaches the scope-stat aggregator.
func WithStatsProvider(p StatsProvider) LifecycleServiceOption {
	return func(s *LifecycleService) { s.stats = p }
}

// WithLifecycleTx attaches a TransactionManager for multi-step operations.
func WithLifecycleTx(tx port.TransactionManager) LifecycleServiceOption {
	return func(s *LifecycleService) { s.tx = tx }
}

// WithLifecycleClock overrides the service clock (tests).
func WithLifecycleClock(c clock) LifecycleServiceOption {
	return func(s *LifecycleService) { s.now = c }
}

// NewLifecycleService builds a LifecycleService over the job ledger.
func NewLifecycleService(repo port.LifecycleJobRepository, opts ...LifecycleServiceOption) *LifecycleService {
	s := &LifecycleService{jobs: repo, now: defaultClock}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

var _ port.LifecycleService = (*LifecycleService)(nil)

// Consolidate enqueues a consolidation job (kind=consolidation): find
// similar memories, merge, create semantic candidates, expire TTL'd rows,
// and create promotion proposals — the worker owns the pass itself.
func (s *LifecycleService) Consolidate(ctx context.Context, scopeKind, scopeID string) (*domain.LifecycleJob, error) {
	return s.enqueue(ctx, domain.JobConsolidation, scopeKind, scopeID)
}

// Decay enqueues a decay job (kind=decay): reduce decay_score on stale
// memories and expire TTL'd records.
func (s *LifecycleService) Decay(ctx context.Context, scopeKind, scopeID string) (*domain.LifecycleJob, error) {
	return s.enqueue(ctx, domain.JobDecay, scopeKind, scopeID)
}

// GetJob polls one lifecycle job (GET /lifecycle/jobs/{job_id}).
func (s *LifecycleService) GetJob(ctx context.Context, jobID uuid.UUID) (*domain.LifecycleJob, error) {
	job, err := s.jobs.GetByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("get job %s: %w", jobID, err)
	}
	return job, nil
}

// GetStats computes memory statistics for scope (global|user:<id>|space:<uuid>).
func (s *LifecycleService) GetStats(ctx context.Context, scope string) (port.MemoryStats, error) {
	if scope == "" {
		scope = "global"
	}
	if s.stats != nil {
		st, err := s.stats.Stats(ctx, scope)
		if err != nil {
			return port.MemoryStats{}, fmt.Errorf("stats for scope %s: %w", scope, err)
		}
		st.Scope, st.GeneratedAt = scope, time.Now().UTC().Format(time.RFC3339)
		return st, nil
	}
	return port.MemoryStats{}, &domain.Error{
		Code:    domain.CodeInternal,
		Message: "no stats provider configured",
	}
}

// enqueue validates the scope shape and persists a queued job.
func (s *LifecycleService) enqueue(ctx context.Context, kind domain.JobKind, scopeKind, scopeID string) (*domain.LifecycleJob, error) {
	switch scopeKind {
	case "global", "user", "session", "space":
	default:
		return nil, domain.NewValidationError("scope_kind", scopeKind, "global|user|session|space")
	}
	if scopeKind != "global" && scopeID == "" {
		return nil, domain.NewValidationError("scope_id", "", "required when scope_kind != global")
	}
	if strings.HasPrefix(scopeID, "user:") || strings.HasPrefix(scopeID, "space:") {
		return nil, domain.NewValidationError("scope_id", scopeID, "bare id; the kind prefix lives in scope_kind")
	}
	job := &domain.LifecycleJob{
		JobID:     uuidMustV7(),
		Kind:      kind,
		Status:    domain.JobStatusQueued,
		ScopeKind: &scopeKind,
		ScopeID:   &scopeID,
		CreatedAt: s.now(),
	}
	if err := job.Validate(); err != nil {
		return nil, err
	}
	stored, err := s.jobs.Create(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("create %s job: %w", kind, err)
	}
	return stored, nil
}
