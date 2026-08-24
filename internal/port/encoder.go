package port

import (
	"context"

	"github.com/pgvector/pgvector-go"
	"github.com/phanijapps/mneme/internal/domain"
)

// EncodeOptions parameterizes the ingestion pipeline for one memory.
type EncodeOptions struct {
	// Model selects the embedding model by registry id; empty means the
	// active default model.
	Model string
	// TypeHint optionally pins the memory type; when empty the encoder
	// classifies it from content.
	TypeHint domain.MemoryType
}

// EncodedMemory is the full ingestion artifact of POST /memories: the vector,
// the inferred-or-hinted type, and the entities extracted from content.
type EncodedMemory struct {
	Vector          pgvector.Vector
	InferredType    domain.MemoryType
	Entities        []domain.Entity
	EstimatedTokens int
}

// MemoryEncoder realizes the encode step of the write path: type
// classification, entity extraction, and embedding computation against a
// pluggable model backend (architecture.md §2.2).
type MemoryEncoder interface {
	// Encode classifies, extracts, and embeds a single content string.
	Encode(ctx context.Context, content string, opts EncodeOptions) (EncodedMemory, error)
	// EncodeBatch encodes many strings under one options set, preserving
	// input order in the result slice.
	EncodeBatch(ctx context.Context, contents []string, opts EncodeOptions) ([]EncodedMemory, error)
	// ListModels exposes the embedding model registry for selection.
	ListModels(ctx context.Context) ([]*domain.EmbeddingModel, error)
}
