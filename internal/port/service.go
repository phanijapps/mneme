package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/phanijapps/mneme/internal/domain"
)

// ---------------------------------------------------------------------------
// MemoryService — write/read path of POST/GET/PUT/DELETE /memories
// ---------------------------------------------------------------------------

type MemoryService interface {
	// SaveMemory encodes (type inference, entity extraction, embedding) and
	// stores a new memory, returning the server-stamped record.
	SaveMemory(ctx context.Context, m *domain.Memory) (*domain.Memory, error)
	GetMemory(ctx context.Context, id uuid.UUID) (*domain.Memory, error)
	ListMemories(ctx context.Context, filter MemoryFilter) (Page[*domain.Memory], error)
	// UpdateMemory applies attribute updates; content changes supersede via a
	// new version row, guarded by optimistic concurrency on m.Version.
	UpdateMemory(ctx context.Context, m *domain.Memory) (*domain.Memory, error)
	// DeleteMemory soft-deletes (closes validity); purge is a separate admin
	// concern.
	DeleteMemory(ctx context.Context, id uuid.UUID) error
	// LinkMemories creates a directed, weighted edge between two memories.
	LinkMemories(ctx context.Context, l *domain.MemoryLink) (*domain.MemoryLink, error)
}

// ---------------------------------------------------------------------------
// RecallService — POST /recall and GET /recall/{request_id}
// ---------------------------------------------------------------------------

type RecallService interface {
	// Recall executes a request: sync mode returns the result inline; async
	// mode queues it and the result is fetched via GetRecallStatus.
	Recall(ctx context.Context, req *domain.RecallRequest) (*domain.RecallResult, error)
	// GetRecallStatus polls a request; the result is non-nil once status is
	// completed.
	GetRecallStatus(ctx context.Context, requestID uuid.UUID) (*domain.RecallRequest, *domain.RecallResult, error)
}

// ---------------------------------------------------------------------------
// SessionService — POST/GET /sessions and the working-set mutations
// ---------------------------------------------------------------------------

type SessionService interface {
	// StartSession creates a session and returns it with its bootstrap
	// injection plan (already reflected in ActiveMemories/InjectionOrder).
	StartSession(ctx context.Context, s *domain.AgentSession) (*domain.AgentSession, []domain.InjectionPlanItem, error)
	GetSession(ctx context.Context, sessionID uuid.UUID) (*domain.AgentSession, error)
	ActivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error)
	DeactivateMemory(ctx context.Context, sessionID, memoryID uuid.UUID) (*domain.AgentSession, error)
	// EndSession stamps the summary and triggers the consolidation job.
	EndSession(ctx context.Context, sessionID uuid.UUID, summary *string) (*domain.AgentSession, *domain.LifecycleJob, error)
}

// ---------------------------------------------------------------------------
// SpaceService — shared spaces, review-gated promotion, sync
// ---------------------------------------------------------------------------

type SpaceService interface {
	CreateSpace(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error)
	// ListSpaces returns spaces readable by a principal.
	ListSpaces(ctx context.Context, principalType domain.PrincipalType, principalID string) ([]*domain.SharedMemorySpace, error)
	GetSpace(ctx context.Context, spaceID uuid.UUID) (*domain.SharedMemorySpace, error)
	UpdateSpace(ctx context.Context, s *domain.SharedMemorySpace) (*domain.SharedMemorySpace, error)
	// PromoteMemory proposes a promotion into a space; returns the persisted
	// proposal carrying its review diff.
	PromoteMemory(ctx context.Context, p *domain.PromotionProposal) (*domain.PromotionProposal, error)
	// ReviewProposals lists proposals of a space, optionally narrowed by status.
	ReviewProposals(ctx context.Context, spaceID uuid.UUID, status *domain.ProposalStatus) ([]*domain.PromotionProposal, error)
	ApproveProposal(ctx context.Context, spaceID, proposalID uuid.UUID, reviewer string) (*domain.PromotionProposal, error)
	RejectProposal(ctx context.Context, spaceID, proposalID uuid.UUID, reviewer, reason string) (*domain.PromotionProposal, error)
	// SyncSpace triggers backend synchronization, returning the job ledger
	// entry for polling.
	SyncSpace(ctx context.Context, spaceID uuid.UUID) (*domain.LifecycleJob, error)
}

// ---------------------------------------------------------------------------
// LifecycleService — consolidate, decay, stats, job polling
// ---------------------------------------------------------------------------

type LifecycleService interface {
	// Consolidate triggers a consolidation pass over a scope (scopeKind:
	// global|user|session; scopeID empty for global), returning the job.
	Consolidate(ctx context.Context, scopeKind, scopeID string) (*domain.LifecycleJob, error)
	// Decay triggers a decay pass (score recomputation) over a scope.
	Decay(ctx context.Context, scopeKind, scopeID string) (*domain.LifecycleJob, error)
	// GetJob polls one lifecycle job (GET /lifecycle/jobs/{job_id}).
	GetJob(ctx context.Context, jobID uuid.UUID) (*domain.LifecycleJob, error)
	// GetStats computes memory statistics for scope (global|user:<id>|space:<uuid>).
	GetStats(ctx context.Context, scope string) (MemoryStats, error)
}

// MemoryStats is the GET /lifecycle/stats response body (api-contracts §1.2.5).
type MemoryStats struct {
	Scope       string       `json:"scope"`
	GeneratedAt string       `json:"generated_at"`
	Counts      StatCounts   `json:"counts"`
	Quality     StatQuality  `json:"quality"`
	Links       StatLinks    `json:"links"`
	Spaces      StatSpaces   `json:"spaces"`
	IndexHealth *IndexHealth `json:"index_health,omitempty"`
}

type StatCounts struct {
	Total         int            `json:"total"`
	ByType        map[string]int `json:"by_type"`
	ByAccessScope map[string]int `json:"by_access_scope"`
	Expired       int            `json:"expired"`
	Superseded    int            `json:"superseded"`
}

type StatQuality struct {
	AvgConfidence    float64 `json:"avg_confidence"`
	AvgDecayScore    float64 `json:"avg_decay_score"`
	LowDecayBelowH03 int     `json:"low_decay_below_03"`
}

type StatLinks struct {
	Total              int            `json:"total"`
	ByRelationshipType map[string]int `json:"by_relationship_type"`
}

type StatSpaces struct {
	Total            int `json:"total"`
	PendingProposals int `json:"pending_proposals"`
}

// IndexHealth reports per-strategy retrieval freshness.
type IndexHealth struct {
	Vector   IndexState `json:"vector"`
	BM25     IndexState `json:"bm25"`
	Graph    IndexState `json:"graph"`
	Temporal IndexState `json:"temporal"`
}

type IndexState struct {
	Status     string `json:"status"` // fresh|stale|rebuilding
	LagSeconds int    `json:"lag_seconds"`
}
