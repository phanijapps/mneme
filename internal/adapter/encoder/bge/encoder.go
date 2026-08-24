// Package bge implements port.MemoryEncoder against a local Ollama instance
// running the BAAI/bge-small-en-v1.5 embedding model (384 dims).
//
// Ollama exposes bge-small-en-v1.5 natively; embeddings are computed over
// HTTP so the service stays free of CGO/ONNX dependencies. The model must be
// pulled once: `ollama pull bge-small-en-v1.5`.
package bge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/phanijapps/mneme/internal/adapter/encoder/text"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

const (
	// DefaultModel is the Ollama tag for BAAI/bge-small-en-v1.5.
	DefaultModel = "bge-small-en-v1.5"

	// DefaultDimensions is bge-small's vector width.
	DefaultDimensions = 384

	// DefaultBaseURL is the standard local Ollama endpoint.
	DefaultBaseURL = "http://localhost:11434"

	embedPath   = "/api/embed"
	listPath    = "/api/tags"
	contentType = "application/json"
)

// Config configures the Ollama-backed encoder.
type Config struct {
	// BaseURL of the Ollama server; defaults to DefaultBaseURL. The
	// OLLAMA_BASE_URL environment variable overrides the zero value.
	BaseURL string
	// Model is the Ollama embedding tag; defaults to DefaultModel.
	Model string
	// HTTPClient used for requests; nil means http.DefaultClient with a
	// sane timeout.
	HTTPClient *http.Client
	// Provider label recorded in the embedding_models registry.
	Provider string
}

func (c *Config) baseURL() string {
	if c == nil || c.BaseURL == "" {
		return DefaultBaseURL
	}
	return strings.TrimSuffix(c.BaseURL, "/")
}

func (c *Config) model() string {
	if c == nil || c.Model == "" {
		return DefaultModel
	}
	return c.Model
}

func (c *Config) httpClient() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Config) provider() string {
	if c != nil && c.Provider != "" {
		return c.Provider
	}
	return "ollama"
}

// Encoder computes BGE-small embeddings via Ollama, pairing them with the
// shared text heuristics for type classification and entity extraction.
type Encoder struct {
	cfg  Config
	client *http.Client

	mu     sync.RWMutex
	models []*domain.EmbeddingModel
}

// compile-time interface compliance check.
var _ port.MemoryEncoder = (*Encoder)(nil)

// New builds an Encoder from cfg. It does not dial Ollama; connection
// problems surface on first Encode/ListModels call.
func New(cfg Config) *Encoder {
	return &Encoder{cfg: cfg, client: cfg.httpClient()}
}

// embedRequest / embedResponse model the Ollama /api/embed contract.
type embedRequest struct {
	Model string `json:"model"`
	Input []any  `json:"input"` // string or []string; keep `any` for the docs contract
}

type embedResponse struct {
	Model          string      `json:"model"`
	Embeddings     [][]float64 `json:"embeddings"`
	TotalDuration  int64       `json:"total_duration,omitempty"`
	LoadDuration   int64       `json:"load_duration,omitempty"`
	PromptEvalCount int         `json:"prompt_eval_count,omitempty"`
}

// tagsResponse models the Ollama /api/tags contract.
type tagsResponse struct {
	Models []struct {
		Name       string    `json:"name"`
		Model      string    `json:"model"`
		ModifiedAt time.Time `json:"modified_at"`
		Details    struct {
			Family            string `json:"family"`
			Families          []string `json:"families"`
			ParameterSize     string `json:"parameter_size"`
			QuantizationLevel string `json:"quantization_level"`
		} `json:"details"`
	} `json:"models"`
}

// embeddingModelFamilies are the Ollama model families that produce
// embeddings (rather than generate tokens).
var embeddingModelFamilies = map[string]bool{
	"bert":       true,
	"nomic-bert": true,
	"jina-bert-v2": true,
}

// dimsOverride lets tests pin a model's dimension without a live Ollama.
var dimsOverride = map[string]int{}

// Encode classifies, extracts entities, embeds, and returns the ingestion
// artifact for one content string.
func (e *Encoder) Encode(ctx context.Context, content string, opts port.EncodeOptions) (port.EncodedMemory, error) {
	if strings.TrimSpace(content) == "" {
		return port.EncodedMemory{}, fmt.Errorf("bge: encode of empty content")
	}
	vecs, err := e.embed(ctx, []string{content}, opts.Model)
	if err != nil {
		return port.EncodedMemory{}, err
	}
	if len(vecs) != 1 {
		return port.EncodedMemory{}, fmt.Errorf("bge: expected 1 embedding, got %d", len(vecs))
	}
	return port.EncodedMemory{
		Vector:          vecs[0],
		InferredType:    e.classify(opts, content),
		Entities:        text.ExtractEntities(content),
		EstimatedTokens: text.EstimateTokens(content),
	}, nil
}

// EncodeBatch encodes many strings preserving input order.
func (e *Encoder) EncodeBatch(ctx context.Context, contents []string, opts port.EncodeOptions) ([]port.EncodedMemory, error) {
	if len(contents) == 0 {
		return nil, fmt.Errorf("bge: empty batch")
	}
	for i, c := range contents {
		if strings.TrimSpace(c) == "" {
			return nil, fmt.Errorf("bge: batch item %d is empty", i)
		}
	}
	vecs, err := e.embed(ctx, contents, opts.Model)
	if err != nil {
		return nil, err
	}
	if len(vecs) != len(contents) {
		return nil, fmt.Errorf("bge: expected %d embeddings, got %d", len(contents), len(vecs))
	}
	out := make([]port.EncodedMemory, len(contents))
	for i, c := range contents {
		out[i] = port.EncodedMemory{
			Vector:          vecs[i],
			InferredType:    e.classify(opts, c),
			Entities:        text.ExtractEntities(c),
			EstimatedTokens: text.EstimateTokens(c),
		}
	}
	return out, nil
}

// ListModels queries Ollama for installed models, filtered to embedding
// families, and returns them as registry rows.
func (e *Encoder) ListModels(ctx context.Context) ([]*domain.EmbeddingModel, error) {
	body, err := e.get(ctx, listPath)
	if err != nil {
		return nil, fmt.Errorf("bge: list models: %w", err)
	}
	var tags tagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("bge: decode /api/tags: %w", err)
	}
	var out []*domain.EmbeddingModel
	for _, m := range tags.Models {
		// Registry ids are bare model names; drop any ":tag" suffix.
		id := m.Name
		if i := strings.Index(id, ":"); i > 0 {
			id = id[:i]
		}
		if !e.isEmbeddingModel(id, m.Details.Families) {
			continue
		}
		out = append(out, &domain.EmbeddingModel{
			ModelID:        id,
			Provider:       e.cfg.provider(),
			Dimensions:     e.dims(id),
			DistanceMetric: domain.MetricCosine,
			IsActive:       id == e.cfg.model(),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bge: no embedding models installed (try: ollama pull %s)", DefaultModel)
	}
	return out, nil
}

// activeModel reports whether name is the configured active model.
func (e *Encoder) activeModel(name string) bool { return name == e.cfg.model() }

// isEmbeddingModel matches either the family list or a known tag prefix.
func (e *Encoder) isEmbeddingModel(name string, families []string) bool {
	for _, f := range families {
		if embeddingModelFamilies[f] {
			return true
		}
	}
	for _, p := range knownEmbeddingTags {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// knownEmbeddingTags are Ollama tags that are embedding models regardless of
// reported family (the tags endpoint is not always populated).
var knownEmbeddingTags = []string{
	"bge-", "nomic-embed", "mxbai-embed", "all-minilm", "snowflake-arctic-embed",
	"paraphrase-", "gemma-embed", "jina-embed",
}

// dims returns the dimension for an Ollama model tag; unknown tags default
// to bge-small's 384.
func (e *Encoder) dims(name string) int {
	if d, ok := dimsOverride[name]; ok {
		return d
	}
	switch {
	case strings.Contains(name, "bge-m3"):
		return 1024
	case strings.HasPrefix(name, "bge-large"):
		return 1024
	case strings.HasPrefix(name, "bge-base"):
		return 768
	case strings.HasPrefix(name, "bge-"):
		return 384
	case strings.HasPrefix(name, "nomic-embed-text"):
		return 768
	case strings.HasPrefix(name, "mxbai-embed-large"):
		return 1024
	case strings.HasPrefix(name, "all-minilm"):
		return 384
	case strings.HasPrefix(name, "snowflake-arctic-embed"):
		return 1024
	default:
		return DefaultDimensions
	}
}

// embed posts one batch to /api/embed and returns vectors in input order.
func (e *Encoder) embed(ctx context.Context, contents []string, model string) ([]pgvector.Vector, error) {
	if model == "" {
		model = e.cfg.model()
	}
	input := make([]any, len(contents))
	for i, c := range contents {
		input[i] = c
	}
	raw, err := json.Marshal(embedRequest{Model: model, Input: input})
	if err != nil {
		return nil, fmt.Errorf("bge: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.baseURL()+embedPath, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("bge: build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bge: ollama %s unreachable: %w", e.cfg.baseURL(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("bge: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bge: ollama returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var er embedResponse
	if err := json.Unmarshal(body, &er); err != nil {
		return nil, fmt.Errorf("bge: decode /api/embed: %w", err)
	}
	vecs := make([]pgvector.Vector, len(er.Embeddings))
	for i, dims := range er.Embeddings {
		fs := make([]float32, len(dims))
		for j, d := range dims {
			fs[j] = float32(d)
		}
		vecs[i] = pgvector.NewVector(fs)
	}
	return vecs, nil
}

// classify honors an explicit TypeHint before falling back to heuristics.
func (e *Encoder) classify(opts port.EncodeOptions, content string) domain.MemoryType {
	if opts.TypeHint != "" && opts.TypeHint.Valid() {
		return opts.TypeHint
	}
	return text.Classify(content)
}

// get performs a GET against the Ollama server.
func (e *Encoder) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.cfg.baseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bge: ollama %s unreachable: %w", e.cfg.baseURL(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}
