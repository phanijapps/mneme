// Package encoder is the adapter-side factory for MemoryEncoder
// implementations. Providers are selected by name at wiring time; adding a
// provider means adding a case here and a package under this directory.
package encoder

import (
	"fmt"
	"os"
	"strings"

	"github.com/phanijapps/mneme/internal/adapter/encoder/bge"
	"github.com/phanijapps/mneme/internal/adapter/encoder/stub"
	"github.com/phanijapps/mneme/internal/port"
)

// Providers supported by NewEncoder.
const (
	ProviderOllama = "ollama" // BGE-small (or other Ollama embedding models)
	ProviderStub   = "stub"   // deterministic, offline; tests only
)

// Config selects and parameterizes an encoder implementation.
type Config struct {
	// Provider names the backend: "ollama" or "stub".
	Provider string
	// BaseURL for HTTP-based providers (Ollama). Falls back to
	// OLLAMA_BASE_URL, then the provider default.
	BaseURL string
	// Model is the embedding model tag. Empty means the provider default
	// (bge-small-en-v1.5 for Ollama).
	Model string
}

// Defaults for the factory.
const (
	DefaultProvider = ProviderOllama
)

// NewEncoder builds a MemoryEncoder for cfg. Unknown providers are an error
// rather than a silent fallback, so wiring mistakes fail fast at startup.
func NewEncoder(cfg Config) (port.MemoryEncoder, error) {
	p := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if p == "" {
		p = DefaultProvider
	}
	switch p {
	case ProviderOllama:
		base := cfg.BaseURL
		if base == "" {
			base = os.Getenv("OLLAMA_BASE_URL")
		}
		return bge.New(bge.Config{BaseURL: base, Model: cfg.Model}), nil
	case ProviderStub:
		return stub.New(), nil
	default:
		return nil, fmt.Errorf("encoder: unknown provider %q (valid: %s, %s)",
			cfg.Provider, ProviderOllama, ProviderStub)
	}
}
