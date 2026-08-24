package recall

import (
	"context"

	"github.com/phanijapps/mneme/internal/domain"
)

// Reranker re-scores fused candidates against the original query
// (architecture.md §2.2: cross-encoder over the top 20–50). Pluggable; v1
// ships the identity stub.
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []domain.RecallCandidate) ([]domain.RecallCandidate, error)
}

// IdentityReranker passes candidates through unchanged — the v1 stub.
//
// A real implementation would call a cross-encoder model (e.g. an HTTP
// reranking service scoring (query, memory-content) pairs) and reorder
// candidates by that score, setting RerankScore. The engine calls it only
// when params.Rerank == cross_encoder; "none" skips re-ranking entirely.
type IdentityReranker struct{}

// Rerank implements Reranker by returning the input unchanged.
func (IdentityReranker) Rerank(_ context.Context, _ string, candidates []domain.RecallCandidate) ([]domain.RecallCandidate, error) {
	return candidates, nil
}
