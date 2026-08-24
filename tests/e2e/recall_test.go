//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recallResponse struct {
	Result struct {
		Candidates []struct {
			MemoryID         string   `json:"memory_id"`
			Score            float64  `json:"score"`
			SourceStrategies []string `json:"source_strategies"`
		} `json:"candidates"`
		InjectionPlan []struct {
			MemoryID string `json:"memory_id"`
			Position int    `json:"position"`
			SlotCost int    `json:"slot_cost"`
		} `json:"injection_plan"`
		SlotsUsed  int   `json:"slots_used"`
		TokensUsed int   `json:"tokens_used"`
		LatencyMS  *int  `json:"latency_ms"`
	} `json:"result"`
}

func recallBody(sessionID, query string, strategies []string, extra map[string]any) map[string]any {
	rp := map[string]any{"strategies": strategies}
	for k, v := range extra {
		rp[k] = v
	}
	return map[string]any{
		"query":            query,
		"trigger":          "user_query",
		"agent_id":         "e2e-agent",
		"session_id":       sessionID,
		"retrieval_params": rp,
	}
}

// TestRecallFlow seeds memories with known embeddings and text, then
// verifies hybrid recall end-to-end: scores, multi-strategy provenance,
// top_k clamping, and budget behavior.
func TestRecallFlow(t *testing.T) {
	c := newTestServer(t)
	tag := fmt.Sprintf("e2e-recall-%d", time.Now().UnixNano())

	// Seed: 3 deployment-related memories on the deployment axis (dense
	// hits), plus off-topic distractors on other axes.
	dep1 := saveMemoryWithVector(t, "semantic", "Deployment pipeline runs on Kubernetes with rolling updates", []string{tag}, 0, "agent_observation")
	dep2 := saveMemoryWithVector(t, "procedural", "Postgres migration checklist for the deployment runbook", []string{tag}, 0, "agent_observation")
	dep3 := saveMemoryWithVector(t, "semantic", "Vector index rebuild procedure after bulk ingestion", []string{tag}, 3, "agent_observation")
	saveMemoryWithVector(t, "episodic", "Pasta recipes from the team offsite cooking session", []string{tag}, 7, "agent_observation")
	saveMemoryWithVector(t, "semantic", "Gardening notes: tomatoes need full sun", []string{tag}, 8, "agent_observation")

	session := c.createTestSession(t, "user-recall", 0)
	sessionID := mustUUID(t, sessionMap(t, session), "session_id")

	// Sync recall, all strategies. The query matches deployment text (bm25)
	// and the deployment axis (vector); temporal matches everything valid.
	var resp recallResponse
	body := recallBody(sessionID.String(), "deployment kubernetes runbook",
		[]string{"vector", "bm25", "temporal"}, map[string]any{"min_score": 0.0})
	code, raw := c.do("POST", "/api/v1/recall", body, &resp)
	requireOK(t, code, raw, http.StatusOK)
	require.NotEmpty(t, resp.Result.Candidates, "recall returns candidates")

	// Relevance scores present and in (0, 1].
	for _, cand := range resp.Result.Candidates {
		assert.Greater(t, cand.Score, 0.0, "candidate carries a relevance score")
		assert.LessOrEqual(t, cand.Score, 1.0)
	}

	// The deployment hits must be present with multi-strategy provenance.
	found := map[string][]string{}
	for _, cand := range resp.Result.Candidates {
		found[cand.MemoryID] = cand.SourceStrategies
	}
	require.Contains(t, found, dep1.String(), "dense/text hit dep1 returned")
	assert.GreaterOrEqual(t, len(found[dep1.String()]), 2,
		"dep1 surfaced by multiple strategies: %v", found[dep1.String()])
	require.Contains(t, found, dep2.String())
	require.Contains(t, found, dep3.String())

	// Ordering: top fused candidate is a deployment hit.
	assert.Equal(t, dep1.String(), resp.Result.Candidates[0].MemoryID)

	// Injection plan is ordered and non-empty.
	require.NotEmpty(t, resp.Result.InjectionPlan)
	for i, item := range resp.Result.InjectionPlan {
		assert.Equal(t, i, item.Position, "plan positions are contiguous from 0")
	}
	require.NotNil(t, resp.Result.LatencyMS, "latency recorded")

	// Type filter (via list endpoint semantics on recall context is not
	// supported; filters verified through memory listing instead — here we
	// verify tag/type filtering on the list surface).
	var page struct {
		Items []map[string]any `json:"items"`
	}
	code, raw = c.do("GET", "/api/v1/memories?type=procedural&tags="+tag, nil, &page)
	requireOK(t, code, raw, http.StatusOK)
	require.Len(t, page.Items, 1)
	assert.Contains(t, page.Items[0]["content"], "checklist")

	// top_k limit is respected.
	var capped recallResponse
	body = recallBody(sessionID.String(), "deployment kubernetes postgres vector budget telemetry",
		[]string{"bm25"}, map[string]any{"top_k": 2, "min_score": 0.0})
	code, raw = c.do("POST", "/api/v1/recall", body, &capped)
	requireOK(t, code, raw, http.StatusOK)
	assert.LessOrEqual(t, len(capped.Result.Candidates), 2, "top_k bounds candidates")

	// top_k above the max is clamped to 200 (validation would reject >200,
	// so verify the ceiling holds at the boundary: 200 is accepted).
	var atMax recallResponse
	body = recallBody(sessionID.String(), "deployment", []string{"bm25"},
		map[string]any{"top_k": 200, "min_score": 0.0})
	code, raw = c.do("POST", "/api/v1/recall", body, &atMax)
	requireOK(t, code, raw, http.StatusOK)
	assert.LessOrEqual(t, len(atMax.Result.Candidates), 200)

	// min_score filters fused scores.
	var strict recallResponse
	body = recallBody(sessionID.String(), "deployment kubernetes",
		[]string{"vector"}, map[string]any{"min_score": 0.99})
	code, raw = c.do("POST", "/api/v1/recall", body, &strict)
	requireOK(t, code, raw, http.StatusOK)
	for _, cand := range strict.Result.Candidates {
		assert.GreaterOrEqual(t, cand.Score, 0.99)
	}

	// Session budget enforcement: slot_budget above the session's budget
	// is rejected with RECALL_BUDGET_INVALID (422).
	tight := c.createTestSession(t, "user-recall-tight", 2)
	var errOut errBody
	body = recallBody(mustUUID(t, sessionMap(t, tight), "session_id").String(), "deployment",
		[]string{"bm25"}, map[string]any{"slot_budget": 4})
	code, raw = c.do("POST", "/api/v1/recall", body, &errOut)
	assert.Equal(t, http.StatusUnprocessableEntity, code, "body: %s", raw)
	assert.Equal(t, "RECALL_BUDGET_INVALID", errOut.Error.Code)
}
