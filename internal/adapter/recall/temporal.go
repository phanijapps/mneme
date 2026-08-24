package recall

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/phanijapps/mneme/internal/domain"
)

// TemporalSearch is the time-bounded path (Q2): GiST validity_range @>
// containment at the request's point in time, ordered by confidence then
// recency. The Q2 validity predicate is additionally applied as a WHERE
// clause on every other strategy via scope.where, so this strategy exists
// for temporal-triggered recall where relevance IS validity.
type TemporalSearch struct{ pool DB }

// NewTemporalSearch builds the temporal strategy over pool.
func NewTemporalSearch(pool DB) *TemporalSearch { return &TemporalSearch{pool: pool} }

// Search returns memories valid at sc.at ranked by confidence (Q2's ORDER
// BY). score = confidence when present, else decay_score, else 0.
func (t *TemporalSearch) Search(ctx context.Context, sc scope, topK int) (StrategyResult, error) {
	b := &qb{}
	b.sql.WriteString(`SELECT m.id,
	       coalesce(m.confidence, m.decay_score, 0)::float8 AS score
	  FROM memories m
	 WHERE `)
	sc.where(b, false)
	sc.visibility(b)
	fmt.Fprintf(&b.sql, `
	 ORDER BY m.confidence DESC NULLS LAST, m.created_at DESC
	 LIMIT %s`, b.arg(topK))

	rows, err := t.pool.Query(ctx, b.sql.String(), b.args...)
	if err != nil {
		return StrategyResult{}, mapQueryErr(err)
	}
	items, err := pgx.CollectRows(rows, scanRankedItem)
	if err != nil {
		return StrategyResult{}, mapQueryErr(err)
	}
	return StrategyResult{Strategy: domain.StrategyTemporal, Items: items}, nil
}
