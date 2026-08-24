package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// EmbeddingModel is a registry row tracking which model produced vectors,
// its dimensionality, and distance metric (review F11). dims must match one
// of the typed columns in memory_embeddings (currently 1536 or 768).
type EmbeddingModel struct {
	ModelID        string         `json:"model_id" validate:"required"`
	Provider       string         `json:"provider" validate:"required"`
	Dimensions     int            `json:"dims" validate:"required,min=1"`
	DistanceMetric DistanceMetric `json:"distance_metric"` // default cosine
	IsActive       bool           `json:"is_active"`
	CreatedAt      time.Time      `json:"created_at"`
}

// SupportedDimensions are the typed pgvector columns available for
// alternate-model vectors (pgvector indexes require a fixed typmod).
var SupportedDimensions = []int{1536, 768}

// Validate checks required fields, metric enum, and supported dims.
func (m *EmbeddingModel) Validate() error {
	if m.ModelID == "" {
		return NewValidationError("EmbeddingModel.ModelID", m.ModelID, "non-empty")
	}
	if m.Provider == "" {
		return NewValidationError("EmbeddingModel.Provider", m.Provider, "non-empty")
	}
	if !m.DistanceMetric.Valid() {
		return NewValidationError("EmbeddingModel.DistanceMetric", m.DistanceMetric.String(),
			"cosine|l2|ip")
	}
	for _, d := range SupportedDimensions {
		if m.Dimensions == d {
			return nil
		}
	}
	return NewValidationError("EmbeddingModel.Dimensions", intString(m.Dimensions),
		"one of 1536, 768 (typed vector columns)")
}

// MemoryEmbedding stores an alternate-model vector for a memory. Exactly one
// of Vec1536/Vec768 must be populated, matching the model's registered dims.
type MemoryEmbedding struct {
	MemoryID  uuid.UUID        `json:"memory_id" validate:"required"`
	ModelID   string           `json:"model_id" validate:"required"`
	Vec1536   *pgvector.Vector `json:"-"`
	Vec768    *pgvector.Vector `json:"-"`
	CreatedAt time.Time        `json:"created_at"`
}

// Validate enforces the exactly-one-vector CHECK of memory_embeddings.
func (e *MemoryEmbedding) Validate() error {
	populated := 0
	if e.Vec1536 != nil {
		populated++
	}
	if e.Vec768 != nil {
		populated++
	}
	if populated != 1 {
		return NewValidationError("MemoryEmbedding", "vectors populated",
			"exactly one of vec_1536, vec_768 must be set")
	}
	return nil
}
