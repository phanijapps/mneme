package port

import (
	"context"

	"github.com/phanijapps/mneme/internal/domain"
)

// RecallEngine executes the 4-way hybrid search (architecture.md §2.2):
// vector, BM25-like full-text, graph traversal, and temporal — in parallel,
// then merges and re-ranks into the final result. Implementations fan out to
// the strategies named in RecallRequest.RetrievalParams.Strategies.
type RecallEngine interface {
	// Recall runs the hybrid search for one request and returns the merged
	// RecallResult: candidates with source_strategies recorded per entry, the
	// ordered injection plan, and slot/token usage against the budgets.
	Recall(ctx context.Context, req *domain.RecallRequest) (*domain.RecallResult, error)
}
