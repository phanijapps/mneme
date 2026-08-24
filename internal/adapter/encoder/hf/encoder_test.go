package hf

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// miniVocab is a tiny BERT-style vocab: specials, a few words, and ##X
// continuations so subword decomposition is observable.
func miniVocab() map[string]int {
	tokens := []string{
		"[PAD]", "[UNK]", "[CLS]", "[SEP]",
		"hello", "world", "play", "##ing",
		"un", "##believ", "##able", "postgres", "uses", "hnsw", "indexes",
	}
	v := make(map[string]int, len(tokens))
	for i, t := range tokens {
		v[t] = i
	}
	return v
}

func TestTokenizerBasic(t *testing.T) {
	w := newWordPiece(miniVocab())
	v := miniVocab()
	ids := w.Encode("hello world", 512)
	want := []int64{int64(v["[CLS]"]), int64(v["hello"]), int64(v["world"]), int64(v["[SEP]"])}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids[%d] = %d, want %d", i, ids[i], want[i])
		}
	}
}

func TestTokenizerLowercasesAndSplitsPunctuation(t *testing.T) {
	w := newWordPiece(miniVocab())
	v := miniVocab()
	ids := w.Encode("Hello!", 512)
	want := []int64{int64(v["[CLS]"]), int64(v["hello"]), int64(v["[UNK]"]), int64(v["[SEP]"])}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v (lowercased + punct isolated)", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids[%d] = %d, want %d", i, ids[i], want[i])
		}
	}
}

func TestTokenizerSubwords(t *testing.T) {
	w := newWordPiece(miniVocab())
	v := miniVocab()
	ids := w.Encode("playing", 512)
	want := []int64{int64(v["[CLS]"]), int64(v["play"]), int64(v["##ing"]), int64(v["[SEP]"])}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v (play + ##ing)", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids[%d] = %d, want %d", i, ids[i], want[i])
		}
	}
}

func TestTokenizerUnknownWordIsUNK(t *testing.T) {
	w := newWordPiece(miniVocab())
	v := miniVocab()
	ids := w.Encode("qqqzz", 512)
	if len(ids) != 3 || ids[1] != int64(v["[UNK]"]) {
		t.Fatalf("ids = %v, want [CLS] [UNK] [SEP]", ids)
	}
}

func TestTokenizerTruncation(t *testing.T) {
	w := newWordPiece(miniVocab())
	ids := w.Encode("hello world hello world hello world", 6)
	if len(ids) > 6 {
		t.Fatalf("len = %d, want <= maxLen 6", len(ids))
	}
	if ids[len(ids)-1] == ids[0] {
		t.Fatalf("last token should be [SEP]-capped, got %v", ids)
	}
}

func TestTokenizerEmptyText(t *testing.T) {
	w := newWordPiece(miniVocab())
	ids := w.Encode("   ", 512)
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want [CLS] [SEP]", ids)
	}
}

func TestEncodeBatchPadsToMaxRow(t *testing.T) {
	w := newWordPiece(miniVocab())
	rows, seq := w.encodeBatch([]string{"hello world", "play"}, 512)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// "hello world" = CLS hello world SEP = 4; padding target is 4.
	if seq != 4 {
		t.Fatalf("seq = %d, want 4", seq)
	}
	if len(rows[0].ids) != 4 || len(rows[1].ids) != 3 {
		t.Fatalf("row lens = %d, %d; want 4, 3", len(rows[0].ids), len(rows[1].ids))
	}
}

func TestMeanPoolAndNormalize(t *testing.T) {
	// 2 real tokens, hidden dim 2. Pool = mean of the two rows, L2-normalized.
	hidden := []float32{1, 0, 0, 2}
	got := poolTokens(hidden, []int64{1, 1}, 2, 2)
	vec := got.Slice()
	if len(vec) != 2 {
		t.Fatalf("len = %d, want 2", len(vec))
	}
	// mean = [0.5, 1]; normalized = [0.4472, 0.8944]
	if math.Abs(float64(vec[0])-0.4472136) > 1e-5 || math.Abs(float64(vec[1])-0.8944272) > 1e-5 {
		t.Fatalf("vec = %v, want [0.44721 0.89443]", vec)
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if math.Abs(norm-1.0) > 1e-5 {
		t.Fatalf("|vec|^2 = %f, want 1.0", norm)
	}
}

func TestMeanPoolIgnoresPaddedTokens(t *testing.T) {
	// seq=3 but only 1 real token id: pool over just that token.
	hidden := []float32{3, 4, 99, 99, 99, 99}
	got := poolTokens(hidden, []int64{7}, 3, 2)
	vec := got.Slice()
	// mean of single token [3,4] normalized -> [0.6, 0.8]
	if math.Abs(float64(vec[0])-0.6) > 1e-6 || math.Abs(float64(vec[1])-0.8) > 1e-6 {
		t.Fatalf("vec = %v, want [0.6 0.8]", vec)
	}
}

func TestPoolZeroWeightYieldsZeroVector(t *testing.T) {
	got := poolTokens([]float32{1, 2, 3, 4}, nil, 2, 2)
	for _, v := range got.Slice() {
		if v != 0 {
			t.Fatalf("vec = %v, want zeros", got.Slice())
		}
	}
}

func TestDimsForModel(t *testing.T) {
	cases := map[string]int{
		"sentence-transformers/all-MiniLM-L6-v2": 384,
		"sentence-transformers/all-mpnet-base-v2": 768,
		"BAAI/bge-small-en-v1.5":                 384,
		"BAAI/bge-large-en-v1.5":                 1024,
		"intfloat/multilingual-e5-large":         1024,
		"totally-unknown-model":                  DefaultDimensions,
	}
	for id, want := range cases {
		if got := dimsForModel(id); got != want {
			t.Errorf("dimsForModel(%q) = %d, want %d", id, got, want)
		}
	}
}

func TestConfigResolution(t *testing.T) {
	var nilCfg *Config
	if nilCfg.model() != DefaultModel {
		t.Fatal("nil cfg model default")
	}
	if nilCfg.modelDir() != "" {
		t.Fatal("nil cfg modelDir")
	}
	c := &Config{Model: "BAAI/bge-large-en-v1.5"}
	if c.dimensions() != 1024 {
		t.Fatalf("dims = %d, want 1024 (inferred)", c.dimensions())
	}
	c2 := &Config{Dimensions: 768}
	if c2.dimensions() != 768 {
		t.Fatalf("dims = %d, want 768 (explicit)", c2.dimensions())
	}
}

// writeModelDir creates a minimal model directory for New() validation
// tests. The .onnx is intentionally garbage — enough to pass Stat but
// never loadable — unless valid is true.
func writeModelDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestNewRequiresModelDir(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for empty ModelDir")
	}
	if _, err := New(Config{ModelDir: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestNewRequiresOnnxFile(t *testing.T) {
	dir := writeModelDir(t, map[string]string{"vocab.txt": "[PAD]\n[UNK]\n[CLS]\n[SEP]\nhello\n"})
	if _, err := New(Config{ModelDir: dir}); err == nil {
		t.Fatal("expected error for missing model.onnx")
	}
}

func TestNewRequiresVocab(t *testing.T) {
	dir := writeModelDir(t, map[string]string{"model.onnx": "\x00garbage"})
	if _, err := New(Config{ModelDir: dir}); err == nil {
		t.Fatal("expected error for missing vocab.txt")
	}
}

func TestLoadMaxLenDefaults(t *testing.T) {
	dir := t.TempDir()
	n, err := loadMaxLen(dir)
	if err != nil {
		t.Fatalf("loadMaxLen: %v", err)
	}
	if n != DefaultMaxLen {
		t.Fatalf("maxLen = %d, want default %d", n, DefaultMaxLen)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_position_embeddings": 256}`), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err = loadMaxLen(dir)
	if err != nil || n != 256 {
		t.Fatalf("maxLen = %d, err = %v; want 256", n, err)
	}
}

func TestLoadVocabEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vocab.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWordPiece(path); err == nil {
		t.Fatal("expected error for empty vocab")
	}
}

// Encoder-level error paths that don't need ONNX Runtime: the constructor
// rejects bad input before touching the shared library.
func TestEncodeEmptyContent(t *testing.T) {
	if err := validateContent("   "); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestClassifyHonorsTypeHint(t *testing.T) {
	e := &Encoder{}
	got := e.classify(port.EncodeOptions{TypeHint: domain.MemoryTypeProcedural}, "Postgres notes")
	if got != domain.MemoryTypeProcedural {
		t.Fatalf("type = %s, want procedural (hint)", got)
	}
	got = e.classify(port.EncodeOptions{}, "we met Kafka team yesterday")
	if got != domain.MemoryTypeEpisodic {
		t.Fatalf("type = %s, want episodic (heuristics)", got)
	}
}

// TestListModels uses a hand-built Encoder (no session) because ListModels
// only reads config — proving ONNX isn't needed for registry behavior.
func TestListModels(t *testing.T) {
	e := &Encoder{cfg: Config{Model: DefaultModel}}
	models, err := e.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	m := models[0]
	if m.ModelID != DefaultModel || m.Provider != "huggingface" || !m.IsActive {
		t.Fatalf("model = %+v", m)
	}
	if m.Dimensions != 384 || m.DistanceMetric != domain.MetricCosine {
		t.Fatalf("dims/metric = %d/%s", m.Dimensions, m.DistanceMetric)
	}
}
