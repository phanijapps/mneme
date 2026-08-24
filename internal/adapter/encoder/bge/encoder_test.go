package bge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

// newMock spins up an httptest server speaking the Ollama /api/embed and
// /api/tags contract. Every input string maps to a deterministic 384-dim
// vector seeded from its first byte.
func newMock(t *testing.T) (*httptest.Server, *map[string]int) {
	t.Helper()
	requests := map[string]int{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, r *http.Request) {
		requests["embed"]++
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad json"})
			return
		}
		if req.Model != DefaultModel {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "model '" + req.Model + "' not found"})
			return
		}
		embeddings := make([][]float64, len(req.Input))
		for i, in := range req.Input {
			s, _ := in.(string)
			embeddings[i] = vector384(s)
		}
		_ = json.NewEncoder(w).Encode(embedResponse{Model: req.Model, Embeddings: embeddings})
	})
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		requests["tags"]++
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{
				"name":   "bge-small-en-v1.5:latest",
				"model":  "bge-small-en-v1.5:latest",
				"details": map[string]any{"family": "bert", "families": []string{"bert"}},
			},
			{
				"name":   "llama3.2:latest",
				"model":  "llama3.2:latest",
				"details": map[string]any{"family": "llama", "families": []string{"llama"}},
			},
			{
				"name":   "nomic-embed-text:latest",
				"model":  "nomic-embed-text:latest",
				"details": map[string]any{"family": "nomic-bert", "families": []string{"nomic-bert"}},
			},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &requests
}

// vector384 builds a deterministic 384-dim vector from a string.
func vector384(s string) []float64 {
	v := make([]float64, DefaultDimensions)
	if s == "" {
		return v
	}
	seed := float64(s[0])
	for i := range v {
		v[i] = (seed + float64(i)) / 1000.0
	}
	return v
}

func newTestEncoder(t *testing.T) *Encoder {
	t.Helper()
	srv, _ := newMock(t)
	return New(Config{BaseURL: srv.URL})
}

func TestEncodeReturns384Dims(t *testing.T) {
	e := newTestEncoder(t)
	em, err := e.Encode(context.Background(), "Postgres uses HNSW indexes", port.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(em.Vector.Slice()) != DefaultDimensions {
		t.Fatalf("dims = %d, want %d", len(em.Vector.Slice()), DefaultDimensions)
	}
	want := vector384("Postgres uses HNSW indexes")
	got := em.Vector.Slice()
	for i := range want {
		if got[i] != float32(want[i]) {
			t.Fatalf("vec[%d] = %f, want %f", i, got[i], want[i])
		}
	}
}

func TestEncodeClassifiesAndExtracts(t *testing.T) {
	e := newTestEncoder(t)
	em, err := e.Encode(context.Background(), "we met Kafka team yesterday", port.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if em.InferredType != domain.MemoryTypeEpisodic {
		t.Fatalf("type = %s, want episodic", em.InferredType)
	}
	if len(em.Entities) == 0 {
		t.Fatal("expected entities from 'Kafka'")
	}
	if em.EstimatedTokens <= 0 {
		t.Fatalf("tokens = %d, want > 0", em.EstimatedTokens)
	}
}

func TestEncodeHonorsTypeHint(t *testing.T) {
	e := newTestEncoder(t)
	em, err := e.Encode(context.Background(), "Postgres notes", port.EncodeOptions{TypeHint: domain.MemoryTypeProcedural})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if em.InferredType != domain.MemoryTypeProcedural {
		t.Fatalf("type = %s, want procedural (hint)", em.InferredType)
	}
}

func TestEncodeEmptyContent(t *testing.T) {
	e := newTestEncoder(t)
	if _, err := e.Encode(context.Background(), "   ", port.EncodeOptions{}); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestEncodeBatch(t *testing.T) {
	e := newTestEncoder(t)
	contents := []string{"alpha", "beta", "gamma"}
	out, err := e.EncodeBatch(context.Background(), contents, port.EncodeOptions{})
	if err != nil {
		t.Fatalf("EncodeBatch: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	for i, em := range out {
		if len(em.Vector.Slice()) != DefaultDimensions {
			t.Fatalf("out[%d] dims = %d", i, len(em.Vector.Slice()))
		}
		want := vector384(contents[i])
		if em.Vector.Slice()[0] != float32(want[0]) {
			t.Fatalf("out[%d] not in input order", i)
		}
	}
}

func TestEncodeBatchEmpty(t *testing.T) {
	e := newTestEncoder(t)
	if _, err := e.EncodeBatch(context.Background(), nil, port.EncodeOptions{}); err == nil {
		t.Fatal("expected error for empty batch")
	}
}

func TestEncodeConnectionRefused(t *testing.T) {
	e := New(Config{BaseURL: "http://127.0.0.1:1"})
	_, err := e.Encode(context.Background(), "x", port.EncodeOptions{})
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("err = %v, want unreachable wrap", err)
	}
}

func TestEncodeInvalidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not json"))
	}))
	t.Cleanup(srv.Close)
	e := New(Config{BaseURL: srv.URL})
	if _, err := e.Encode(context.Background(), "x", port.EncodeOptions{}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestEncodeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	e := New(Config{BaseURL: srv.URL})
	_, err := e.Encode(context.Background(), "x", port.EncodeOptions{})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want 500 wrap", err)
	}
}

func TestListModelsFiltersEmbeddingModels(t *testing.T) {
	e := newTestEncoder(t)
	models, err := e.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2 (llama3.2 filtered out)", len(models))
	}
	var active int
	for _, m := range models {
		if m.Provider != "ollama" {
			t.Fatalf("provider = %s", m.Provider)
		}
		if m.DistanceMetric != domain.MetricCosine {
			t.Fatalf("metric = %s", m.DistanceMetric)
		}
		if m.IsActive {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active = %d, want 1", active)
	}
}

func TestListModelsNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	e := New(Config{BaseURL: srv.URL})
	if _, err := e.ListModels(context.Background()); err == nil {
		t.Fatal("expected no-models error")
	}
}

func TestListModelsConnectionRefused(t *testing.T) {
	e := New(Config{BaseURL: "http://127.0.0.1:1"})
	if _, err := e.ListModels(context.Background()); err == nil {
		t.Fatal("expected connection error")
	}
}
