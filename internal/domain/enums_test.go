package domain

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runParseTest parses each valid value (checking round-trip via any()) and
// asserts an error for an invalid one.
func runParseTest[T ~string](t *testing.T, parse func(string) (T, error), valid []string) {
	t.Helper()
	for _, raw := range valid {
		v, err := parse(raw)
		require.NoError(t, err, "parse %q", raw)
		assert.Equal(t, raw, any(v).(fmt.Stringer).String(), "String() should round-trip")
	}
	if _, err := parse("definitely-invalid"); err == nil {
		t.Error("expected error for invalid value")
	}
}

func TestParseMemoryType(t *testing.T) {
	runParseTest(t, ParseMemoryType, []string{"episodic", "semantic", "procedural"})
}

func TestParseContentFormat(t *testing.T) {
	runParseTest(t, ParseContentFormat, []string{"markdown", "plain", "json"})
}

func TestParseOrigin(t *testing.T) {
	runParseTest(t, ParseOrigin,
		[]string{"agent_observation", "user_instruction", "file_artifact", "consolidation"})
}

func TestParseAccessScope(t *testing.T) {
	runParseTest(t, ParseAccessScope, []string{"individual", "shared"})
}

func TestParseRelationshipType(t *testing.T) {
	runParseTest(t, ParseRelationshipType,
		[]string{"derived_from", "supersedes", "similar_to", "co_occurs_with", "causal_next", "anchors_entity"})
}

func TestParseEntityType(t *testing.T) {
	runParseTest(t, ParseEntityType,
		[]string{"person", "project", "repository", "tool", "organization", "concept"})
}

func TestParsePrincipalType(t *testing.T) {
	runParseTest(t, ParsePrincipalType, []string{"user", "agent", "session", "group"})
}

func TestParseAgentType(t *testing.T) {
	// claude-code with a hyphen, per pgvector-data-model §3.2 CHECK.
	runParseTest(t, ParseAgentType, []string{"claude-code", "codex", "cursor", "letta", "custom"})
}

func TestParseTriggerType(t *testing.T) {
	runParseTest(t, ParseTriggerType,
		[]string{"task_context", "user_query", "temporal", "associative", "session_start"})
}

func TestParseStrategyType(t *testing.T) {
	runParseTest(t, ParseStrategyType, []string{"vector", "bm25", "graph", "temporal"})
}

func TestParseRerankType(t *testing.T) {
	runParseTest(t, ParseRerankType, []string{"cross_encoder", "none"})
}

func TestParseRecallMode(t *testing.T) {
	runParseTest(t, ParseRecallMode, []string{"sync", "async"})
}

func TestParseJobKind(t *testing.T) {
	runParseTest(t, ParseJobKind, []string{"consolidation", "decay", "space_sync", "session_end"})
}

func TestParseJobStatus(t *testing.T) {
	runParseTest(t, ParseJobStatus, []string{"queued", "running", "completed", "failed"})
}

func TestParseProposalStatus(t *testing.T) {
	runParseTest(t, ParseProposalStatus, []string{"draft", "in_review", "merged", "rejected"})
}

func TestParseProposalTargetKind(t *testing.T) {
	runParseTest(t, ParseProposalTargetKind, []string{"spec", "rule", "agent_doc", "memory_doc"})
}

func TestParseSyncStatus(t *testing.T) {
	runParseTest(t, ParseSyncStatus, []string{"in_sync", "pending_review", "diverged", "offline"})
}

func TestParseStorageBackendType(t *testing.T) {
	runParseTest(t, ParseStorageBackendType, []string{"files", "relational", "vector", "graph", "hybrid"})
}

func TestParseAccessType(t *testing.T) {
	runParseTest(t, ParseAccessType, []string{"recall", "direct_get", "session_activate"})
}

func TestEnumValuesMatchDataModel(t *testing.T) {
	// Spot-check the exact wire values against pgvector-data-model CHECKs.
	assert.Equal(t, "individual", string(AccessScopeIndividual))
	assert.Equal(t, "bm25", string(StrategyBM25), "sparse path is bm25, not fulltext")
	assert.Equal(t, "claude-code", string(AgentClaudeCode), "hyphenated per DDL")
	assert.Equal(t, "pending_review", string(SyncStatusPendingReview))
	assert.Equal(t, "session_end", string(JobSessionEnd))
	assert.Equal(t, "in_review", string(ProposalStatusInReview))
	assert.True(t, ProposalStatusMerged.Terminal())
	assert.True(t, ProposalStatusRejected.Terminal())
	assert.False(t, ProposalStatusDraft.Terminal())
}
