//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMemoryLifecycle walks one memory through the full stack: save → get →
// supersede → list → soft delete → verify retention.
func TestMemoryLifecycle(t *testing.T) {
	c := newTestServer(t)
	tag := fmt.Sprintf("e2e-mem-%d", time.Now().UnixNano())

	// Save.
	saved := c.createTestMemory(t, "episodic", "Deployed mneme v0.1 to staging cluster", []string{tag})
	memoryID := mustUUID(t, saved, "id")
	require.Equal(t, float64(1), saved["version"], "new memory starts at version 1")
	assert.Equal(t, "e2e-agent", saved["agent_id"], "server-stamped agent_id (F21)")
	assert.Equal(t, "e2e-agent", saved["actor"], "server-stamped actor (F21)")
	assert.Equal(t, "agent", saved["owner_principal_type"])
	require.NotNil(t, saved["decay_score"], "decay_score defaults to 1.0")
	assert.InDelta(t, 1.0, saved["decay_score"].(float64), 1e-9)
	require.NotNil(t, saved["valid_from"], "validity opened at save")

	// Get by ID round-trips.
	var got map[string]any
	code, raw := c.do("GET", "/api/v1/memories/"+memoryID.String(), nil, &got)
	requireOK(t, code, raw, http.StatusOK)
	assert.Equal(t, saved["content"], got["content"])
	assert.Equal(t, saved["id"], got["id"])

	// Supersession-first: a content update creates version 2 as a NEW row;
	// the old row is closed and points at its successor.
	newContent := "Deployed mneme v0.2 to staging cluster"
	var updated map[string]any
	code, raw = c.do("PUT", "/api/v1/memories/"+memoryID.String(), map[string]any{
		"content":   newContent,
		"confidence": 0.95,
	}, &updated)
	requireOK(t, code, raw, http.StatusOK)
	require.Equal(t, float64(2), updated["version"], "content change bumps version")
	assert.NotEqual(t, memoryID.String(), updated["id"], "supersession inserts a new row")

	// Old version: closed validity, superseded_by set, still on disk.
	ver, validUntil, _, supersededBy, _, exists := memoryRow(t, memoryID)
	require.True(t, exists, "old version row retained (supersession-first)")
	assert.Equal(t, 1, ver)
	require.NotNil(t, validUntil, "old version validity closed")
	require.NotNil(t, supersededBy, "old version points at successor")
	assert.Equal(t, updated["id"], supersededBy.String())

	// The old id no longer resolves as live state (§GET /memories/{id}:
	// superseded → 410 MEMORY_EXPIRED, body still carries superseded_by).
	var errOut errBody
	code, raw = c.do("GET", "/api/v1/memories/"+memoryID.String(), nil, &errOut)
	assert.Equal(t, http.StatusGone, code, "superseded id is not the live row: %s", raw)
	assert.Equal(t, "MEMORY_EXPIRED", errOut.Error.Code)
	assert.Equal(t, updated["id"], errOut.Error.Details["superseded_by"])

	// List with type + tag filters finds the live version only.
	var page struct {
		Items []map[string]any `json:"items"`
	}
	code, raw = c.do("GET", "/api/v1/memories?type=episodic&tags="+tag, nil, &page)
	requireOK(t, code, raw, http.StatusOK)
	require.Len(t, page.Items, 1, "one live row for this tag")
	assert.Equal(t, updated["id"], page.Items[0]["id"])
	assert.NotEmpty(t, page.Items[0]["content"])

	// Metadata-only update does NOT bump version (no supersession).
	code, raw = c.do("PUT", "/api/v1/memories/"+updated["id"].(string), map[string]any{
		"confidence": 0.5,
	}, &updated)
	requireOK(t, code, raw, http.StatusOK)
	assert.Equal(t, float64(2), updated["version"], "metadata-only update keeps version")

	liveID := updated["id"].(string)

	// Soft delete: 204, row retained with deleted_at, validity closed.
	code, raw = c.do("DELETE", "/api/v1/memories/"+liveID, nil, nil)
	requireOK(t, code, raw, http.StatusNoContent)

	_, validUntil2, deletedAt, _, _, exists2 := memoryRow(t, mustUUIDFromString(t, liveID))
	require.True(t, exists2, "soft delete retains the row")
	require.NotNil(t, deletedAt, "deleted_at set")
	require.NotNil(t, validUntil2, "validity closed on delete")

	// Deleted memory is gone from the live surface.
	code, _ = c.do("GET", "/api/v1/memories/"+liveID, nil, nil)
	assert.Equal(t, http.StatusNotFound, code)
}

func mustUUIDFromString(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}
