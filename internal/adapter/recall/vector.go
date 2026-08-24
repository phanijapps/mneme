package recall

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/phanijapps/mneme/internal/domain"
)

// VectorSearch is the dense path (Q1 dense CTE): HNSW cosine over
// memories.embedding, cast to halfvec to match memories_embedding_hnsw_idx.
type VectorSearch struct{ pool DB }

// NewVectorSearch builds the dense strategy over pool.
func NewVectorSearch(pool DB) *VectorSearch { return &VectorSearch{pool: pool} }

// Search embeds the query and returns memories ranked by cosine similarity
// (1 − distance), descending. The memories.embedding column is 1536-dim;
// alternate-model (768) vectors live in memory_embeddings.vec_768 and are
// searched when opts.model pins a 768-dim model.
func (v *VectorSearch) Search(ctx context.Context, query string, embedder QueryEmbedder, model string, sc scope, topK int, minSimilarity float64) (StrategyResult, error) {
	vec, err := embedder.EmbedQuery(ctx, query)
	if err != nil {
		return StrategyResult{}, fmt.Errorf("embed query: %w", err)
	}
	if len(vec) == 0 {
		return StrategyResult{}, fmt.Errorf("empty query embedding")
	}
	switch len(vec) {
	case 1536, 768:
	default:
		return StrategyResult{}, fmt.Errorf("unsupported embedding dimension %d (want 1536 or 768)", len(vec))
	}

	var items []RankedItem
	if model != "" && len(vec) == 768 {
		items, err = v.searchAltModel(ctx, vec, model, sc, topK, minSimilarity)
	} else if len(vec) == 768 {
		// No model pinned but query is 768-dim: pad-to-1536 is lossy; the
		// registry only stores typed columns, so search the alt table for
		// any 768 model present.
		items, err = v.searchAltModel(ctx, vec, "", sc, topK, minSimilarity)
	} else {
		items, err = v.searchPrimary(ctx, vec, sc, topK, minSimilarity)
	}
	if err != nil {
		return StrategyResult{}, err
	}
	return StrategyResult{Strategy: domain.StrategyVector, Items: items}, nil
}

// searchPrimary hits memories.embedding (vector(1536)) through the halfvec
// HNSW index — the Q1 dense CTE, scoped.
func (v *VectorSearch) searchPrimary(ctx context.Context, vec []float32, sc scope, topK int, minSimilarity float64) ([]RankedItem, error) {
	b := &qb{}
	b.sql.WriteString(`SELECT m.id,
	       (1 - ((m.embedding::halfvec(`)
	vecP := b.arg(vectorArg(vec))
	fmt.Fprintf(&b.sql, `%d)) <=> (%s::halfvec(%d))))::float8 AS score
	  FROM memories m
	 WHERE m.embedding IS NOT NULL
	   AND (1 - ((m.embedding::halfvec(%d)) <=> (%s::halfvec(%d))))::float8 >= %s
	   AND `, len(vec), vecP, len(vec), len(vec), vecP, len(vec), b.arg(minSimilarity))
	sc.where(b, false)
	sc.visibility(b)
	fmt.Fprintf(&b.sql, `
	 ORDER BY (m.embedding::halfvec(%d)) <=> (%s::halfvec(%d))
	 LIMIT %s`, len(vec), vecP, len(vec), b.arg(topK))

	rows, err := v.pool.Query(ctx, b.sql.String(), b.args...)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	items, err := pgx.CollectRows(rows, scanRankedItem)
	return items, mapQueryErr(err)
}

// searchAltModel hits memory_embeddings.vec_768 (HNSW 768) for
// alternate-model vectors, joining memories for the scope filters.
func (v *VectorSearch) searchAltModel(ctx context.Context, vec []float32, model string, sc scope, topK int, minSimilarity float64) ([]RankedItem, error) {
	b := &qb{}
	b.sql.WriteString(`SELECT m.id,
	       (1 - ((me.vec_768::halfvec(`)
	vecP := b.arg(vectorArg(vec))
	modelP := b.arg(model)
	fmt.Fprintf(&b.sql, `768)) <=> (%s::halfvec(768))))::float8 AS score
	  FROM memories m
	  JOIN memory_embeddings me ON me.memory_id = m.id
	 WHERE me.vec_768 IS NOT NULL
	   AND (%s = '' OR me.model_id = %s)
	   AND (1 - ((me.vec_768::halfvec(768)) <=> (%s::halfvec(768))))::float8 >= %s
	   AND `, vecP, modelP, modelP, vecP, b.arg(minSimilarity))
	sc.where(b, false)
	sc.visibility(b)
	fmt.Fprintf(&b.sql, `
	 ORDER BY (me.vec_768::halfvec(768)) <=> (%s::halfvec(768))
	 LIMIT %s`, vecP, b.arg(topK))

	rows, err := v.pool.Query(ctx, b.sql.String(), b.args...)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	items, err := pgx.CollectRows(rows, scanRankedItem)
	return items, mapQueryErr(err)
}
