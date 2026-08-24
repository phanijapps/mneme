//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionFlow: create → activate → check usage → deactivate → end
// (with consolidation job enqueued).
func TestSessionFlow(t *testing.T) {
	c := newTestServer(t)

	created := c.createTestSession(t, "user-session", 3)
	sessionID := mustUUID(t, sessionMap(t, created), "session_id")

	m1 := c.createTestMemory(t, "semantic", "Session test memory one", []string{"e2e-session"})
	m2 := c.createTestMemory(t, "procedural", "Session test memory two", []string{"e2e-session"})

	// Activate a memory: usage window reflects it.
	var act map[string]any
	code, raw := c.do("POST", "/api/v1/sessions/"+sessionID.String()+"/memories",
		map[string]any{"memory_id": m1["id"]}, &act)
	requireOK(t, code, raw, http.StatusOK)
	assert.NotEmpty(t, act["injection"], "activation returns an injection item")

	var got struct {
		Session map[string]any `json:"session"`
		Usage   struct {
			TokensRemaining int `json:"tokens_remaining"`
			SlotsRemaining  int `json:"slots_remaining"`
		} `json:"usage"`
	}
	code, raw = c.do("GET", "/api/v1/sessions/"+sessionID.String(), nil, &got)
	requireOK(t, code, raw, http.StatusOK)
	cw := got.Session["context_window"].(map[string]any)
	assert.Equal(t, float64(1), cw["slots_used"], "one active memory consumes one slot")
	assert.Equal(t, 2, got.Usage.SlotsRemaining, "slot budget accounting")

	// Deactivate frees the slot.
	code, raw = c.do("DELETE",
		"/api/v1/sessions/"+sessionID.String()+"/memories/"+m1["id"].(string), nil, nil)
	requireOK(t, code, raw, http.StatusNoContent)

	code, raw = c.do("GET", "/api/v1/sessions/"+sessionID.String(), nil, &got)
	requireOK(t, code, raw, http.StatusOK)
	cw = got.Session["context_window"].(map[string]any)
	assert.Equal(t, float64(0), cw["slots_used"], "deactivation frees the slot")

	// Budget enforcement: budget 1, keep one memory active, second activate
	// fails with SLOT_BUDGET_EXCEEDED.
	tight := c.createTestSession(t, "user-tight", 1)
	tightID := mustUUID(t, sessionMap(t, tight), "session_id")
	code, raw = c.do("POST", "/api/v1/sessions/"+tightID.String()+"/memories",
		map[string]any{"memory_id": m1["id"]}, nil)
	requireOK(t, code, raw, http.StatusOK)
	var errOut errBody
	code, raw = c.do("POST", "/api/v1/sessions/"+tightID.String()+"/memories",
		map[string]any{"memory_id": m2["id"]}, &errOut)
	assert.Equal(t, http.StatusConflict, code, "body: %s", raw)
	assert.Equal(t, "SLOT_BUDGET_EXCEEDED", errOut.Error.Code)

	// End session: 202 with a consolidation job reference.
	var end struct {
		Session          map[string]any  `json:"session"`
		ConsolidationJob *map[string]any `json:"consolidation_job"`
	}
	code, raw = c.do("POST", "/api/v1/sessions/"+sessionID.String()+"/end",
		map[string]any{"summary": "e2e session complete"}, &end)
	requireOK(t, code, raw, http.StatusAccepted)
	require.NotNil(t, end.ConsolidationJob, "ending enqueues consolidation")
	job := *end.ConsolidationJob
	jobID, ok := job["job_id"].(string)
	require.True(t, ok, "job ref carries job_id: %v", job)

	// The job is pollable via the lifecycle surface.
	var jobBody map[string]any
	code, raw = c.do("GET", "/api/v1/lifecycle/jobs/"+jobID, nil, &jobBody)
	requireOK(t, code, raw, http.StatusOK)
	assert.Equal(t, "consolidation", jobBody["kind"])
	assert.Equal(t, "queued", jobBody["status"])
}
