package recall

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/phanijapps/mneme/internal/domain"
)

// GraphSearch is the associative path (Q3): a recursive CTE over
// memory_links seeded by phase-1 entry points, accumulating path weight and
// capped at maxHops (default 2). FalkorDB pattern: vector entry, graph
// expansion (retrieval-and-recall.md).
type GraphSearch struct{ pool DB }

// NewGraphSearch builds the graph strategy over pool.
func NewGraphSearch(pool DB) *GraphSearch { return &GraphSearch{pool: pool} }

// walkableRelTypes restricts the walk to associative edges (Q3);
// supersedes/anchors_entity are lifecycle edges, not recall paths.
const walkableRelTypes = "('similar_to', 'co_occurs_with', 'causal_next', 'derived_from')"

// Search expands from seedIDs up to maxHops and returns non-seed memories
// ranked by best path weight (product of edge weights, closer = higher).
// The origin guard blocks trivial A→B→A bounce; maxHops bounds cycles.
func (g *GraphSearch) Search(ctx context.Context, seedIDs []uuid.UUID, maxHops int, sc scope, topK int) (StrategyResult, error) {
	if len(seedIDs) == 0 {
		return StrategyResult{Strategy: domain.StrategyGraph, Items: nil}, nil
	}
	if maxHops <= 0 {
		maxHops = 2
	}

	seedArr := make([]string, len(seedIDs))
	for i, id := range seedIDs {
		seedArr[i] = id.String()
	}

	b := &qb{}
	b.sql.WriteString(`WITH RECURSIVE walk (memory_id, origin_id, hops, path_weight) AS (
	    SELECT s.id, s.id, 0, 1.0::float8
	      FROM unnest(`)
	b.sql.WriteString(b.arg(seedArr))
	b.sql.WriteString(`::uuid[]) AS s(id)
	    UNION ALL
	    SELECT l.target_id, w.origin_id, w.hops + 1,
	           (w.path_weight * l.weight)::float8
	      FROM walk w
	      JOIN memory_links l ON l.source_id = w.memory_id
	     WHERE w.hops < `)
	b.sql.WriteString(b.arg(maxHops))
	fmt.Fprintf(&b.sql, `
	       AND l.relationship_type IN %s
	       AND l.target_id <> ANY (ARRAY[w.memory_id, w.origin_id])
	  )
	  SELECT m.id, max(w.path_weight)::float8 AS score
	    FROM walk w
	    JOIN memories m ON m.id = w.memory_id AND w.hops > 0
	   WHERE `, walkableRelTypes)
	sc.where(b, false)
	sc.visibility(b)
	fmt.Fprintf(&b.sql, `
	   GROUP BY m.id
	   ORDER BY score DESC, m.id
	   LIMIT %s`, b.arg(topK))

	rows, err := g.pool.Query(ctx, b.sql.String(), b.args...)
	if err != nil {
		return StrategyResult{}, mapQueryErr(err)
	}
	items, err := pgx.CollectRows(rows, scanRankedItem)
	if err != nil {
		return StrategyResult{}, mapQueryErr(err)
	}
	return StrategyResult{Strategy: domain.StrategyGraph, Items: items}, nil
}
