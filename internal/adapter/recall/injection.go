package recall

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/phanijapps/mneme/internal/domain"
)

// Injection budgets default to the working-memory envelope from
// retrieval-and-recall.md: ~100–150 usable instruction slots of the
// ~150–200 an agent reliably follows (Claude Code evidence), and a
// conservative token ceiling.
const (
	DefaultSlotBudget  = 100
	DefaultTokenBudget = 8000
)

// tokensForChar is a rough chars/4 token estimate — the standard heuristic
// when no tokenizer is wired. slot_cost is 1 per memory (one instruction
// slot each).
func tokensForChars(n int) int { return n / 4 }

// ContentSource fetches per-memory content lengths for slot/token costing.
type ContentSource struct{ pool DB }

// NewContentSource builds the content fetcher over pool.
func NewContentSource(pool DB) *ContentSource { return &ContentSource{pool: pool} }

// ContentLengths returns char_count(content) per requested id; missing ids
// are absent from the map.
func (c *ContentSource) ContentLengths(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	arr := make([]string, len(ids))
	for i, id := range ids {
		arr[i] = id.String()
	}
	rows, err := c.pool.Query(ctx,
		`SELECT id, char_length(content) FROM memories WHERE id = ANY($1::uuid[])`, arr)
	if err != nil {
		return nil, mapQueryErr(err)
	}
	type lenRow struct {
		ID  uuid.UUID
		Len int
	}
	got, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (lenRow, error) {
		var lr lenRow
		err := row.Scan(&lr.ID, &lr.Len)
		return lr, err
	})
	if err != nil {
		return nil, mapQueryErr(err)
	}
	for _, lr := range got {
		out[lr.ID] = lr.Len
	}
	return out, nil
}

// BuildInjectionPlan orders candidates by score (rerank_score wins when
// set — plan.go §"order by rerank_score descending") and fills positions
// while both budgets hold. Returns the plan plus totals actually used.
// Budgets of 0 mean "unbounded" (0-value requests are validated as
// min=0; a nil budget uses the defaults above).
func BuildInjectionPlan(candidates []domain.RecallCandidate, lengths map[uuid.UUID]int, slotBudget, tokenBudget int) ([]domain.InjectionPlanItem, int, int, error) {
	ordered := make([]domain.RecallCandidate, len(candidates))
	copy(ordered, candidates)
	sort.SliceStable(ordered, func(i, j int) bool {
		si, sj := planScore(ordered[i]), planScore(ordered[j])
		if si != sj {
			return si > sj
		}
		return ordered[i].MemoryID.String() < ordered[j].MemoryID.String()
	})

	plan := make([]domain.InjectionPlanItem, 0, len(ordered))
	slotsUsed, tokensUsed := 0, 0
	for _, c := range ordered {
		if slotBudget > 0 && slotsUsed+1 > slotBudget {
			break
		}
		tokens := tokensForChars(lengths[c.MemoryID])
		if tokenBudget > 0 && tokensUsed+tokens > tokenBudget {
			break
		}
		plan = append(plan, domain.InjectionPlanItem{
			MemoryID: c.MemoryID,
			Position: len(plan),
			SlotCost: 1,
		})
		slotsUsed++
		tokensUsed += tokens
	}
	return plan, slotsUsed, tokensUsed, nil
}

func planScore(c domain.RecallCandidate) float64 {
	if c.RerankScore != nil {
		return *c.RerankScore
	}
	return c.Score
}
