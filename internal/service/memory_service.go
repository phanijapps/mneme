// Package service implements the application layer: use-case orchestration
// over the port interfaces (architecture.md §2.2). Services own business
// rules (budgets, duplicate prevention, supersession, provenance stamping)
// and know nothing about HTTP or MCP.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// Principal is the authenticated caller (F21). Transport layers derive it
// from auth material and inject it into the context; services stamp it onto
// records, overwriting any client-supplied provenance.
type Principal struct {
	AgentID string
	Actor   string
}

type principalKey struct{}

// WithPrincipal returns a context carrying the authenticated principal.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFromContext extracts the principal; ok is false when the caller
// is unauthenticated.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// clock is injectable time for deterministic tests.
type clock func() time.Time

func defaultClock() time.Time { return time.Now().UTC() }

// MemoryService implements port.MemoryService.
type MemoryService struct {
	memories  port.MemoryRepository
	encoder   port.MemoryEncoder
	accessLog port.MemoryAccessLogRepository
	tx        port.TransactionManager
	now       clock
}

// MemoryServiceOption configures optional MemoryService dependencies.
type MemoryServiceOption func(*MemoryService)

// WithEncoder attaches a MemoryEncoder for the write path (embedding,
// type inference, entity extraction).
func WithEncoder(e port.MemoryEncoder) MemoryServiceOption {
	return func(s *MemoryService) { s.encoder = e }
}

// WithAccessLog attaches the decoupled access log (review F8).
func WithAccessLog(l port.MemoryAccessLogRepository) MemoryServiceOption {
	return func(s *MemoryService) { s.accessLog = l }
}

// WithTx attaches a TransactionManager for multi-step operations.
func WithTx(tx port.TransactionManager) MemoryServiceOption {
	return func(s *MemoryService) { s.tx = tx }
}

// WithClock overrides the service clock (tests).
func WithClock(c clock) MemoryServiceOption {
	return func(s *MemoryService) { s.now = c }
}

// NewMemoryService builds a MemoryService over the memory repository.
func NewMemoryService(repo port.MemoryRepository, opts ...MemoryServiceOption) *MemoryService {
	s := &MemoryService{memories: repo, now: defaultClock}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

var _ port.MemoryService = (*MemoryService)(nil)

// SaveMemory encodes and stores a new memory, server-stamping identity,
// provenance, versioning, and decay defaults.
func (s *MemoryService) SaveMemory(ctx context.Context, m *domain.Memory) (*domain.Memory, error) {
	if m == nil {
		return nil, domain.NewValidationError("Memory", "<nil>", "non-nil")
	}
	now := s.now()
	stampMemoryProvenance(m, ctx, now)
	if m.Embedding == nil && s.encoder != nil {
		enc, err := s.encoder.Encode(ctx, m.Content, port.EncodeOptions{TypeHint: m.Type})
		if err != nil {
			return nil, fmt.Errorf("encode memory %s: %w", m.ID, err)
		}
		vec := enc.Vector
		m.Embedding = &vec
		if m.Type == "" {
			m.Type = enc.InferredType
		}
	}
	if m.ID == uuid.Nil {
		m.ID = uuidMustV7()
	}
	if m.Version == 0 {
		m.Version = 1
	}
	if m.DecayScore == nil {
		one := 1.0
		m.DecayScore = &one
	}
	if m.ValidFrom == nil {
		t := now
		m.ValidFrom = &t
	}
	m.CreatedAt, m.UpdatedAt = now, now
	if err := m.Validate(); err != nil {
		return nil, err
	}
	save := func(ctx context.Context) error {
		stored, err := s.memories.Save(ctx, m)
		if err != nil {
			return err
		}
		*m = *stored
		return nil
	}
	if s.tx != nil {
		if err := s.tx.WithTx(ctx, save); err != nil {
			return nil, err
		}
	} else if err := save(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// GetMemory returns one live memory; the access log entry is the decoupled
// read footprint (review F8).
func (s *MemoryService) GetMemory(ctx context.Context, id uuid.UUID) (*domain.Memory, error) {
	m, err := s.memories.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get memory %s: %w", id, err)
	}
	if s.accessLog != nil {
		if err := s.accessLog.Append(ctx, &domain.MemoryAccessLog{
			MemoryID: m.ID, AccessedBy: m.ID, // accessed_by refined by transport principal
			AccessType: domain.AccessTypeDirectGet, AccessedAt: s.now(),
		}); err != nil {
			return nil, fmt.Errorf("log access for %s: %w", id, err)
		}
	}
	return m, nil
}

// ListMemories delegates filtering and cursor pagination to the repository.
func (s *MemoryService) ListMemories(ctx context.Context, filter port.MemoryFilter) (port.Page[*domain.Memory], error) {
	page, err := s.memories.List(ctx, filter)
	if err != nil {
		return port.Page[*domain.Memory]{}, fmt.Errorf("list memories: %w", err)
	}
	return page, nil
}

// UpdateMemory applies an update. Content changes supersede: the repository
// closes the current version's validity and inserts the next version row,
// guarded by optimistic concurrency on the caller's m.Version.
func (s *MemoryService) UpdateMemory(ctx context.Context, m *domain.Memory) (*domain.Memory, error) {
	if m == nil || m.ID == uuid.Nil {
		return nil, domain.NewValidationError("Memory.ID", "<nil>", "non-nil id")
	}
	existing, err := s.memories.GetByID(ctx, m.ID)
	if err != nil {
		return nil, fmt.Errorf("get memory %s for update: %w", m.ID, err)
	}
	now := s.now()
	stampMemoryProvenance(m, ctx, now)
	m.CreatedAt = existing.CreatedAt
	m.UpdatedAt = now
	if m.Version == 0 {
		m.Version = existing.Version
	}
	if m.Content != existing.Content { // supersession: next version row
		m.Version = existing.Version + 1
		m.SupersededBy = nil
		m.ValidUntil = nil
		if m.ValidFrom == nil {
			t := now
			m.ValidFrom = &t
		}
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	updated, err := s.memories.Update(ctx, m, existing.Version)
	if err != nil {
		return nil, fmt.Errorf("update memory %s: %w", m.ID, err)
	}
	return updated, nil
}

// DeleteMemory soft-deletes: validity is closed, nothing is destroyed.
func (s *MemoryService) DeleteMemory(ctx context.Context, id uuid.UUID) error {
	if err := s.memories.SoftDelete(ctx, id); err != nil {
		return fmt.Errorf("soft delete memory %s: %w", id, err)
	}
	return nil
}

// LinkMemories creates a directed, weighted edge after integrity checks.
func (s *MemoryService) LinkMemories(ctx context.Context, l *domain.MemoryLink) (*domain.MemoryLink, error) {
	if l == nil {
		return nil, domain.NewValidationError("MemoryLink", "<nil>", "non-nil")
	}
	if _, err := s.memories.GetByID(ctx, l.SourceID); err != nil {
		return nil, &domain.Error{
			Code: domain.CodeLinkIntegrity, Message: "source memory does not exist",
			Details: map[string]any{"source_id": l.SourceID},
		}
	}
	if _, err := s.memories.GetByID(ctx, l.TargetID); err != nil {
		return nil, &domain.Error{
			Code: domain.CodeLinkIntegrity, Message: "target memory does not exist",
			Details: map[string]any{"target_id": l.TargetID},
		}
	}
	if l.ID == uuid.Nil {
		l.ID = uuidMustV7()
	}
	l.CreatedAt = s.now()
	if err := l.Validate(); err != nil {
		return nil, err
	}
	stored, err := s.memories.SaveLink(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("save link %s: %w", l.ID, err)
	}
	return stored, nil
}

// stampMemoryProvenance overwrites client-supplied provenance with the
// authenticated principal (F21) and refreshes timestamps.
func stampMemoryProvenance(m *domain.Memory, ctx context.Context, now time.Time) {
	if p, ok := PrincipalFromContext(ctx); ok {
		m.AgentID = nil
		m.Actor = nil
		if p.AgentID != "" {
			m.AgentID = &p.AgentID
		}
		if p.Actor != "" {
			m.Actor = &p.Actor
		}
	}
}

func uuidMustV7() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(errors.New("uuid v7 generation failed: " + err.Error()))
	}
	return id
}
