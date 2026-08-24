package recall

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/phanijapps/mneme/internal/domain"
)

// FullTextSearch is the sparse path (Q1 sparse CTE): ts_rank_cd over the
// generated search_tsv with websearch_to_tsquery. BM25-like, not true BM25
// — ts_rank_cd is cover-density term-frequency, no IDF weighting
// (retrieval-and-recall.md; evaluate ParadeDB pg_search if lexical quality
// disappoints, F13).
type FullTextSearch struct{ pool DB }

// NewFullTextSearch builds the sparse strategy over pool.
func NewFullTextSearch(pool DB) *FullTextSearch { return &FullTextSearch{pool: pool} }

// Search matches the websearch tsquery and ranks by ts_rank_cd descending.
func (f *FullTextSearch) Search(ctx context.Context, query string, sc scope, topK int) (StrategyResult, error) {
	b := &qb{}
	b.sql.WriteString(`SELECT m.id,
	       ts_rank_cd(m.search_tsv, websearch_to_tsquery('english', `)
	qP := b.arg(query)
	b.sql.WriteString(qP)
	b.sql.WriteString(`))::float8 AS score
	  FROM memories m
	 WHERE m.search_tsv @@ websearch_to_tsquery('english', `)
	b.sql.WriteString(qP)
	b.sql.WriteString(`) AND `)
	sc.where(b, false)
	sc.visibility(b)
	fmt.Fprintf(&b.sql, `
	 ORDER BY ts_rank_cd(m.search_tsv, websearch_to_tsquery('english', %s)) DESC
	 LIMIT %s`, qP, b.arg(topK))

	rows, err := f.pool.Query(ctx, b.sql.String(), b.args...)
	if err != nil {
		return StrategyResult{}, mapQueryErr(err)
	}
	items, err := pgx.CollectRows(rows, scanRankedItem)
	if err != nil {
		return StrategyResult{}, mapQueryErr(err)
	}
	return StrategyResult{Strategy: domain.StrategyBM25, Items: items}, nil
}
