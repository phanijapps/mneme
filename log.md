# mneme ward log

## [2026-08-24] feature | BGE-small encoder via Ollama, embedding provider swap guide, origin-story.md, README layout fix

- `docs/origin-story.md` created (full transcript + Latest Update); `docs/how-i-was-built.md` deleted.
- README: website/GitHub link at very top, logo+name side-by-side centered, Origin Story + Embedding Guide links, Pluggable Encoder feature row.
- `internal/adapter/encoder/`: `bge/` (Ollama bge-small-en-v1.5, 384 dims), `text/` (shared heuristics), `stub/` (offline deterministic), `encoder.go` factory (ollama|stub). Unit tests with mock Ollama HTTP server — all pass.
- `docs/embedding-guide.md`: swap guide for Bedrock, Vertex AI, MLflow, Ollama variants, OpenAI + comparison table + re-embedding caveats.
- `go build ./...`, `go vet ./internal/adapter/encoder/...`, `go test ./internal/adapter/encoder/...` green.

## [2026-08-24] feature | mneme-viz: 3D memory-graph dashboard app

- `cmd/mneme-viz/main.go`: standalone UI server — embedded HTML at `/`, reverse proxy `/api/v1/*` → `MNEME_API_URL` (default localhost:8080), `VIZ_PORT` (default :8090), graceful shutdown + slog JSON (same pattern as mneme-api).
- `internal/viz/embed.go` (embed.FS) + `internal/viz/index.html`: self-contained dashboard — D3 v7 + d3-force-3d + Three.js 0.160 CDN only; 3D force-directed graph (sphere=memory by type, octahedron=session), link colors by relationship, starfield, hover labels/tooltips, click→detail panel, dblclick→expand links, "Recall similar", sidebar stats/filters/search, top-bar connection status + API-key input (Bearer via localStorage), auto-rotate toggle.
- Verified: `go build ./cmd/mneme-viz`, `go build ./...`, `go vet ./...`, HTML served at `/`, proxy 401/502/auth-header passthrough against a stub API, SIGTERM → "stopped" logged.

