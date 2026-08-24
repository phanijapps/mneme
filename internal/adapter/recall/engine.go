// Package recall implements the 4-way hybrid retrieval engine
// (architecture.md §2.2 Retrieval Engine): dense vector, BM25-like
// full-text, graph traversal, and temporal — fused with Reciprocal Rank
// Fusion and re-ranked before budgeted injection.
package recall

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// DB is the pgx execution surface the strategies need (satisfied by
// *pgxpool.Pool and pgx.Tx).
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// mapQueryErr wraps a pgx error with strategy context; pgx.ErrNoRows is not
// possible for list queries, so everything else is a real failure.
func mapQueryErr(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return fmt.Errorf("postgres %s: %s", pgErr.Code, pgErr.Message)
	}
	return err
}

// qb is a positional-parameter query builder: every arg is registered
// through arg(), which returns its $n placeholder, so SQL text and args
// can never drift apart.
type qb struct {
	sql  strings.Builder
	args []any
	n    int
}

func (b *qb) arg(v any) string {
	b.n++
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", b.n)
}

// principal is the caller identity for the Q7 visibility predicate.
type principal struct {
	typ string
	id  string
}

// scope is the shared WHERE-clause fragment applied to every strategy
// query: soft-delete, TTL, supersession, temporal validity (Q2), and
// owner/space visibility (Q7). Built once per request.
type scope struct {
	principal principal
	at        time.Time
}

// where appends the base filters every strategy shares: not deleted, not
// TTL-expired, not superseded, and (unless skipTemporal) valid at s.at —
// Q2: interval covers the instant OR no temporal semantics.
func (s scope) where(b *qb, skipTemporal bool) {
	fmt.Fprintf(&b.sql, `m.deleted_at IS NULL
	  AND (m.ttl_expires_at IS NULL OR m.ttl_expires_at > %s::timestamptz)
	  AND m.superseded_by IS NULL`, b.arg(s.at))
	if !skipTemporal {
		fmt.Fprintf(&b.sql, `
	  AND (m.validity_range @> %s::timestamptz OR (m.valid_from IS NULL AND m.valid_until IS NULL))`, b.arg(s.at))
	}
}

// visibility appends the Q7 predicate: own individual-scope memories plus
// every space the principal can read (membership or space ownership or
// world-readable non-user space).
func (s scope) visibility(b *qb) {
	fmt.Fprintf(&b.sql, ` AND (
	     (m.access_scope = 'individual' AND m.owner_principal_type = %s AND m.owner_principal_id = %s)
	     OR m.shared_space_id IN (
	       SELECT sm.space_id FROM space_memberships sm
	        WHERE sm.principal_type = %s AND sm.principal_id = %s
	          AND sm.access_level IN ('read','write','promote','admin')
	       UNION
	       SELECT sp.id FROM shared_memory_spaces sp
	        WHERE (sp.owner_type = %s AND sp.owner_id = %s)
	           OR (sp.default_access <> 'none' AND sp.owner_type <> %s)
	     )
	   )`,
		b.arg(s.principal.typ), b.arg(s.principal.id),
		b.arg(s.principal.typ), b.arg(s.principal.id),
		b.arg(s.principal.typ), b.arg(s.principal.id), b.arg(s.principal.typ))
}

// StrategyResult is one strategy's ranked list plus its provenance.
type StrategyResult struct {
	Strategy domain.StrategyType
	Items    []RankedItem
}

// RankedItem is one (memory_id, native score) pair from a strategy.
type RankedItem struct {
	MemoryID uuid.UUID
	Score    float64
}

func scanRankedItem(row pgx.CollectableRow) (RankedItem, error) {
	var it RankedItem
	err := row.Scan(&it.MemoryID, &it.Score)
	return it, err
}

// QueryEmbedder turns a recall query string into a dense vector. Production
// wiring adapts port.MemoryEncoder; tests use deterministic stubs.
type QueryEmbedder interface {
	EmbedQuery(ctx context.Context, query string) (embedding []float32, err error)
}

// Options configures an Engine. Zero values select the documented defaults.
type Options struct {
	// PrincipalType/PrincipalID identify the caller for the Q7 visibility
	// predicate. When PrincipalID is empty the engine falls back to
	// ("agent", req.AgentID) per request.
	PrincipalType domain.PrincipalType
	PrincipalID   string
	// ModelID optionally pins the dense path to one embedding model; when
	// empty the primary memories.embedding column is used.
	ModelID string
	// Embedder is required whenever the vector strategy is requested.
	Embedder QueryEmbedder
	// Reranker is used when params.Rerank == cross_encoder; defaults to the
	// identity stub (v1 has no external reranking service).
	Reranker Reranker
	// GraphHops bounds the recursive walk (Q3 uses 2).
	GraphHops int
	// GraphSeeds is how many top phase-1 hits seed the graph walk.
	GraphSeeds int
	// VectorMinSimilarity floors the dense leg: cosine hits below it are
	// noise, not matches, and must not ride into fusion on rank alone
	// (RRF is score-blind — rank 2 of 2 would otherwise normalize to ~0.98).
	VectorMinSimilarity float64
}

func (o *Options) fill() {
	if o.PrincipalType == "" {
		o.PrincipalType = domain.PrincipalAgent
	}
	if o.Reranker == nil {
		o.Reranker = IdentityReranker{}
	}
	if o.GraphHops <= 0 {
		o.GraphHops = 2
	}
	if o.GraphSeeds <= 0 {
		o.GraphSeeds = 10
	}
	if o.VectorMinSimilarity <= 0 {
		o.VectorMinSimilarity = 0.1
	}
}

// Engine implements port.RecallEngine on PostgreSQL.
type Engine struct {
	pool      *pgxpool.Pool
	opts      Options
	vector    *VectorSearch
	fulltext  *FullTextSearch
	graph     *GraphSearch
	temporal  *TemporalSearch
	injection *ContentSource
}

// NewEngine builds a recall engine over pool.
func NewEngine(pool *pgxpool.Pool, opts Options) *Engine {
	opts.fill()
	return &Engine{
		pool:      pool,
		opts:      opts,
		vector:    NewVectorSearch(pool),
		fulltext:  NewFullTextSearch(pool),
		graph:     NewGraphSearch(pool),
		temporal:  NewTemporalSearch(pool),
		injection: NewContentSource(pool),
	}
}

var _ port.RecallEngine = (*Engine)(nil)

// defaults mirror recall_requests column defaults (review F6).
func (e *Engine) defaults(req *domain.RecallRequest) {
	if len(req.RetrievalParams.Strategies) == 0 {
		req.RetrievalParams.Strategies = []domain.StrategyType{domain.StrategyVector}
	}
	if req.RetrievalParams.TopK == 0 {
		req.RetrievalParams.TopK = 50
	}
	// Hard ceiling from the finalized design (review F6); domain.Validate
	// rejects out-of-range values, the engine clamps defensively.
	if req.RetrievalParams.TopK > 200 {
		req.RetrievalParams.TopK = 200
	}
	if req.RetrievalParams.Rerank == "" {
		req.RetrievalParams.Rerank = domain.RerankCrossEncoder
	}
	if req.RetrievalParams.MinScore == 0 {
		req.RetrievalParams.MinScore = 0.35
	}
}

func hasStrategy(req *domain.RecallRequest, s domain.StrategyType) bool {
	for _, x := range req.RetrievalParams.Strategies {
		if x == s {
			return true
		}
	}
	return false
}

// Recall runs the requested strategies, fuses their ranked lists with RRF,
// re-ranks, truncates to top_k, and builds the budgeted injection plan.
func (e *Engine) Recall(ctx context.Context, req *domain.RecallRequest) (*domain.RecallResult, error) {
	start := time.Now()
	if req == nil {
		return nil, fmt.Errorf("recall: nil request")
	}
	if req.Query == "" {
		return nil, domain.NewValidationError("RecallRequest.Query", "", "non-empty")
	}
	e.defaults(req)
	p := req.RetrievalParams

	pr := principal{typ: string(e.opts.PrincipalType), id: e.opts.PrincipalID}
	if pr.id == "" {
		pr.typ = string(domain.PrincipalAgent)
		pr.id = req.AgentID
	}
	// Point-in-time defaults to now; RecallContext.TimeBounds[0] pins it.
	at := time.Now().UTC()
	if req.Context.TimeBounds != nil {
		at = req.Context.TimeBounds[0].UTC()
	}

	sets, err := e.runStrategies(ctx, req, pr, at)
	if err != nil {
		return nil, err
	}

	candidates := FuseRRF(sets, RRFK)
	candidates = FilterByMinScore(candidates, p.MinScore)

	if p.Rerank != domain.RerankNone {
		reranked, rerr := e.opts.Reranker.Rerank(ctx, req.Query, candidates)
		if rerr != nil {
			return nil, fmt.Errorf("recall: rerank: %w", rerr)
		}
		candidates = reranked
	}
	if len(candidates) > p.TopK {
		candidates = candidates[:p.TopK]
	}

	plan, slotsUsed, tokensUsed, err := e.buildPlan(ctx, candidates, p)
	if err != nil {
		return nil, err
	}

	latency := int(time.Since(start).Milliseconds())
	return &domain.RecallResult{
		ResultID:      req.RequestID, // UNIQUE(request_id): one result per request
		RequestID:     req.RequestID,
		Candidates:    candidates,
		InjectionPlan: plan,
		SlotsUsed:     slotsUsed,
		TokensUsed:    tokensUsed,
		LatencyMS:     &latency,
		CreatedAt:     time.Now().UTC(),
	}, nil
}

// runStrategies executes phase 1 (vector, fulltext, temporal) concurrently,
// then phase 2 (graph) seeded by phase-1 hits. When only the graph strategy
// is requested, fulltext runs anyway to find entry points ("vector entry,
// graph expansion" — retrieval-and-recall.md).
func (e *Engine) runStrategies(ctx context.Context, req *domain.RecallRequest, pr principal, at time.Time) ([]StrategyResult, error) {
	p := req.RetrievalParams
	sc := scope{principal: pr, at: at}

	type outcome struct {
		set StrategyResult
		err error
	}
	outcomes := make([]outcome, 3)
	var wg sync.WaitGroup
	run := func(i int, fn func() (StrategyResult, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			set, err := fn()
			outcomes[i] = outcome{set: set, err: err}
		}()
	}
	if hasStrategy(req, domain.StrategyVector) {
		if e.opts.Embedder == nil {
			return nil, fmt.Errorf("recall: vector strategy requested but no QueryEmbedder configured")
		}
		run(0, func() (StrategyResult, error) {
			return e.vector.Search(ctx, req.Query, e.opts.Embedder, e.opts.ModelID, sc, p.TopK, e.opts.VectorMinSimilarity)
		})
	}
	if hasStrategy(req, domain.StrategyBM25) {
		run(1, func() (StrategyResult, error) {
			return e.fulltext.Search(ctx, req.Query, sc, p.TopK)
		})
	}
	if hasStrategy(req, domain.StrategyTemporal) {
		run(2, func() (StrategyResult, error) {
			return e.temporal.Search(ctx, sc, p.TopK)
		})
	}
	wg.Wait()

	sets := make([]StrategyResult, 0, 4)
	for _, oc := range outcomes {
		if oc.err != nil {
			return nil, fmt.Errorf("recall: strategy failed: %w", oc.err)
		}
		if oc.set.Strategy != "" {
			sets = append(sets, oc.set)
		}
	}

	if hasStrategy(req, domain.StrategyGraph) {
		seedIDs := SeedIDs(sets, e.opts.GraphSeeds)
		if len(seedIDs) == 0 {
			ft, err := e.fulltext.Search(ctx, req.Query, sc, e.opts.GraphSeeds)
			if err != nil {
				return nil, fmt.Errorf("recall: graph entry-point search: %w", err)
			}
			seedIDs = SeedIDs([]StrategyResult{ft}, e.opts.GraphSeeds)
		}
		if len(seedIDs) > 0 {
			gs, err := e.graph.Search(ctx, seedIDs, e.opts.GraphHops, sc, p.TopK)
			if err != nil {
				return nil, fmt.Errorf("recall: graph strategy: %w", err)
			}
			sets = append(sets, gs)
		}
	}
	return sets, nil
}

// SeedIDs takes the union of memory IDs across sets, ordered by best rank
// (uuid tie-break for determinism), capped at n.
func SeedIDs(sets []StrategyResult, n int) []uuid.UUID {
	best := map[uuid.UUID]int{}
	for _, set := range sets {
		for rank, it := range set.Items {
			if prev, ok := best[it.MemoryID]; !ok || rank < prev {
				best[it.MemoryID] = rank
			}
		}
	}
	ids := make([]uuid.UUID, 0, len(best))
	for id := range best {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if best[ids[i]] != best[ids[j]] {
			return best[ids[i]] < best[ids[j]]
		}
		return ids[i].String() < ids[j].String()
	})
	if len(ids) > n {
		ids = ids[:n]
	}
	return ids
}

func (e *Engine) buildPlan(ctx context.Context, candidates []domain.RecallCandidate, p domain.RetrievalParams) ([]domain.InjectionPlanItem, int, int, error) {
	if len(candidates) == 0 {
		return []domain.InjectionPlanItem{}, 0, 0, nil
	}
	ids := make([]uuid.UUID, len(candidates))
	for i, c := range candidates {
		ids[i] = c.MemoryID
	}
	lengths, err := e.injection.ContentLengths(ctx, ids)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("recall: injection content lengths: %w", err)
	}
	slotBudget, tokenBudget := DefaultSlotBudget, DefaultTokenBudget
	if p.SlotBudget != nil {
		slotBudget = *p.SlotBudget
	}
	if p.TokenBudget != nil {
		tokenBudget = *p.TokenBudget
	}
	return BuildInjectionPlan(candidates, lengths, slotBudget, tokenBudget)
}

// vectorArg adapts a []float32 query embedding to a pgvector bind value.
func vectorArg(v []float32) pgvector.Vector { return pgvector.NewVector(v) }
