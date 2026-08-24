// Package port defines the driven and driving interfaces of the clean
// architecture (architecture.md §2.2). Ports import only the domain layer and
// the standard library (plus pgvector-go, the shared vector type also used by
// domain); adapters (pgx, chi, mcp-go) implement these interfaces and must
// never be imported from here.
package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared list primitives
// ---------------------------------------------------------------------------

// Page is the cursor-pagination envelope shared by list operations
// (api-contracts §1.1 pagination envelope).
type Page[T any] struct {
	Items      []T
	NextCursor string
	HasMore    bool
}

// SortOrder mirrors the `order` query param of GET /memories.
type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

// MemorySortField mirrors the `sort` query param of GET /memories.
// last_accessed_at is served from the memory_access_log aggregate (review F8).
type MemorySortField string

const (
	SortByCreatedAt    MemorySortField = "created_at"
	SortByUpdatedAt    MemorySortField = "updated_at"
	SortByLastAccessed MemorySortField = "last_accessed_at"
	SortByConfidence   MemorySortField = "confidence"
	SortByDecayScore   MemorySortField = "decay_score"
)

// TagsMatch selects csv tag-filter semantics: any (OR) or all (AND).
type TagsMatch string

const (
	TagsMatchAny TagsMatch = "any"
	TagsMatchAll TagsMatch = "all"
)

// ---------------------------------------------------------------------------
// MemoryRepository
// ---------------------------------------------------------------------------

// MemoryRepository persists Memory records (data-models §3.1). One row per
// version; content updates supersede rather than mutate (supersession-first).
type MemoryRepository interface {
	// Save inserts a new memory version and returns the stored row with
	// server-stamped fields (id, created_at, version, valid_from).
	Save(ctx context.Context, m *domain.Memory) (*domain.Memory, error)
	// GetByID returns the live (non-deleted) memory and appends a
	// memory_access_log row with access_type=direct_get (review F8).
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Memory, error)
	// GetByVersion returns a specific version of a memory (at_version reads).
	GetByVersion(ctx context.Context, id uuid.UUID, version int) (*domain.Memory, error)
	// List applies every filter of GET /memories with cursor pagination.
	List(ctx context.Context, filter MemoryFilter) (Page[*domain.Memory], error)
	// Update persists changes. Content changes supersede: the current
	// version's validity is closed and a new version row is inserted
	// (optimistic concurrency on filter.ExpectedVersion when non-zero).
	Update(ctx context.Context, m *domain.Memory, expectedVersion int) (*domain.Memory, error)
	// SoftDelete sets deleted_at and closes validity; nothing is destroyed.
	SoftDelete(ctx context.Context, id uuid.UUID) error
	// Purge hard-deletes the row and its dependents (admin only).
	Purge(ctx context.Context, id uuid.UUID) error
	// SaveLink inserts a directed, weighted edge between two memories.
	SaveLink(ctx context.Context, l *domain.MemoryLink) (*domain.MemoryLink, error)
	// ListLinks returns edges touching a memory, filtered by direction and
	// relationship type.
	ListLinks(ctx context.Context, memoryID uuid.UUID, opts LinkFilter) ([]*domain.MemoryLink, error)
}

// MemoryFilter carries every query param of GET /memories. Zero-value fields
// are ignored; the struct is constructed via functional options.
type MemoryFilter struct {
	Types          []domain.MemoryType
	AccessScope    *domain.AccessScope
	SharedSpaceID  *uuid.UUID
	Tags           []string
	TagsMatch      TagsMatch
	EntityID       *uuid.UUID
	Query          string // full-text; ts_rank_cd path, not true BM25
	CreatedFrom    *time.Time
	CreatedTo      *time.Time
	UpdatedFrom    *time.Time
	UpdatedTo      *time.Time
	MinConfidence  *float64
	MinDecayScore  *float64
	ValidAt        *time.Time // point-in-time validity interval filter
	IncludeExpired bool
	Sort           MemorySortField
	Order          SortOrder
	Limit          int
	Cursor         string
}

// MemoryFilterOption mutates a MemoryFilter (functional options pattern).
type MemoryFilterOption func(*MemoryFilter)

// NewMemoryFilter builds a MemoryFilter from options; unset fields default
// (sort=created_at, order=desc, limit=50) at the adapter boundary.
func NewMemoryFilter(opts ...MemoryFilterOption) MemoryFilter {
	f := MemoryFilter{Limit: 50, Sort: SortByCreatedAt, Order: SortDesc}
	for _, opt := range opts {
		opt(&f)
	}
	return f
}

// WithTypes filters by memory type (OR semantics).
func WithTypes(types ...domain.MemoryType) MemoryFilterOption {
	return func(f *MemoryFilter) { f.Types = types }
}

// WithTags filters by tag pattern with any/all match semantics.
func WithTags(match TagsMatch, tags ...string) MemoryFilterOption {
	return func(f *MemoryFilter) { f.Tags, f.TagsMatch = tags, match }
}

// WithTextQuery filters by full-text query (q param).
func WithTextQuery(q string) MemoryFilterOption {
	return func(f *MemoryFilter) { f.Query = q }
}

// WithValidAt returns only memories whose validity interval covers t.
func WithValidAt(t time.Time) MemoryFilterOption {
	return func(f *MemoryFilter) { f.ValidAt = &t }
}

// WithPagination sets limit and opaque cursor from a prior page.
func WithPagination(limit int, cursor string) MemoryFilterOption {
	return func(f *MemoryFilter) { f.Limit, f.Cursor = limit, cursor }
}

// LinkFilter narrows ListLinks results.
type LinkFilter struct {
	Direction         string // "outgoing" | "incoming" | "both" (default)
	RelationshipTypes []domain.RelationshipType
	MinWeight         *float64
	Limit             int
}

// ---------------------------------------------------------------------------
// EntityRepository
// ---------------------------------------------------------------------------

// EntityRepository persists the entity registry and its M:N ties to memories
// (data-models §3.3).
type EntityRepository interface {
	Save(ctx context.Context, e *domain.Entity) (*domain.Entity, error)
	GetByID(ctx context.Context, entityID uuid.UUID) (*domain.Entity, error)
	// GetByName resolves the canonical entity for an exact (non-alias) name.
	GetByName(ctx context.Context, name string) (*domain.Entity, error)
	// ListByMemory returns entities anchored to a memory via memory_entities.
	ListByMemory(ctx context.Context, memoryID uuid.UUID) ([]*domain.Entity, error)
	// SaveMemoryEntity records a memory↔entity tie (LinkMemory in plan §3).
	SaveMemoryEntity(ctx context.Context, me *domain.MemoryEntity) error
	// SaveFact appends a temporal fact; supersession closes validity rather
	// than deleting.
	SaveFact(ctx context.Context, fact *domain.EntityFact) (*domain.EntityFact, error)
	ListFacts(ctx context.Context, entityID uuid.UUID) ([]*domain.EntityFact, error)
}

// ---------------------------------------------------------------------------
// AgentSessionRepository
// ---------------------------------------------------------------------------

// AgentSessionRepository persists agent sessions and their working sets
// (data-models §3.4).
type AgentSessionRepository interface {
	Create(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, error)
	GetByID(ctx context.Context, sessionID uuid.UUID) (*domain.AgentSession, error)
	Update(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, error)
	// EndSession stamps ended_at and the closing summary; idempotent guard
	// returns ErrInvalidState on double end (SESSION_ALREADY_ENDED).
	EndSession(ctx context.Context, sessionID uuid.UUID, summary *string) (*domain.AgentSession, error)
	// ActivateMemory adds a memory to the session working set and appends a
	// memory_access_log row (access_type=session_activate). Enforces the
	// instruction-slot budget, returning ErrSlotBudgetExceeded.
	ActivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error)
	// DeactivateMemory removes a memory from the working set.
	DeactivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error)
}

// ---------------------------------------------------------------------------
// SharedMemorySpaceRepository
// ---------------------------------------------------------------------------

// SharedMemorySpaceRepository persists spaces and memberships (data-models
// §3.5). principal_id has no FK — principals are external.
type SharedMemorySpaceRepository interface {
	Create(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error)
	GetByID(ctx context.Context, spaceID uuid.UUID) (*domain.SharedMemorySpace, error)
	// List returns spaces a principal can read (membership-scoped).
	List(ctx context.Context, principalType domain.PrincipalType, principalID string) ([]*domain.SharedMemorySpace, error)
	Update(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error)
	ListMemberships(ctx context.Context, spaceID uuid.UUID) ([]*domain.SpaceMembership, error)
	AddMembership(ctx context.Context, m *domain.SpaceMembership) error
	// RemoveMembership revokes one principal's access from a space.
	RemoveMembership(ctx context.Context, spaceID uuid.UUID, principalType domain.PrincipalType, principalID string) error
}

// ---------------------------------------------------------------------------
// PromotionProposalRepository
// ---------------------------------------------------------------------------

// PromotionProposalRepository persists review-gated promotions into shared
// spaces (data-models §3.8). At most one open proposal per
// (candidate, space) — review F10.
type PromotionProposalRepository interface {
	Create(ctx context.Context, p *domain.PromotionProposal) (*domain.PromotionProposal, error)
	GetByID(ctx context.Context, proposalID uuid.UUID) (*domain.PromotionProposal, error)
	// ListPending returns non-terminal proposals of a space (in_review).
	ListPending(ctx context.Context, spaceID uuid.UUID) ([]*domain.PromotionProposal, error)
	// Approve transitions a proposal to merged (terminal); reviewer is
	// recorded. Returns ErrDuplicateProposal on re-resolve attempts.
	Approve(ctx context.Context, proposalID uuid.UUID, reviewer string) (*domain.PromotionProposal, error)
	// Reject transitions a proposal to rejected (terminal) with a required
	// reason (review F7).
	Reject(ctx context.Context, proposalID uuid.UUID, reviewer, reason string) (*domain.PromotionProposal, error)
}

// ---------------------------------------------------------------------------
// Recall repositories
// ---------------------------------------------------------------------------

// RecallRequestRepository persists the recall-router operational log
// (data-models §3.6).
type RecallRequestRepository interface {
	Create(ctx context.Context, r *domain.RecallRequest) (*domain.RecallRequest, error)
	GetByID(ctx context.Context, requestID uuid.UUID) (*domain.RecallRequest, error)
	// UpdateStatus advances queued→running→completed|failed; failure carries
	// the domain error (required iff status=failed).
	UpdateStatus(ctx context.Context, requestID uuid.UUID, status domain.RecallStatus, failure *domain.Error) error
}

// RecallResultRepository persists the merged, re-ranked answer per request
// (data-models §3.7). UNIQUE(request_id): one result per request.
type RecallResultRepository interface {
	Save(ctx context.Context, r *domain.RecallResult) (*domain.RecallResult, error)
	ListByRequest(ctx context.Context, requestID uuid.UUID) ([]*domain.RecallResult, error)
}

// ---------------------------------------------------------------------------
// Embedding repositories
// ---------------------------------------------------------------------------

// EmbeddingModelRepository persists the model registry (review F11). model_id
// is the registry name; dims must match a typed vector column (1536|768).
type EmbeddingModelRepository interface {
	Save(ctx context.Context, m *domain.EmbeddingModel) (*domain.EmbeddingModel, error)
	GetByID(ctx context.Context, modelID string) (*domain.EmbeddingModel, error)
	List(ctx context.Context, activeOnly bool) ([]*domain.EmbeddingModel, error)
}

// MemoryEmbeddingRepository persists alternate-model vectors. Exactly one of
// vec_1536/vec_768 is populated, matching the model's registered dims.
type MemoryEmbeddingRepository interface {
	Save(ctx context.Context, e *domain.MemoryEmbedding) error
	GetByMemoryID(ctx context.Context, memoryID uuid.UUID) ([]*domain.MemoryEmbedding, error)
	ListByModel(ctx context.Context, modelID string, limit int) ([]*domain.MemoryEmbedding, error)
}

// ---------------------------------------------------------------------------
// LifecycleJobRepository
// ---------------------------------------------------------------------------

// LifecycleJobRepository persists the async job ledger backing every JobRef
// poll target (review F3).
type LifecycleJobRepository interface {
	Create(ctx context.Context, j *domain.LifecycleJob) (*domain.LifecycleJob, error)
	GetByID(ctx context.Context, jobID uuid.UUID) (*domain.LifecycleJob, error)
	// UpdateStatus advances queued→running→completed|failed; result is the
	// job payload on completion, failure the domain error on failure.
	UpdateStatus(ctx context.Context, jobID uuid.UUID, status domain.JobStatus, result any, failure *domain.Error) error
	// ListPending returns queued/running jobs, optionally narrowed by kind.
	ListPending(ctx context.Context, kind *domain.JobKind, limit int) ([]*domain.LifecycleJob, error)
}

// ---------------------------------------------------------------------------
// MemoryAccessLogRepository
// ---------------------------------------------------------------------------

// MemoryAccessLogRepository appends to the decoupled access log (review F8):
// reads never write to the memories table; recency scoring reads this log.
type MemoryAccessLogRepository interface {
	Append(ctx context.Context, entry *domain.MemoryAccessLog) error
	ListByMemory(ctx context.Context, memoryID uuid.UUID, limit int) ([]*domain.MemoryAccessLog, error)
}
