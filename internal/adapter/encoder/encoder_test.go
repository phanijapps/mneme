package encoder

import (
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
