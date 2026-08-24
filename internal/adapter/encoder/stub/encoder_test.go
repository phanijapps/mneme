package stub

import (
	"context"
	"testing"

	"github.com/phanijapps/mneme/internal/domain"
	"github.com/phanijapps/mneme/internal/port"
)

func TestStubEncodeDeterministic(t *testing.T) {
	e := New()
	a, err := e.Encode(context.Background(), "same content", port.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	b, err := e.Encode(context.Background(), "same content", port.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(a.Vector.Slice()) != Dimensions {
		t.Fatalf("dims = %d, want %d", len(a.Vector.Slice()), Dimensions)
	}
	for i := range a.Vector.Slice() {
		if a.Vector.Slice()[i] != b.Vector.Slice()[i] {
			t.Fatalf("not deterministic at dim %d", i)
		}
	}
	if a.Vector.Slice()[0] == b.Vector.Slice()[1] && a.Vector.Slice()[0] != 0 {
		t.Fatal("different inputs should differ")
	}
}

func TestStubEncodeDistinctInputs(t *testing.T) {
	e := New()
	a, _ := e.Encode(context.Background(), "alpha", port.EncodeOptions{})
	b, _ := e.Encode(context.Background(), "beta", port.EncodeOptions{})
	if a.Vector.Slice()[0] == b.Vector.Slice()[0] {
		t.Fatal("distinct inputs produced identical vectors")
	}
}

func TestStubEncodeEmpty(t *testing.T) {
	if _, err := New().Encode(context.Background(), "", port.EncodeOptions{}); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestStubEncodeBatch(t *testing.T) {
	e := New()
	out, err := e.EncodeBatch(context.Background(), []string{"a", "b", "c"}, port.EncodeOptions{})
	if err != nil {
		t.Fatalf("EncodeBatch: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
}

func TestStubTypeHintWins(t *testing.T) {
	e := New()
	em, _ := e.Encode(context.Background(), "Postgres notes", port.EncodeOptions{TypeHint: domain.MemoryTypeSemantic})
	if em.InferredType != domain.MemoryTypeSemantic {
		t.Fatalf("type = %s", em.InferredType)
	}
}

func TestStubListModels(t *testing.T) {
	models, err := New().ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "stub-encoder" || !models[0].IsActive {
		t.Fatalf("models = %+v", models)
	}
}
