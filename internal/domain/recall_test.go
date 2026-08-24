package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validRecallRequest() *RecallRequest {
	return &RecallRequest{
		RequestID: uuid.New(),
		Query:     "how do we index vectors?",
		Trigger:   TriggerUserQuery,
		AgentID:   "claude-code",
		SessionID: uuid.New(),
		RetrievalParams: RetrievalParams{
			Strategies: []StrategyType{StrategyVector, StrategyBM25},
			TopK:       50,
			Rerank:     RerankCrossEncoder,
			MinScore:   0.35,
		},
		Mode:        RecallModeSync,
		Status:      RecallStatusCompleted,
		RequestedAt: time.Now(),
	}
}

func TestRecallRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*RecallRequest)
		wantErr bool
	}{
		{"valid", func(*RecallRequest) {}, false},
		{"empty query", func(r *RecallRequest) { r.Query = "" }, true},
		{"invalid trigger", func(r *RecallRequest) { r.Trigger = TriggerType("spontaneous") }, true},
		{"missing agent", func(r *RecallRequest) { r.AgentID = "" }, true},
		{"no strategies", func(r *RecallRequest) { r.RetrievalParams.Strategies = nil }, true},
		{"invalid strategy", func(r *RecallRequest) {
			r.RetrievalParams.Strategies = []StrategyType{StrategyType("fts")}
		}, true},
		{"top_k zero", func(r *RecallRequest) { r.RetrievalParams.TopK = 0 }, true},
		{"top_k over 200", func(r *RecallRequest) { r.RetrievalParams.TopK = 201 }, true},
		{"top_k 200 boundary ok", func(r *RecallRequest) { r.RetrievalParams.TopK = 200 }, false},
		{"invalid rerank", func(r *RecallRequest) { r.RetrievalParams.Rerank = RerankType("bm25") }, true},
		{"min_score over 1", func(r *RecallRequest) { r.RetrievalParams.MinScore = 1.1 }, true},
		{"negative slot budget", func(r *RecallRequest) { r.RetrievalParams.SlotBudget = ptrInt(-1) }, true},
		{"negative token budget", func(r *RecallRequest) { r.RetrievalParams.TokenBudget = ptrInt(-5) }, true},
		{"zero budgets ok", func(r *RecallRequest) {
			r.RetrievalParams.SlotBudget = ptrInt(0)
			r.RetrievalParams.TokenBudget = ptrInt(0)
		}, false},
		{"invalid mode", func(r *RecallRequest) { r.Mode = RecallMode("batch") }, true},
		{"failed without error", func(r *RecallRequest) { r.Status = RecallStatusFailed }, true},
		{"failed with error ok", func(r *RecallRequest) {
			r.Status = RecallStatusFailed
			r.Error = &Error{Code: CodeInternal, Message: "boom"}
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validRecallRequest()
			tt.mutate(r)
			err := r.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, CodeValidationErr, err.(*Error).Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func ptrInt(i int) *int { return &i }

func TestLifecycleJobValidation(t *testing.T) {
	newJob := func() *LifecycleJob {
		return &LifecycleJob{JobID: uuid.New(), Kind: JobConsolidation,
			Status: JobStatusRunning, CreatedAt: time.Now()}
	}

	require.NoError(t, newJob().Validate())

	completed := newJob()
	completed.Status = JobStatusCompleted
	require.Error(t, completed.Validate(), "terminal without finished_at")

	finished := time.Now()
	completed.FinishedAt = &finished
	require.NoError(t, completed.Validate())

	failed := newJob()
	failed.Status = JobStatusFailed
	failed.FinishedAt = &finished
	require.Error(t, failed.Validate(), "failed without error")

	failed.Error = &Error{Code: CodeInternal, Message: "boom"}
	require.NoError(t, failed.Validate())

	badKind := newJob()
	badKind.Kind = JobKind("garbage")
	require.Error(t, badKind.Validate())
}

func TestEmbeddingModelValidation(t *testing.T) {
	m := &EmbeddingModel{ModelID: "text-embedding-3-small", Provider: "openai",
		Dimensions: 1536, DistanceMetric: MetricCosine, IsActive: true}
	require.NoError(t, m.Validate())

	m.Dimensions = 3072 // no typed column yet
	require.Error(t, m.Validate())

	m.Dimensions = 768
	require.NoError(t, m.Validate())

	m.DistanceMetric = DistanceMetric("manhattan")
	require.Error(t, m.Validate())
}

func TestMemoryEmbeddingExactlyOneVector(t *testing.T) {
	zero := &MemoryEmbedding{MemoryID: uuid.New(), ModelID: "m"}
	require.Error(t, zero.Validate(), "no vector populated")

	vec := pgvector.NewVector(nil)
	one := &MemoryEmbedding{MemoryID: uuid.New(), ModelID: "bge-base-en-v1.5", Vec768: &vec}
	require.NoError(t, one.Validate())

	one.Vec1536 = &vec
	require.Error(t, one.Validate(), "both vectors populated")
}

func TestMemoryAccessLogValidation(t *testing.T) {
	ok := &MemoryAccessLog{MemoryID: uuid.New(), AccessedBy: uuid.New(),
		AccessType: AccessTypeRecall, AccessedAt: time.Now()}
	require.NoError(t, ok.Validate())

	bad := &MemoryAccessLog{MemoryID: uuid.New(), AccessedBy: uuid.New(),
		AccessType: AccessType("write")}
	require.Error(t, bad.Validate())
}
