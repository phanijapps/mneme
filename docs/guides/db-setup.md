# Database Setup Guide

mneme stores everything in a single PostgreSQL 16+ database with the
[pgvector](https://github.com/pgvector/pgvector) extension (HNSW vector
indexes + tsvector full-text + recursive CTE graph traversal). This guide
gets that database running and verified.

---

## Can I use pglite?

Honest answer: **no, not for mneme.** [pglite](https://pglite.dev) is a
WASM-compiled PostgreSQL that runs embedded **inside Node.js or the
browser**. There is no Go equivalent and no cgo/FFI bridge that lets a Go
binary embed pglite today. The properties that make pglite attractive
(zero-install, single-file, embedded) are Node-runtime-specific.

What mneme uses instead for "PostgreSQL without a local install":

| Need | Solution used |
|------|---------------|
| Local dev database | Docker `pgvector/pgvector:pg16` (one command, below) |
| CI / integration tests | [testcontainers-go](https://golang.testcontainers.org) spins up an ephemeral pgvector container per test run — see `tests/` |
| Embedded-ish testing in Go | Not available; testcontainers is the closest practical substitute |

If a Go-embeddable Postgres-WASM appears, the interface is isolated behind
`internal/adapter/db` and the migration layer, so swapping it in would be a
single-adapter change.

---

## Option A — PostgreSQL on Docker (recommended)

One command, correct extension pre-installed:

```bash
docker run -d \
  --name mneme-db \
  -p 5432:5432 \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=mneme \
  pgvector/pgvector:pg16
```

- Image: `pgvector/pgvector:pg16` — official Postgres 16 with pgvector
  compiled in (the plain `postgres:16` image does **not** include it).
- Connection string becomes
  `postgres://postgres:postgres@localhost:5432/mneme?sslmode=disable`.

Useful lifecycle commands:

```bash
docker stop mneme-db    # stop (data survives in the anonymous volume)
docker start mneme-db   # start again
docker logs mneme-db    # watch the Postgres log
docker rm -f mneme-db   # destroy container + its data
```

## Option B — Local PostgreSQL install

1. **Install PostgreSQL 16+** — use your platform's package manager
   (`apt install postgresql-16`, `brew install postgresql@16`, or the
   EnterpriseDB installer).
2. **Install the pgvector extension** — build from source or package:

   ```bash
   # Debian/Ubuntu (PGDG repo)
   apt install postgresql-16-pgvector

   # macOS
   brew install pgvector

   # From source (any platform)
   git clone --branch v0.8.0 https://github.com/pgvector/pgvector.git
   cd pgvector && make && make install
   ```

3. **Create the database**:

   ```bash
   createdb mneme
   # or: psql -c 'CREATE DATABASE mneme;'
   ```

4. pgvector itself is created by migration `001_extensions.sql`
   (`CREATE EXTENSION IF NOT EXISTS vector`), so no manual
   `CREATE EXTENSION` is needed — but the extension binaries must be
   present on the server (steps 1–2).

---

## Connection string format

```
postgres://<user>:<password>@<host>:5432/<dbname>?sslmode=disable
```

mneme reads it from one environment variable:

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/mneme?sslmode=disable"
```

Notes:

- `DATABASE_URL` is the canonical variable (default:
  `postgres://localhost:5432/mneme?sslmode=disable`).
- Use `sslmode=require` (or `verify-full` + CA cert) for any non-local
  deployment; `sslmode=disable` is for local dev only.
- The pool is created in `internal/adapter/db` (`db.NewPool`); pool tuning
  (`MaxConns`, `MaxConnIdleSec` via `db.PoolConfig`) is code-level in v1,
  not yet env-driven.

## Running migrations

Migrations are **embedded in the binary** (`migrations/*.sql` + `embed.go`)
and applied automatically at `mneme-api` startup:

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/mneme?sslmode=disable"
go run ./cmd/mneme-api        # pool + goose migrations run before serving
```

Prefer running them by hand? The migration files are standard
[goose](https://github.com/pressly/goose) SQL migrations:

```bash
goose postgres "$DATABASE_URL" up        # from the repo root (migrations/)
goose postgres "$DATABASE_URL" down      # roll back one
goose postgres "$DATABASE_URL" status    # see applied versions
```

## Verification

Confirm the database is ready and mneme's schema is in place:

```bash
# 1. Server is up and pgvector is available
psql "$DATABASE_URL" -c "SELECT extname, extversion FROM pg_extension;"
# expect: vector | 0.8.x

# 2. goose bookkeeping table exists
psql "$DATABASE_URL" -c "SELECT version_id, is_applied FROM goose_db_version ORDER BY version_id DESC LIMIT 5;"
# expect: 18 rows applied in order

# 3. Core tables exist
psql "$DATABASE_URL" -c "\dt"
# expect: memories, memory_links, memory_embeddings, shared_memory_spaces,
#         agent_sessions, recall_requests, recall_results, promotion_proposals,
#         lifecycle_jobs, entities, space_memberships, memory_access_log,
#         embedding_models

# 4. Vector index sanity check (after data exists)
psql "$DATABASE_URL" -c "
  SELECT indexname FROM pg_indexes
  WHERE tablename = 'memory_embeddings' AND indexdef LIKE '%hnsw%';"
```

If the API itself starts cleanly (`mneme-api listening addr=:8080` in the
JSON logs), migrations already succeeded — startup aborts before serving
otherwise.
