// Package hf implements port.MemoryEncoder by running Hugging Face
// sentence-transformers models directly in-process through ONNX Runtime —
// no Ollama install, no Python sidecar, no HTTP hop.
//
// The model directory must contain an ONNX export (model.onnx), a
// tokenizer vocabulary (vocab.txt), and optionally config.json /
// tokenizer_config.json (used for max sequence length). Any directory
// layout produced by `optimum-cli export` or downloaded from the HF Hub
// in ONNX format works.
package hf

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
	"github.com/pgvector/pgvector-go"
	"github.com/phanijapps/mneme/internal/adapter/encoder/text"
	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

const (
	// DefaultModel is the HF id for all-MiniLM-L6-v2 — small, fast, strong
	// for English sentence similarity.
	DefaultModel = "sentence-transformers/all-MiniLM-L6-v2"

	// DefaultDimensions is all-MiniLM-L6-v2's vector width.
	DefaultDimensions = 384

	// DefaultMaxLen caps tokenization when the model dir carries no
	// config.json (BERT-family models typically declare 512).
	DefaultMaxLen = 512

	// modelFile and vocabFile are read from ModelDir.
	modelFile = "model.onnx"
	vocabFile = "vocab.txt"

	inputIDsName  = "input_ids"
	attentionName = "attention_mask"
	tokenTypeName = "token_type_ids"

	// provider is the registry label for this adapter.
	provider = "huggingface"
)

// Config configures the ONNX-backed encoder.
type Config struct {
	// ModelDir is a local directory containing model.onnx and tokenizer
	// files (vocab.txt, config.json, ...), e.g. an optimum-cli export or
	// an ONNX snapshot from the HF Hub. Required.
	ModelDir string
	// Model is the HF model id recorded in the embedding model registry
	// (metadata only — inference uses ModelDir); defaults to DefaultModel.
	Model string
	// Dimensions is the expected vector width; 0 means infer from the
	// model id, falling back to DefaultDimensions.
	Dimensions int
	// ONNXLibPath optionally points at the onnxruntime shared library
	// (libonnxruntime.so). Empty uses onnxruntime_go's default discovery
	// (onnxruntime.so on the system library path).
	ONNXLibPath string
}

func (c *Config) modelDir() string {
	if c == nil || c.ModelDir == "" {
		return ""
	}
	return filepath.Clean(c.ModelDir)
}

func (c *Config) model() string {
	if c == nil || c.Model == "" {
		return DefaultModel
	}
	return c.Model
}

func (c *Config) dimensions() int {
	if c != nil && c.Dimensions > 0 {
		return c.Dimensions
	}
	return dimsForModel(c.model())
}

// dimsForModel maps common HF embedding ids to their hidden size; unknown
// ids get the MiniLM default.
func dimsForModel(id string) int {
	switch {
	case strings.Contains(id, "all-MiniLM-L6-v2"):
		return 384
	case strings.Contains(id, "all-mpnet-base-v2"):
		return 768
	case strings.Contains(id, "bge-small-en-v1.5"):
		return 384
	case strings.Contains(id, "bge-base-en-v1.5"):
		return 768
	case strings.Contains(id, "bge-large-en-v1.5"), strings.Contains(id, "bge-m3"):
		return 1024
	case strings.Contains(id, "e5-large"), strings.Contains(id, "gte-large"):
		return 1024
	case strings.Contains(id, "e5-base"), strings.Contains(id, "gte-base"):
		return 768
	case strings.Contains(id, "e5-small"), strings.Contains(id, "gte-small"):
		return 384
	default:
		return DefaultDimensions
	}
}

// ONNX Runtime keeps process-global state: the environment initializes
// once and the shared-library path only takes effect on that first call.
var (
	onnxInitOnce sync.Once
	onnxInitErr  error
	onnxLibPath  string
)

func initONNX(libPath string) error {
	onnxInitOnce.Do(func() {
		onnxLibPath = libPath
		if libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		}
		if err := ort.InitializeEnvironment(); err != nil {
			onnxInitErr = fmt.Errorf("init onnxruntime: %w", err)
		}
	})
	if libPath != "" && libPath != onnxLibPath {
		return fmt.Errorf("init onnxruntime: library path %q conflicts with already-initialized %q", libPath, onnxLibPath)
	}
	return onnxInitErr
}

// Encoder computes sentence-transformers embeddings via a local ONNX model,
// pairing them with the shared text heuristics for type classification and
// entity extraction. A BERT-style WordPiece tokenizer runs in-process; the
// hidden states are mean-pooled over the attention mask and L2-normalized —
// the sentence-transformers pooling recipe.
type Encoder struct {
	cfg           Config
	vocab         *wordPiece
	maxLen        int
	session       *ort.DynamicAdvancedSession
	sessionInputs []string
	wantTokenType bool

	// mu serializes session.Run; this wrapper does not document
	// re-entrancy, so play it safe rather than race it.
	mu sync.Mutex
}

// compile-time interface compliance check.
var _ port.MemoryEncoder = (*Encoder)(nil)

// New loads the tokenizer and ONNX session eagerly so wiring mistakes fail
// fast at startup rather than surfacing at first Encode.
func New(cfg Config) (*Encoder, error) {
	dir := cfg.modelDir()
	if dir == "" {
		return nil, fmt.Errorf("hf: onnx: ModelDir is required (a directory containing %s)", modelFile)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("hf: onnx: model dir %s not found", dir)
	}
	modelPath := filepath.Join(dir, modelFile)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("hf: onnx: %s missing from model dir %s", modelFile, dir)
	}

	vocab, err := loadWordPiece(filepath.Join(dir, vocabFile))
	if err != nil {
		return nil, fmt.Errorf("hf: onnx: %w", err)
	}
	maxLen, err := loadMaxLen(dir)
	if err != nil {
		return nil, fmt.Errorf("hf: onnx: %w", err)
	}
	if err := initONNX(cfg.ONNXLibPath); err != nil {
		return nil, fmt.Errorf("hf: onnx: %w", err)
	}

	ins, outs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return nil, fmt.Errorf("hf: onnx: inspect %s: %w", modelFile, err)
	}
	if len(ins) == 0 || len(outs) == 0 {
		return nil, fmt.Errorf("hf: onnx: %s exposes no usable inputs/outputs", modelFile)
	}
	var inputNames []string
	var hasTokenType bool
	for _, in := range ins {
		inputNames = append(inputNames, in.Name)
		if in.Name == tokenTypeName {
			hasTokenType = true
		}
	}
	outputNames := make([]string, len(outs))
	for i, out := range outs {
		outputNames[i] = out.Name
	}

	sess, err := ort.NewDynamicAdvancedSession(modelPath, inputNames, outputNames, nil)
	if err != nil {
		return nil, fmt.Errorf("hf: onnx: load session: %w", err)
	}
	return &Encoder{
		cfg:           cfg,
		vocab:         vocab,
		maxLen:        maxLen,
		session:       sess,
		sessionInputs: inputNames,
		wantTokenType: hasTokenType,
	}, nil
}

// Close releases the ONNX session. The process-global ORT environment
// stays alive for other sessions.
func (e *Encoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != nil {
		if err := e.session.Destroy(); err != nil {
			return fmt.Errorf("hf: onnx: destroy session: %w", err)
		}
		e.session = nil
	}
	return nil
}

// Encode classifies, extracts entities, embeds, and returns the ingestion
// artifact for one content string.
func (e *Encoder) Encode(ctx context.Context, content string, opts port.EncodeOptions) (port.EncodedMemory, error) {
	if err := validateContent(content); err != nil {
		return port.EncodedMemory{}, err
	}
	vecs, err := e.embed(ctx, []string{content})
	if err != nil {
		return port.EncodedMemory{}, err
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
		return nil, fmt.Errorf("hf: empty batch")
	}
	for i, c := range contents {
		if err := validateContent(c); err != nil {
			return nil, fmt.Errorf("hf: batch item %d: %w", i, err)
		}
	}
	vecs, err := e.embed(ctx, contents)
	if err != nil {
		return nil, err
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

// ListModels returns the locally loaded model as its registry row; ONNX
// inference is local, so one Encoder serves exactly one model.
func (e *Encoder) ListModels(ctx context.Context) ([]*domain.EmbeddingModel, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("hf: context cancelled: %w", err)
	}
	return []*domain.EmbeddingModel{{
		ModelID:        e.cfg.model(),
		Provider:       provider,
		Dimensions:     e.cfg.dimensions(),
		DistanceMetric: domain.MetricCosine,
		IsActive:       true,
	}}, nil
}

// validateContent rejects blank input before any tensor work.
func validateContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("hf: encode of empty content")
	}
	return nil
}

// embed tokenizes, runs one padded batch through the session, mean-pools,
// and normalizes. Returns one vector per input, in order.
func (e *Encoder) embed(ctx context.Context, contents []string) ([]pgvector.Vector, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("hf: context cancelled: %w", err)
	}
	rows, seq := e.vocab.encodeBatch(contents, e.maxLen)

	ids := make([]int64, 0, len(contents)*seq)
	mask := make([]int64, 0, len(contents)*seq)
	for _, row := range rows {
		padded := make([]int64, seq)
		copy(padded, row.ids)
		ids = append(ids, padded...)
		m := make([]int64, seq)
		for i := range row.ids {
			m[i] = 1
		}
		mask = append(mask, m...)
	}

	shape := ort.NewShape(int64(len(contents)), int64(seq))
	inputs := make([]ort.Value, 0, 3)
	idsTensor, err := ort.NewTensor(shape, ids)
	if err != nil {
		return nil, fmt.Errorf("hf: onnx: input_ids tensor: %w", err)
	}
	defer idsTensor.Destroy()
	inputs = append(inputs, idsTensor)

	maskTensor, err := ort.NewTensor(shape, mask)
	if err != nil {
		return nil, fmt.Errorf("hf: onnx: attention_mask tensor: %w", err)
	}
	defer maskTensor.Destroy()
	inputs = append(inputs, maskTensor)

	if e.wantTokenType {
		ttTensor, err := ort.NewTensor(shape, make([]int64, len(contents)*seq))
		if err != nil {
			return nil, fmt.Errorf("hf: onnx: token_type_ids tensor: %w", err)
		}
		defer ttTensor.Destroy()
		inputs = append(inputs, ttTensor)
	}
	if len(inputs) != len(e.sessionInputs) {
		return nil, fmt.Errorf("hf: onnx: session wants %d inputs (%s), built %d",
			len(e.sessionInputs), strings.Join(e.sessionInputs, ", "), len(inputs))
	}

	e.mu.Lock()
	outputs := []ort.Value{nil}
	runErr := e.session.Run(inputs, outputs)
	e.mu.Unlock()
	if runErr != nil {
		return nil, fmt.Errorf("hf: onnx: run: %w", runErr)
	}
	outTensor := outputs[0]

	hidden, ok := outTensor.(*ort.Tensor[float32])
	if !ok {
		outTensor.Destroy()
		return nil, fmt.Errorf("hf: onnx: unexpected output type %T (want float32 tensor)", outTensor)
	}
	defer hidden.Destroy()

	data := hidden.GetData()
	hShape := hidden.GetShape()
	if len(hShape) != 3 {
		return nil, fmt.Errorf("hf: onnx: last_hidden_state rank %d, want 3", len(hShape))
	}
	hiddenDim := int(hShape[2])
	if hiddenDim == 0 {
		return nil, fmt.Errorf("hf: onnx: zero hidden dimension")
	}
	if want := len(contents) * seq * hiddenDim; len(data) < want {
		return nil, fmt.Errorf("hf: onnx: output %d floats, want at least %d", len(data), want)
	}

	vecs := make([]pgvector.Vector, len(contents))
	for i := range contents {
		start := i * seq * hiddenDim
		vecs[i] = poolTokens(data[start:start+seq*hiddenDim], rows[i].ids, seq, hiddenDim)
	}
	return vecs, nil
}

// poolTokens mean-pools one row of hidden states over its real (unpadded)
// tokens, then L2-normalizes — the sentence-transformers default pooling.
func poolTokens(hidden []float32, realIDs []int64, seq, hiddenDim int) pgvector.Vector {
	sum := make([]float64, hiddenDim)
	var weight float64
	for t := 0; t < seq && t < len(realIDs); t++ {
		weight++
		base := t * hiddenDim
		for d := 0; d < hiddenDim; d++ {
			sum[d] += float64(hidden[base+d])
		}
	}
	vec := make([]float32, hiddenDim)
	if weight > 0 {
		for d := range sum {
			vec[d] = float32(sum[d] / weight)
		}
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for d := range vec {
			vec[d] = float32(float64(vec[d]) / norm)
		}
	}
	return pgvector.NewVector(vec)
}

// classify honors an explicit TypeHint before falling back to heuristics.
func (e *Encoder) classify(opts port.EncodeOptions, content string) domain.MemoryType {
	if opts.TypeHint != "" && opts.TypeHint.Valid() {
		return opts.TypeHint
	}
	return text.Classify(content)
}

// modelLenConfig carries just the sequence-length fields mneme needs.
type modelLenConfig struct {
	MaxPositionEmbeddings int `json:"max_position_embeddings"`
	ModelMaxLength        int `json:"model_max_length"`
}

// loadMaxLen reads max sequence length from tokenizer_config.json, then
// config.json, defaulting to DefaultMaxLen when neither declares one.
func loadMaxLen(dir string) (int, error) {
	for _, name := range []string{"tokenizer_config.json", "config.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("read %s: %w", name, err)
		}
		var c modelLenConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return 0, fmt.Errorf("parse %s: %w", name, err)
		}
		if n := c.ModelMaxLength; n > 0 {
			return n, nil
		}
		if n := c.MaxPositionEmbeddings; n > 0 {
			return n, nil
		}
	}
	return DefaultMaxLen, nil
}

// loadWordPiece reads a BERT-style vocab.txt (one token per line) and
// returns a tokenizer bound to it.
func loadWordPiece(path string) (*wordPiece, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("vocab: %w", err)
	}
	defer f.Close()
	vocab := make(map[string]int)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	id := 0
	for scanner.Scan() {
		token := strings.TrimSuffix(scanner.Text(), "\r")
		if _, dup := vocab[token]; !dup {
			vocab[token] = id
		}
		id++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("vocab: %w", err)
	}
	if len(vocab) == 0 {
		return nil, fmt.Errorf("vocab: %s is empty", path)
	}
	return newWordPiece(vocab), nil
}
