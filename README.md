# mneme

**AI Agent Memory & Recall System** — a Go service for storing, retrieving, and
sharing AI agent memories, built on PostgreSQL + pgvector.

mneme is the brainchild of **zbot**, an autonomous AI agent. The design emerged
from a genuinely collaborative human–agent conversation: the user asked for AI
agent memory research, the agent debated grammar with the user over a data-model
naming choice, they refined the data models together, and ran adversarial
reviews against the design before a single line of code existed. The full
conversation survives in the architecture docs under `docs/`.

## Architecture

Three-layer design (conceptual → logical → physical), seven logical components,
implemented with Clean Architecture in Go:

```
                         ┌─────────────────────────────────────────────┐
                         │                AGENT RUNTIME                │
                         │  (Claude Code / Codex / Cursor / Letta...)  │
                         └──────┬───────────────────────────▲──────────┘
                        writes  │                           │ context
                                ▼                           │
   ┌────────────────┐   ┌──────────────┐   ┌──────────────┐ │
   │ MEMORY ENCODER │──▶│ MEMORY STORE │◀─▶│ INDEX MGR    │ │
   └────────────────┘   └──────┬───────┘   └──────▲───────┘ │
                               │                  │         │
                               │   ┌──────────────┴──────┐  │
                               │   │ RETRIEVAL ENGINE    │──┼──┐
                               │   └──────────▲──────────┘  │  │
                               │              │ candidates  │  │
                               │   ┌──────────┴──────────┐  │  │
                               └──▶│ RECALL ROUTER       │──┘  │
                                   └──────────▲──────────┘     │
                                              │ recall request │
                                   ┌──────────┴──────────┐     │
                                   │ MEMORY LIFECYCLE MGR│◀────┘
                                   └──────────┬──────────┘
                                              │
                                   ┌──────────▼──────────┐
                                   │ TEAM SYNC LAYER     │
                                   └─────────────────────┘
```

- **Clean Architecture:** `domain` (pure entities) ← `port` (interfaces) ←
  `adapter` (pgx repository, hybrid recall engine, encoder, chi REST + mcp-go
  MCP transports) ← `service` (use cases) — dependencies point inward only.
- **Hybrid recall:** 4-way retrieval (vector similarity, lexical rank, graph
  traversal via CTEs, temporal weighting) fused with Reciprocal Rank Fusion.
- **Supersession-first retention:** memories are never hard-deleted — validity
  intervals close and replacements link via `supersedes`.
- **Two front doors:** REST API (`cmd/mneme-api`, chi) and MCP server
  (`cmd/mneme-mcp`, mcp-go) sharing the same service core.

Full details: [`docs/architecture.md`](docs/architecture.md).

## Prerequisites

- Go 1.23+
- PostgreSQL 16+ with pgvector 0.8+
- Docker (for integration and E2E tests via testcontainers)

## Build

```bash
go build ./cmd/mneme-api    # → mneme-api
go build ./cmd/mneme-mcp    # → mneme-mcp
```

## Test

```bash
go test ./...               # unit tests
go test ./tests/...         # integration + E2E (requires Docker)
```

## Run

```bash
./mneme-api                 # REST API server
./mneme-mcp                 # MCP server (stdio)
```

## Modules

| Purpose | Module |
|---------|--------|
| PostgreSQL driver | [jackc/pgx/v5](https://github.com/jackc/pgx) |
| Vector types | [pgvector/pgvector-go](https://github.com/pgvector/pgvector-go) |
| HTTP router | [go-chi/chi/v5](https://github.com/go-chi/chi) |
| MCP server | [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) |
| Migrations | [pressly/goose/v3](https://github.com/pressly/goose) |
| Validation | [go-playground/validator/v10](https://github.com/go-playground/validator) |
| Test assertions | [stretchr/testify](https://github.com/stretchr/testify) |
| Integration tests | [testcontainers/testcontainers-go](https://github.com/testcontainers/testcontainers-go) |
| UUIDs | [google/uuid](https://github.com/google/uuid) |
| OpenAPI docs | [swaggo/swag](https://github.com/swaggo/swag) + [swaggo/http-swagger](https://github.com/swaggo/http-swagger) |

## Documentation

See [`docs/`](docs/) for the full architecture: data models, pgvector physical
schema, API contracts, retrieval & recall design, memory taxonomy, and the
adversarial design review.

## Provenance

Designed by **zbot** (autonomous AI agent) in collaboration with its user,
2026. The research, debates, and reviews that produced this architecture are
preserved in the `mneme` and `ai-memory-research` wards of the zbot workspace.
