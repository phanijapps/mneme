// Package stub is an offline MemoryEncoder returning deterministic vectors.
// It exists so tests and local development can run the full write path
// without an embedding backend; vectors are hash-seeded per content, not
// semantically meaningful.
package stub

import (
	"context"
	"hash/fnv"
	"strings"

	"github.com/pgvector/pgvector-go"
	"github.com/phanijapps/mneme/internal/adapter/encoder/text"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// Dimensions of the stub vectors. Intentionally distinct from real models
// so stub-produced vectors are never confused with production ones.
const Dimensions = 8

// compile-time interface compliance check.
var _ port.MemoryEncoder = (*Encoder)(nil)

// Encoder is the stub implementation.
type Encoder struct{}

// New returns a stub encoder.
func New() *Encoder { return &Encoder{} }

// Encode returns a deterministic 8-dim vector seeded by content, plus the
// shared heuristics for type and entities.
func (e *Encoder) Encode(_ context.Context, content string, opts port.EncodeOptions) (port.EncodedMemory, error) {
	if strings.TrimSpace(content) == "" {
		return port.EncodedMemory{}, errEmpty()
	}
	return port.EncodedMemory{
		Vector:          vectorFor(content),
		InferredType:    classify(opts, content),
		Entities:        text.ExtractEntities(content),
		EstimatedTokens: text.EstimateTokens(content),
	}, nil
}

// EncodeBatch encodes each item, preserving order.
func (e *Encoder) EncodeBatch(_ context.Context, contents []string, opts port.EncodeOptions) ([]port.EncodedMemory, error) {
	out := make([]port.EncodedMemory, len(contents))
	for i, c := range contents {
		em, err := e.Encode(context.Background(), c, opts)
		if err != nil {
			return nil, err
		}
		out[i] = em
	}
	return out, nil
}

// ListModels reports only the stub model.
func (e *Encoder) ListModels(_ context.Context) ([]*domain.EmbeddingModel, error) {
	return []*domain.EmbeddingModel{{
		ModelID:        "stub-encoder",
		Provider:       "stub",
		Dimensions:     Dimensions,
		DistanceMetric: domain.MetricCosine,
		IsActive:       true,
	}}, nil
}

// vectorFor derives a stable 8-dim vector from content via FNV-1a.
func vectorFor(content string) pgvector.Vector {
	h := fnv.New64a()
	_, _ = h.Write([]byte(content))
	seed := h.Sum64()
	vec := make([]float32, Dimensions)
	for i := range vec {
		// Bit-rotate the seed per dimension; values in [-1, 1).
		seed = seed*6364136223846793005 + 1442695040888963407
		vec[i] = float32(int64(seed%2001)-1000) / 1000.0
	}
	return pgvector.NewVector(vec)
}

// classify honors TypeHint before heuristic classification.
func classify(opts port.EncodeOptions, content string) domain.MemoryType {
	if opts.TypeHint != "" && opts.TypeHint.Valid() {
		return opts.TypeHint
	}
	return text.Classify(content)
}

// errEmpty is the empty-content sentinel.
func errEmpty() error { return &EmptyContentError{} }

// EmptyContentError reports an attempt to encode "".
type EmptyContentError struct{}

func (e *EmptyContentError) Error() string { return "stub: encode of empty content" }
