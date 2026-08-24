//go:build integration

package recall

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/network"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/phanijapps/mneme/internal/adapter/db"
	"github.com/phanijapps/mneme/internal/domain"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "mneme",
			"POSTGRES_PASSWORD": "mneme",
			"POSTGRES_DB":       "mneme_test",
		},
		WaitingFor: wait.ForSQL("5432/tcp", "pgx", func(host string, port network.Port) string {
			return fmt.Sprintf("postgres://mneme:mneme@%s:%s/mneme_test?sslmode=disable", host, port.Port())
		}),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Println("start container:", err)
		os.Exit(1)
	}
	defer func() { _ = container.Terminate(ctx) }()

	host, err := container.Host(ctx)
	if err != nil {
		fmt.Println("container host:", err)
		os.Exit(1)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		fmt.Println("mapped port:", err)
		os.Exit(1)
	}
	dbURL := fmt.Sprintf("postgres://mneme:mneme@%s:%s/mneme_test?sslmode=disable", host, port.Port())

	if err := db.RunMigrations(ctx, dbURL); err != nil {
		fmt.Println("migrations:", err)
		os.Exit(1)
	}
	p, err := db.NewPool(ctx, db.PoolConfig{URL: dbURL})
	if err != nil {
		fmt.Println("pool:", err)
		os.Exit(1)
	}
	defer p.Close()
	pool = p
	os.Exit(m.Run())
}

// pgvec adapts []float32 to the nullable domain vector type.
func pgvec(v []float32) *pgvector.Vector { p := pgvector.NewVector(v); return &p }

func utcNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// seedMemory inserts a memory directly and returns its id.
func seedMemory(t *testing.T, content string, mut func(*domain.Memory)) uuid.UUID {
	t.Helper()
	now := utcNow()
	m := &domain.Memory{
		Type:               domain.MemoryTypeSemantic,
		Content:            content,
		ContentFormat:      domain.ContentFormatPlain,
		Origin:             domain.OriginAgentObservation,
		OwnerPrincipalType: domain.PrincipalUser,
		OwnerPrincipalID:   "recall-user",
		CreatedAt:          now,
		UpdatedAt:          now,
		Version:            1,
		AccessScope:        domain.AccessScopeIndividual,
	}
	if mut != nil {
		mut(m)
	}
	if m.ID == uuid.Nil {
		m.ID = uuid.Must(uuid.NewV7())
	}
	_, err := pool.Exec(context.Background(),
		`INSERT INTO memories (id, type, content, content_format, origin,
		   owner_principal_type, owner_principal_id, created_at, updated_at,
		   version, access_scope, embedding, confidence, valid_from, valid_until, decay_score)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		m.ID, m.Type, m.Content, m.ContentFormat, m.Origin,
		m.OwnerPrincipalType, m.OwnerPrincipalID, m.CreatedAt, m.UpdatedAt,
		m.Version, m.AccessScope, m.Embedding, m.Confidence, m.ValidFrom, m.ValidUntil, m.DecayScore)
	require.NoError(t, err)
	return m.ID
}

func seedLink(t *testing.T, source, target uuid.UUID, rel string, weight float64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO memory_links (id, source_id, target_id, relationship_type, weight)
		 VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		uuid.Must(uuid.NewV7()), source, target, rel, weight)
	require.NoError(t, err)
}

// vec returns a deterministic 1536-dim unit-ish vector: axis `axis` set to
// 1 and everything else 0. Cosine distance between different axes is 1.0;
// same axis is 0.0 — exact, HNSW-independent expectations.
func vec(axis int) []float32 {
	v := make([]float32, 1536)
	v[axis%1536] = 1.0
	return v
}

// fixedEmbedder maps a query to a chosen axis.
type fixedEmbedder struct{ axis int }

func (f fixedEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return vec(f.axis), nil
}

func newEngine(t *testing.T, opts Options) *Engine {
	t.Helper()
	opts.PrincipalType = domain.PrincipalUser
	opts.PrincipalID = "recall-user"
	return NewEngine(pool, opts)
}

func reqFor(query string, strategies ...domain.StrategyType) *domain.RecallRequest {
	if strategies == nil {
		strategies = []domain.StrategyType{domain.StrategyVector}
	}
	return &domain.RecallRequest{
		RequestID:   uuid.Must(uuid.NewV7()),
		Query:       query,
		Trigger:     domain.TriggerUserQuery,
		AgentID:     "agent-1",
		SessionID:   uuid.Must(uuid.NewV7()),
		Mode:        domain.RecallModeSync,
		Status:      domain.RecallStatusCompleted,
		RequestedAt: utcNow(),
		RetrievalParams: domain.RetrievalParams{
			Strategies: strategies,
			TopK:       50,
			Rerank:     domain.RerankNone,
			MinScore:   0.01,
		},
	}
}

func candidateIDs(res *domain.RecallResult) []uuid.UUID {
	ids := make([]uuid.UUID, len(res.Candidates))
	for i, c := range res.Candidates {
		ids[i] = c.MemoryID
	}
	return ids
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestRecallVectorOnly(t *testing.T) {
	ctx := context.Background()
	seedMemory(t, "deploy steps for the payments service", func(m *domain.Memory) { m.Embedding = pgvec(vec(10)) })
	want := seedMemory(t, "database rotation runbook for postgres", func(m *domain.Memory) { m.Embedding = pgvec(vec(20)) })
	other := seedMemory(t, "unrelated note about coffee", func(m *domain.Memory) { m.Embedding = pgvec(vec(30)) })

	eng := newEngine(t, Options{Embedder: fixedEmbedder{axis: 20}})
	res, err := eng.Recall(ctx, reqFor("postgres rotation"))
	require.NoError(t, err)
	ids := candidateIDs(res)
	require.True(t, containsID(ids, want), "vector match missing; got %v", ids)
	require.False(t, containsID(ids, other), "orthogonal memory leaked in")
	// ranked first: distance 0 beats everything
	require.Equal(t, want, ids[0])
	// provenance recorded
	require.Equal(t, []domain.StrategyType{domain.StrategyVector}, res.Candidates[0].SourceStrategies)
}

func TestRecallFullTextOnly(t *testing.T) {
	ctx := context.Background()
	seedMemory(t, "the kettle is broken in the kitchen", nil)
	want := seedMemory(t, "quorum requires three nodes to commit writes", nil)
	seedMemory(t, "lunch menu for friday team sync", nil)

	eng := newEngine(t, Options{})
	res, err := eng.Recall(ctx, reqFor("quorum nodes", domain.StrategyBM25))
	require.NoError(t, err)
	ids := candidateIDs(res)
	require.NotEmpty(t, ids)
	require.True(t, containsID(ids, want), "fulltext match missing; got %v", ids)
	require.Equal(t, []domain.StrategyType{domain.StrategyBM25}, res.Candidates[0].SourceStrategies)
}

func TestRecallHybridFusion(t *testing.T) {
	ctx := context.Background()
	// both-match: rank 1 in vector (axis 5) and rank 1 in fulltext
	both := seedMemory(t, "kubernetes pod eviction thresholds", func(m *domain.Memory) { m.Embedding = pgvec(vec(5)) })
	// vector-only match: orthogonal wording, partial similarity to query axis
	vonly := seedMemory(t, "zzz qqq random filler text here", func(m *domain.Memory) { m.Embedding = pgvec(partialVec(6, 5)) })
	// fulltext-only match: right words, wrong axis
	fonly := seedMemory(t, "kubernetes pod eviction details and policy", func(m *domain.Memory) { m.Embedding = pgvec(vec(40)) })

	eng := newEngine(t, Options{Embedder: fixedEmbedder{axis: 5}})
	res, err := eng.Recall(ctx, reqFor("kubernetes pod eviction",
		domain.StrategyVector, domain.StrategyBM25))
	require.NoError(t, err)
	ids := candidateIDs(res)
	for _, id := range []uuid.UUID{both, vonly, fonly} {
		require.True(t, containsID(ids, id), "expected %s in %v", id, ids)
	}
	// RRF: rank-1 in two lists fuses above rank-1 in one
	require.Equal(t, both, ids[0])
	require.Len(t, res.Candidates[0].SourceStrategies, 2)
}

func TestRecallGraphTraversal(t *testing.T) {
	ctx := context.Background()
	entry := seedMemory(t, "graph entry point about caching", func(m *domain.Memory) { m.Embedding = pgvec(vec(60)) })
	hop1 := seedMemory(t, "one hop from entry", nil)
	hop2 := seedMemory(t, "two hops from entry", nil)
	unlinked := seedMemory(t, "totally unlinked memory", nil)

	seedLink(t, entry, hop1, "similar_to", 0.9)
	seedLink(t, hop1, hop2, "causal_next", 0.8)

	eng := newEngine(t, Options{Embedder: fixedEmbedder{axis: 60}, GraphHops: 2})
	res, err := eng.Recall(ctx, reqFor("caching", domain.StrategyVector, domain.StrategyGraph))
	require.NoError(t, err)
	ids := candidateIDs(res)
	require.True(t, containsID(ids, hop1), "1-hop expansion missing; got %v", ids)
	require.True(t, containsID(ids, hop2), "2-hop expansion missing; got %v", ids)
	require.False(t, containsID(ids, unlinked), "unlinked memory leaked into graph results")
	// proximity: hop1 (weight .9) outranks hop2 (.72)
	require.Less(t, indexOf(ids, hop1), indexOf(ids, hop2))
}

func TestRecallTemporalFiltering(t *testing.T) {
	ctx := context.Background()
	live := seedMemory(t, "currently valid fact one", func(m *domain.Memory) {
		vf, vu := utcNow().Add(-time.Hour), utcNow().Add(time.Hour)
		m.ValidFrom, m.ValidUntil = &vf, &vu
	})
	expired := seedMemory(t, "stale fact that should not surface", func(m *domain.Memory) {
		vf, vu := utcNow().Add(-48*time.Hour), utcNow().Add(-24*time.Hour)
		m.ValidFrom, m.ValidUntil = &vf, &vu
	})
	eternal := seedMemory(t, "fact with no temporal bounds", nil)

	eng := newEngine(t, Options{Embedder: fixedEmbedder{axis: 1}})
	res, err := eng.Recall(ctx, reqFor("fact", domain.StrategyBM25))
	require.NoError(t, err)
	ids := candidateIDs(res)
	require.True(t, containsID(ids, live), "valid memory missing; got %v", ids)
	require.True(t, containsID(ids, eternal), "no-bounds memory missing (Q2 fallback); got %v", ids)
	require.False(t, containsID(ids, expired), "expired memory leaked past temporal filter")
}

func TestRecallTemporalStrategyPointInTime(t *testing.T) {
	ctx := context.Background()
	past := utcNow().Add(-time.Duration(rand.Int63n(1e6)))
	historic := seedMemory(t, "valid only in the past", func(m *domain.Memory) {
		vf, vu := past.Add(-time.Hour), past.Add(time.Hour)
		m.ValidFrom, m.ValidUntil = &vf, &vu
	})
	eng := newEngine(t, Options{})
	bounds := &[2]time.Time{past, past}
	r := reqFor("anything", domain.StrategyTemporal)
	r.Context.TimeBounds = bounds
	res, err := eng.Recall(ctx, r)
	require.NoError(t, err)
	require.True(t, containsID(candidateIDs(res), historic),
		"point-in-time recall missed interval containing %s", past)
}

func TestRecallTopKLimit(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		seedMemory(t, fmt.Sprintf("bulk memory %d about shared topic tokens", i), nil)
	}
	eng := newEngine(t, Options{})
	r := reqFor("shared topic tokens", domain.StrategyBM25)
	r.RetrievalParams.TopK = 3
	res, err := eng.Recall(ctx, r)
	require.NoError(t, err)
	require.Len(t, res.Candidates, 3)
	require.LessOrEqual(t, len(res.InjectionPlan), 3)
}

func TestRecallOwnerScopeFiltering(t *testing.T) {
	ctx := context.Background()
	seedMemory(t, "someone else private secret memory", func(m *domain.Memory) {
		m.OwnerPrincipalID = "other-user"
	})

	eng := newEngine(t, Options{})
	res, err := eng.Recall(ctx, reqFor("secret", domain.StrategyBM25))
	require.NoError(t, err)
	for _, c := range res.Candidates {
		require.NotEqual(t, "someone else private secret memory", c.MemoryID.String(),
			"foreign-owned memory must not surface")
	}
	require.Empty(t, candidateIDs(res), "owner filter failed: got %v", candidateIDs(res))
}

func TestRecallInjectionPlanBudgets(t *testing.T) {
	ctx := context.Background()
	// long content: ~400 tokens each at chars/4
	var ids []uuid.UUID
	for i := 0; i < 5; i++ {
		content := ""
		for j := 0; j < 200; j++ {
			content += fmt.Sprintf("budget test %d-%d ", i, j)
		}
		ids = append(ids, seedMemory(t, content, nil))
	}
	eng := newEngine(t, Options{})
	r := reqFor("budget test", domain.StrategyBM25)
	tok := 1000 // ~2 long memories
	r.RetrievalParams.TokenBudget = &tok
	res, err := eng.Recall(ctx, r)
	require.NoError(t, err)
	require.NotEmpty(t, res.Candidates)
	require.LessOrEqual(t, res.TokensUsed, 1000)
	require.Len(t, res.InjectionPlan, res.SlotsUsed)
	// positions are dense and ordered
	for i, item := range res.InjectionPlan {
		require.Equal(t, i, item.Position)
		require.Equal(t, 1, item.SlotCost)
	}
	_ = ids
}

func TestRerankNonePassesThrough(t *testing.T) {
	ctx := context.Background()
	id := seedMemory(t, "identity reranker target memory alpha", nil)
	eng := newEngine(t, Options{})
	res, err := eng.Recall(ctx, reqFor("alpha", domain.StrategyBM25))
	require.NoError(t, err)
	require.True(t, containsID(candidateIDs(res), id))
	for _, c := range res.Candidates {
		require.Nil(t, c.RerankScore, "rerank=none must leave RerankScore unset")
	}
}

func indexOf(ids []uuid.UUID, id uuid.UUID) int {
	for i, x := range ids {
		if x == id {
			return i
		}
	}
	return -1
}

// partialVec is a unit vector dominated by `dominant` with a 0.3 component
// on `shared` — cosine ~0.30 to a shared-axis query: a real partial dense
// match that clears the similarity floor without ranking first.
func partialVec(dominant, shared int) []float32 {
	v := make([]float32, 1536)
	v[dominant%1536] = 0.95
	v[shared%1536] = 0.3
	norm := 0.0
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	n := float32(1.0 / normSqrt(norm))
	for i := range v {
		v[i] *= n
	}
	return v
}

func normSqrt(x float64) float64 {
	if x <= 0 {
		return 1
	}
	r := x
	for i := 0; i < 40; i++ {
		r = (r + x/r) / 2
	}
	return r
}
