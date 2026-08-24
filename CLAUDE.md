# CLAUDE.md

Guidance for Claude Code and other AI agents working in this repository.

## Architecture overview

mneme is a Go service for storing, retrieving, and sharing AI agent memories,
backed by PostgreSQL + pgvector. Clean Architecture with strict layering —
dependencies point inward only:

```
cmd/mneme-api, cmd/mneme-mcp        entry points (construct adapters, wire services)
  └── internal/adapter/transport    chi REST handlers + mcp-go MCP handlers
        └── internal/service        use cases (SaveMemory, Recall, Consolidate, ...)
              └── internal/port     interfaces (MemoryRepository, RecallEngine, ...)
                    └── internal/domain  entities, enums, errors (zero external deps)

internal/adapter/repository         pgx implementations of repository ports
internal/adapter/recall             4-way hybrid retrieval (vector + lexical + graph + temporal)
internal/adapter/encoder            embedding generation
internal/adapter/db                 connection pool + goose migrations runner
```

Full 3-layer architecture (conceptual/logical/physical) and the 7 logical
components: `docs/architecture.md`.

## Where to find documentation

All design docs live in `docs/`:

| File | Contents |
|------|----------|
| `docs/architecture.md` | 3-layer architecture, 7 components, data flows |
| `docs/data-models.md` | Entity definitions, field tables, JSON schemas |
| `docs/pgvector-data-model.md` | Physical DDL, column types, CHECK constraints, indexes |
| `docs/api-contracts.md` | REST + MCP request/response contracts, error codes |
| `docs/retrieval-and-recall.md` | Recall strategies, ranking, fusion (RRF), injection |
| `docs/memory-taxonomy.md` | Memory types, taxonomy, lifecycle |
| `docs/data-model-review.md` | Adversarial review findings (F1–F24) and resolutions |

`AGENTS.md` holds the binding project doctrine — read it before writing code.

## Running tests

```bash
go test ./...          # unit tests (no external dependencies)
go test ./tests/integration/...   # requires Docker (testcontainers)
go test ./tests/e2e/...           # requires Docker (testcontainers)
```

## Running migrations

```bash
# goose v3 SQL migrations live in migrations/
goose -dir migrations postgres "$DATABASE_URL" up
goose -dir migrations postgres "$DATABASE_URL" status
```

The service also runs migrations on startup via the embedded runner in
`internal/adapter/db/`.

## Key design rules

1. **Supersession-first:** never hard-delete a memory. Close its validity
   interval (`valid_to` / `deleted_at`) and link the replacement via
   `memory_links` (`supersedes` relationship). No repository exposes memory
   deletion.
2. **Provenance is server-stamped (F21):** `agent_id` / `actor` come from the
   authenticated principal resolved server-side. Client-supplied provenance
   fields are ignored or rejected — in both REST and MCP transports.
3. **`top_k` bounds:** default 50, maximum 200. Enforced by recall engine and
   transport validation alike.
4. **Domain purity:** `internal/domain/` imports nothing external. Anything the
   domain needs becomes an interface in `internal/port/`.
5. **TDD:** tests before/alongside implementation; `go test ./...` must pass
   before a step is done.

## Module dependencies

pgx/v5, pgvector-go, chi/v5, mcp-go, goose/v3, validator/v10, testify,
testcontainers-go, google/uuid, swaggo/swag + http-swagger. Do not add
alternatives (no `lib/pq`, no `modelcontextprotocol/go-sdk`). `internal/tools/tools.go`
pins these for `go mod tidy`.
