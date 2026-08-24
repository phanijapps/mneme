package encoder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phanijapps/mneme/internal/port"
)

func TestNewEncoderOllama(t *testing.T) {
	enc, err := NewEncoder(Config{Provider: ProviderOllama, BaseURL: "http://localhost:11434"})
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	if enc == nil {
		t.Fatal("nil encoder")
	}
	var _ port.MemoryEncoder = enc
}

func TestNewEncoderStub(t *testing.T) {
	enc, err := NewEncoder(Config{Provider: ProviderStub})
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	var _ port.MemoryEncoder = enc
}

func TestNewEncoderDefaultProvider(t *testing.T) {
	enc, err := NewEncoder(Config{})
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	var _ port.MemoryEncoder = enc
}

func TestNewEncoderUnknownProvider(t *testing.T) {
	if _, err := NewEncoder(Config{Provider: "openai"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// TestNewEncoderHuggingFaceDelegatesToONNX proves the "huggingface" case
// routes to the ONNX encoder: a dir with a garbage model.onnx must fail
// with the hf ONNX error (session load), not a sidecar/HTTP error.
func TestNewEncoderHuggingFaceDelegatesToONNX(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.onnx"), []byte("\x00garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vocab.txt"), []byte("[PAD]\n[UNK]\n[CLS]\n[SEP]\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewEncoder(Config{Provider: ProviderHuggingFace, ModelDir: dir})
	if err == nil {
		t.Fatal("expected session-load error for garbage model.onnx")
	}
	if !strings.Contains(err.Error(), "hf: onnx") {
		t.Fatalf("err = %v, want hf: onnx prefix (ONNX encoder, not sidecar)", err)
	}
	if strings.Contains(err.Error(), "http") || strings.Contains(err.Error(), "sidecar") {
		t.Fatalf("err = %v, must not reference HTTP/sidecar", err)
	}
}

// TestNewEncoderHuggingFaceRequiresModelDir mirrors the fail-fast contract.
func TestNewEncoderHuggingFaceRequiresModelDir(t *testing.T) {
	t.Setenv("HF_MODEL_DIR", "")
	if _, err := NewEncoder(Config{Provider: ProviderHuggingFace}); err == nil {
		t.Fatal("expected error for missing ModelDir")
	}
}
