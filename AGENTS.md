# AGENTS.md — mneme Project Doctrine

This document defines the binding rules for any agent (or human) writing code in
this repository. When instructions conflict, this file wins over general habits.

Reference architecture: `docs/architecture.md` (3-layer: conceptual/logical/physical).
Data contracts: `docs/data-models.md`, `docs/pgvector-data-model.md`, `docs/api-contracts.md`.

---

## 1. Architecture — Clean Architecture, strictly layered

Dependency arrows point inward. A layer may only import from layers below it.

```
cmd/ (entry points)
  └── internal/adapter/transport  (chi HTTP + mcp-go handlers)
        └── internal/service      (use-case orchestration)
              └── internal/port   (interfaces owned by the use cases)
                    └── internal/domain  (entities, enums, errors — zero external deps)
                          ▲
        internal/adapter/{repository,recall,encoder,db} implement port interfaces
```

- **`internal/domain/`** — pure Go. No imports of pgx, chi, mcp-go, or any other
  module (validator tags as struct tags are the only concession). If domain needs
  it, it must be an interface defined in `internal/port/`.
- **`internal/port/`** — interfaces only. `MemoryRepository`, `RecallEngine`,
  `MemoryEncoder`, `TransactionManager`, service-facing ports. Ports are defined
  by consumers (services), not implementers.
- **`internal/adapter/`** — implementations. `repository/` (pgx), `recall/`
  (hybrid retrieval), `encoder/` (embeddings), `transport/` (chi REST + mcp-go
  MCP), `db/` (pool + migrations). Adapters may import domain + port, never
  service or transport siblings.
- **`internal/service/`** — use cases (SaveMemory, Recall, Consolidate, Decay,
  Promotion). Depend only on ports; construct adapters in `cmd/`.
- **`internal/config/`** — env/file loading. May be imported by cmd and adapters.

**Test placement:** unit tests live beside the code (`*_test.go`); cross-layer
tests in `tests/integration/` (testcontainers) and `tests/e2e/`.

## 2. TDD mandate

Tests are written **before or alongside** implementation, for every layer.
Red-green-refactor. A step is not done until `go test ./...` passes and the new
behavior has a test that fails without the implementation. Table-driven tests
with `testify` (`assert` for non-fatal, `require` for fatal checks).

## 3. Boy Scout Rule

Leave every file cleaner than you found it: fix naming, dead code, and missed
errors in passing. Small cleanups ship with the change that touched the file;
large ones get their own commit. Never leave `TODO` without a referenced issue
or plan step.

## 4. Module list — do not reinvent

Only these researched modules are used. Do not add a dependency without updating
this table and `internal/tools/tools.go` (which pins tool deps for `go mod tidy`).

| Purpose | Module | Import path |
|---------|--------|-------------|
| PostgreSQL driver | pgx v5 | `github.com/jackc/pgx/v5` |
| Vector types | pgvector-go | `github.com/pgvector/pgvector-go` |
| pgx vector registration | pgvector-go/pgx | `github.com/pgvector/pgvector-go/pgx` (alias `pgxvec`) |
| HTTP router | chi v5 | `github.com/go-chi/chi/v5` |
| MCP server | mcp-go | `github.com/mark3labs/mcp-go/mcp`, `.../server` |
| Migrations | goose v3 | `github.com/pressly/goose/v3` |
| Validation | validator v10 | `github.com/go-playground/validator/v10` |
| Test assertions | testify | `github.com/stretchr/testify` |
| Integration tests | testcontainers-go | `github.com/testcontainers/testcontainers-go` |
| UUIDs | google/uuid | `github.com/google/uuid` |
| OpenAPI docs | swaggo | `github.com/swaggo/swag`, `github.com/swaggo/http-swagger` |

Superseded modules are banned: `lib/pq` (use pgx/v5), `modelcontextprotocol/go-sdk`
(early-stage; use `mark3labs/mcp-go`).

## 5. Supersession-first retention

The data model is append-mostly. **Never hard-delete memories.** To retire a
memory, close its validity interval (`valid_to` set, `deleted_at` set) — a
superseding memory references the superseded one via `memory_links`
(`supersedes`). Repositories expose no `Delete` for memories; lifecycle jobs
(consolidation, decay) close validity, they do not purge. This rule is enforced
at every layer: domain (no delete method), service (use-case closes validity),
repository (SQL updates `valid_to`/`deleted_at`).

## 6. Provenance is server-stamped (F21)

`agent_id` / `actor` / principal fields come from the **authenticated principal**
resolved server-side — never accepted from client request bodies. Transports
(extract the principal from auth context) and services (stamp it onto entities)
own this; if a client-supplied provenance field arrives, ignore or reject it.
This applies to both REST and MCP transports.

## 7. Coding conventions

- **Go idioms first.** Follow [Effective Go](https://go.dev/doc/effective_go) and
  `gofmt`/`goimports`. No creative formatting.
- **Errors:** return `error` as the last value; wrap with `%w` using
  `fmt.Errorf`; use domain error types from `internal/domain/errors.go`
  (`ErrNotFound`, `ErrValidation`, `ErrVersionConflict`, ...) so transport maps
  them to API error codes. Never `panic` outside package init; never swallow.
- **Context propagation:** every I/O function takes `context.Context` as its
  first argument and passes it down (pgx queries, HTTP handlers, MCP handlers,
  embedding calls). No `context.Background()` mid-chain; it is created only at
  entry points (`cmd/`, test mains).
- **Naming:** exported types/methods have doc comments starting with the name;
  acronyms are capitalized (`API`, `ID`, `URL`). No stutter (`memory.Memory`,
  not `memory.MemoryService`).
- **Validation:** struct tags (`validate:"..."`) on domain structs; validation
  runs at transport boundaries before services execute.
- **SQL:** all DDL lives in `migrations/` as goose SQL files; repositories
  contain no DDL. Parameterize every query (`$1`, `$2`).
- **Comments:** explain *why*, not *what*. Code that needs a comment to say what
  it does gets renamed first.

## 8. Recall defaults

`top_k` default 50, maximum 200 (see `docs/api-contracts.md`). Recall engines and
transports both enforce the cap.

## 9. Definition of done (per step)

1. `go build ./...` passes.
2. `go test ./...` passes (new behavior covered).
3. `go vet ./...` is clean.
4. Doctrine sections above are respected (check imports, no deletes, provenance).
5. Plan step marked completed in `.zbot/specs/mneme-implementation/plan.md`.
