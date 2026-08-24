package recall

import (
	"sort"

	"github.com/google/uuid"

	"github.com/phanijapps/mneme/internal/domain"
)

// RRFK is the standard Reciprocal Rank Fusion constant (k=60): flattens the
// 1/(k+rank) contribution so no single strategy dominates the fused score.
const RRFK = 60.0

// FuseRRF merges ranked lists with Reciprocal Rank Fusion:
// score(d) = Σ_strategies 1/(k + rank_i(d)). Candidates appearing in more
// strategies (and higher in each) fuse higher; source_strategies records
// provenance. RRF scores are ≤ n_strategies/(k+1) ≈ 0.065 for 4 paths, so
// min_score comparisons use the normalized form.
func FuseRRF(sets []StrategyResult, k float64) []domain.RecallCandidate {
	if len(sets) == 0 {
		return []domain.RecallCandidate{}
	}
	scores := make(map[uuid.UUID]float64, 64)
	strategies := make(map[uuid.UUID][]domain.StrategyType, 64)
	for _, set := range sets {
		for rank, item := range set.Items {
			scores[item.MemoryID] += 1.0 / (k + float64(rank+1))
			if !containsStrategy(strategies[item.MemoryID], set.Strategy) {
				strategies[item.MemoryID] = append(strategies[item.MemoryID], set.Strategy)
			}
		}
	}

	candidates := make([]domain.RecallCandidate, 0, len(scores))
	for id, score := range scores {
		// Normalize by the theoretical max (rank 1 in every set) so the
		// default min_score=0.35 can prune; raw RRF would never reach it.
		maxScore := float64(len(sets)) / (k + 1.0)
		candidates = append(candidates, domain.RecallCandidate{
			MemoryID:         id,
			Score:            score / maxScore,
			SourceStrategies: strategies[id],
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].MemoryID.String() < candidates[j].MemoryID.String()
	})
	return candidates
}

// FilterByMinScore drops candidates below min. min<=0 keeps everything.
func FilterByMinScore(candidates []domain.RecallCandidate, min float64) []domain.RecallCandidate {
	if min <= 0 {
		return candidates
	}
	out := candidates[:0:0]
	for _, c := range candidates {
		if c.Score >= min {
			out = append(out, c)
		}
	}
	return out
}

func containsStrategy(list []domain.StrategyType, s domain.StrategyType) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
