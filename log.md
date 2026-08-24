# mneme ward log

## [2026-08-24] feature | BGE-small encoder via Ollama, embedding provider swap guide, origin-story.md, README layout fix

- `docs/origin-story.md` created (full transcript + Latest Update); `docs/how-i-was-built.md` deleted.
- README: website/GitHub link at very top, logo+name side-by-side centered, Origin Story + Embedding Guide links, Pluggable Encoder feature row.
- `internal/adapter/encoder/`: `bge/` (Ollama bge-small-en-v1.5, 384 dims), `text/` (shared heuristics), `stub/` (offline deterministic), `encoder.go` factory (ollama|stub). Unit tests with mock Ollama HTTP server — all pass.
- `docs/embedding-guide.md`: swap guide for Bedrock, Vertex AI, MLflow, Ollama variants, OpenAI + comparison table + re-embedding caveats.
- `go build ./...`, `go vet ./internal/adapter/encoder/...`, `go test ./internal/adapter/encoder/...` green.
