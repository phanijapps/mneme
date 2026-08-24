<p align="center"><img src="docs/assets/mneme-logo.svg" width="128" alt="mneme logo" /></p>

# mneme

<!-- Badges -->
<div align="center">

[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-336791?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![pgvector](https://img.shields.io/badge/pgvector-0.8+-purple?style=for-the-badge)](https://github.com/pgvector/pgvector)
[![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)](LICENSE)
[![Build Status](https://img.shields.io/badge/Build-Clean-brightgreen?style=for-the-badge)](#)
[![Go Report Card](https://img.shields.io/badge/Go%20Report-A+-brightgreen?style=for-the-badge)](https://goreportcard.com/report/github.com/phanijapps/mneme)

</div>

---

## 🧠 AI Agent Memory & Recall System

A high-performance Go service for storing, retrieving, and sharing AI agent memories, built on PostgreSQL + pgvector. mneme implements a supersession-first lifecycle with 4-way hybrid retrieval for optimal recall.

> **mneme** (/ˈniːmiː/, Greek for *memory*) is the brainchild of **zbot**, an autonomous AI agent. The design emerged from a genuinely collaborative human–agent conversation: the user asked for AI agent memory research, debated grammar over naming choices, refined the data models together, and ran adversarial reviews against the design before a single line of code existed.

---

## ✨ Key Features

| | | |
|---|---|---|
| 🧠 **Multi-type Memory** | Episodic, semantic, procedural, and working memory with temporal validity |
| 🔍 **4-Way Hybrid Retrieval** | Vector similarity + BM25-like + graph traversal + temporal weighting |
| 🔄 **Supersession-First Lifecycle** | No destructive updates — memories are never hard-deleted |
| 👥 **SharedMemorySpace** | Cross-session, cross-user, and cross-agent memory sharing |
| 🛡️ **Server-Stamped Provenance** | Unforgeable memory provenance with server-generated timestamps |
| 📡 **Dual Interface** | REST API (chi) + MCP server (mcp-go) sharing the same service core |
| 🧪 **TDD with Integration Tests** | testcontainers-go for PostgreSQL integration tests |
| 🏗️ **Clean Architecture** | Domain → Port → Adapter → Service → Transport layers |

---

## 🏛️ Architecture

Three-layer design (conceptual → logical → physical) with seven logical components, implemented using Clean Architecture in Go:

```
                         ┌──────────────────────────────────────────────────────┐
                         │                     AGENT RUNTIME                     │
                         │          (Claude Code / Codex / Cursor / Letta...)   │
                         └─────────────────┬────────────────────────▲───────────┘
                                           │                        │
                         writes            │                        │ context
                                           ▼                        │
                         ┌──────────────────────┐  ┌───────────────────────┐
                         │    MEMORY ENCODER    │─▶│       INDEX MGR        │
                         └──────────┬───────────┘  └───────────▲───────────┘
                                    │                           │
                                    ▼                           │
                         ┌──────────────────────┐  ┌───────────────────────┐
                         │      MEMORY STORE    │◀▶│    RETRIEVAL ENGINE   │
                         └──────────┬───────────┘  └───────────▲───────────┘
                                    │                           │
                                    ▼                           │
                         ┌──────────────────────┐  ┌───────────────────────┐
                         │     RECALL ROUTER    │◀─│   MEMORY LIFECYCLE    │
                         └──────────┬───────────┘  └───────────────────────┘
                                    │
                                    ▼
                         ┌──────────────────────┐
                         │   SHARED MEMORY SYNC │
                         └──────────────────────┘

Clean Architecture Layers:
┌──────────────────────────────────────────────────────────────────────────┐
│ TRANSPORT  │  REST API (chi)  │  MCP Server (mcp-go)  │  gRPC (future) │
├────────────┼─────────────────────────────────────────────────────────────┤
│  SERVICE   │  Use Cases: Store, Recall, Promote, Sync                     │
├────────────┼─────────────────────────────────────────────────────────────┤
│  ADAPTER   │  Repository (pgx), Encoder, Hybrid Recall Engine              │
├────────────┼─────────────────────────────────────────────────────────────┤
│   PORT     │  Interfaces: Storer, Recaller, Encoder, Indexer              │
├────────────┼─────────────────────────────────────────────────────────────┤
│   DOMAIN   │  Pure Entities: Memory, Entity, Session, SharedMemorySpace   │
└────────────┴─────────────────────────────────────────────────────────────┘
```

### Seven Core Components

| Component | Responsibility |
|-----------|----------------|
| **Memory Encoder** | Converts raw content into structured memories with embeddings |
| **Memory Store** | System of record; supersession-first retention |
| **Index Manager** | Maintains HNSW, GIN/tsvector, and graph indexes |
| **Retrieval Engine** | 4-way hybrid retrieval with Reciprocal Rank Fusion |
| **Recall Router** | Routes requests to appropriate retrieval strategies |
| **Memory Lifecycle Manager** | Handles promotion, decay scoring, TTL expiry |
| **Shared Memory Sync** | Cross-session/user/agent memory sharing |

---

## 🚀 Quick Start

### Prerequisites

- Go 1.26+
- PostgreSQL 16+ with pgvector 0.8+
- Docker (for integration and E2E tests via testcontainers)

### Build

```bash
git clone https://github.com/phanijapps/mneme.git
cd mneme

# Build REST API server
go build ./cmd/mneme-api    # → mneme-api

# Build MCP server
go build ./cmd/mneme-mcp    # → mneme-mcp
```

### Run Migrations

```bash
# Set database URL
export DATABASE_URL="postgres://user:pass@localhost:5432/mneme?sslmode=disable"

# Run migrations
go run ./cmd/migrate up
```

### Run

```bash
# Start REST API server
./mneme-api

# Or start MCP server (stdio)
./mneme-mcp
```

### Test

```bash
# Unit tests only
go test ./...

# Integration + E2E tests (requires Docker)
go test -tags integration ./tests/...

# Full test suite
go test ./... ./tests/...
```

---

## 📁 Project Structure

```
mneme/
├── cmd/
│   ├── mneme-api/          # REST API server entry point
│   ├── mneme-mcp/          # MCP server entry point
│   └── migrate/            # Migration CLI
├── internal/
│   ├── domain/              # Pure domain entities
│   │   ├── memory.go
│   │   ├── entity.go
│   │   ├── session.go
│   │   ├── space.go
│   │   └── ...
│   ├── port/                # Interface definitions (ports)
│   │   ├── repository.go
│   │   ├── recall.go
│   │   └── ...
│   ├── adapter/
│   │   ├── repository/     # PostgreSQL implementations
│   │   ├── recall/         # Hybrid retrieval implementations
│   │   └── encoder/        # Embedding implementations
│   ├── service/            # Use cases
│   └── transport/
│       ├── rest/           # chi HTTP handlers
│       └── mcp/            # mcp-go server
├── migrations/             # SQL migrations (goose)
├── docs/                   # Architecture documentation
│   ├── architecture.md     # 3-layer architecture
│   ├── data-models.md      # Conceptual data model
│   ├── pgvector-data-model.md  # Physical schema
│   ├── api-contracts.md    # REST + MCP APIs
│   ├── retrieval-and-recall.md # Retrieval strategies
│   ├── memory-taxonomy.md  # Memory types
│   └── data-model-review.md   # Adversarial review
├── tests/                  # Integration + E2E tests
├── go.mod
└── README.md
```

---

## 🛠️ Tech Stack

| Purpose | Module | Purpose |
|---------|--------|---------|
| **Go driver** | `jackc/pgx/v5` | PostgreSQL driver |
| **Vector types** | `pgvector/pgvector-go` | pgvector integration |
| **HTTP router** | `go-chi/chi/v5` | REST API router |
| **MCP server** | `mark3labs/mcp-go` | MCP protocol server |
| **Migrations** | `pressly/goose/v3` | SQL migrations |
| **Validation** | `go-playground/validator/v10` | Request validation |
| **Testing** | `stretchr/testify` | Test assertions |
| **Containers** | `testcontainers/testcontainers-go` | Integration tests |
| **UUIDs** | `google/uuid` | UUID generation |
| **OpenAPI** | `swaggo/swag` | API documentation |

---

## 📡 API Overview

### REST API Endpoints

| Group | Endpoints |
|-------|-----------|
| **Health** | `GET /api/v1/health` |
| **Memories** | `GET/POST /api/v1/memories`, `GET/PUT/DELETE /api/v1/memories/{id}` |
| **Recall** | `POST /api/v1/recall` |
| **Spaces** | `GET/POST /api/v1/spaces`, `GET/POST /api/v1/spaces/{id}/members` |
| **Promotion** | `POST /api/v1/promote`, `GET /api/v1/promote/{id}` |
| **Entities** | `GET /api/v1/entities`, `POST /api/v1/entities`, `GET /api/v1/entities/{id}` |
| **Sessions** | `GET/POST /api/v1/sessions`, `GET /api/v1/sessions/{id}` |

### MCP Tools

| Group | Tools |
|-------|-------|
| **Memory** | `mneme_store_memory`, `mneme_get_memory`, `mneme_update_memory`, `mneme_delete_memory` |
| **Recall** | `mneme_recall_memories`, `mneme_get_context` |
| **Links** | `mneme_link_memories`, `mneme_get_related` |
| **Spaces** | `mneme_create_space`, `mneme_list_spaces`, `mneme_share_memory`, `mneme_join_space` |
| **Promotion** | `mneme_promote_memory`, `mneme_list_proposals` |
| **Entities** | `mneme_create_entity`, `mneme_get_entity`, `mneme_update_entity` |

---

## 💾 Data Model Overview

| Entity | Description |
|--------|-------------|
| **Memory** | Core memory with content, embedding, tags, temporal validity, supersession chain |
| **MemoryLink** | Directed weighted edges between memories for graph traversal |
| **Entity** | Named entities (people, concepts) with temporal facts |
| **AgentSession** | Agent runtime session with context window budget |
| **SharedMemorySpace** | Shared memory container with access policies |
| **PromotionProposal** | Workflow for promoting memories between spaces |
| **EmbeddingModel** | Registry for embedding models and dimensions |

---

## 📚 Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | 3-layer architecture, 7 components, clean architecture layers |
| [Data Models](docs/data-models.md) | Conceptual model (8 entities + 2 logs) |
| [pgvector Schema](docs/pgvector-data-model.md) | Physical PostgreSQL schema with DDL, indexes |
| [API Contracts](docs/api-contracts.md) | 26 REST endpoints + 24 MCP tools |
| [Retrieval & Recall](docs/retrieval-and-recall.md) | 4-way hybrid search, Reciprocal Rank Fusion |
| [Memory Taxonomy](docs/memory-taxonomy.md) | CoALA framework + production system extensions |
| [Data Model Review](docs/data-model-review.md) | Adversarial review (22 findings, 5 critical) |
| [How It Was Built](docs/HOW_IT_WAS_BUILT.md) | Transparency document on the build process |

### 🧭 Setup Guides

| Guide | What it covers |
|-------|----------------|
| [Database Setup](docs/guides/db-setup.md) | pglite honesty check, Docker pgvector, local install, migrations, verification |
| [MCP Server Setup](docs/guides/mcp-setup.md) | Claude Code / Codex / Cursor / generic client config, 24 tools, 3 resources |
| [REST API Setup](docs/guides/api-setup.md) | Env vars, Dockerfile, docker-compose, Swagger UI, 26 endpoints, auth |

---

## 🌐 Website

The project website is hosted on GitHub Pages: **[https://phanijapps.github.io/mneme](https://phanijapps.github.io/mneme)**

To deploy: push to `main` branch with GitHub Actions enabled. The site is served from `/docs/site/`.

---

## 🔧 How It Was Built

mneme is the brainchild of **zbot**, an autonomous AI agent — and it wasn't
built the way most software is. It emerged from a genuinely collaborative
human–agent conversation before a single line of code existed. The full
transparency document is **[docs/HOW_IT_WAS_BUILT.md](docs/HOW_IT_WAS_BUILT.md)**;
the short version of the story:

### The Full Chat Transcript (Condensed)

1. **The research request.** The user asked zbot to research AI agent memory
   and recall systems — already knowing the taxonomy (semantic, procedural,
   working, episodic memory). zbot surveyed CoALA, Letta, Mem0, Zep, A-MEM
   and the coding tools (Claude Code, Cursor, Copilot) and came back with an
   architecture.
2. **The naming correction.** zbot proposed `TeamMemorySpace`. The user
   pushed back — the concept wasn't about teams at all. It could be the same
   user across sessions, different users, or AI agents sharing memory.
   Renamed **`SharedMemorySpace`**. A meaningful refinement, honestly earned.
3. **The grammar debate.** Along the way there was an actual argument about
   "team memory" vs "teams' memory" (possessive plural? compound noun?).
   zbot conceded that English doesn't work the way the concept needed and
   suggested "shared memory". The concept won; the grammar was benched.
4. **The entity interrogation.** The user questioned whether
   `RecallRequest` and `RecallResult` were real entities. After discussion
   they were demoted to **log records** — ephemeral observations of
   retrieval operations, not durable domain state.
5. **The adversarial review.** The user demanded zbot attack its own data
   model. Result: **22 findings, 5 critical** — provenance spoofing,
   supersession gaps, access-control holes among them. All 5 criticals were
   fixed *before* implementation; the rest were deferred with rationale in
   [docs/data-model-review.md](docs/data-model-review.md).
6. **The physical model.** The user asked for the pgvector physical schema.
   zbot produced the DDL — HNSW indexes, tsvector, recursive graph CTEs —
   and **validated it against a real PostgreSQL 17** container before
   calling it done.
7. **The API contracts.** REST (26 endpoints) + MCP (24 tools) were designed
   and reviewed as formal contracts in
   [docs/api-contracts.md](docs/api-contracts.md) — implementation had to
   match, not invent.
8. **The overnight build.** The user said "build the best repo you can" and
   went to sleep. zbot executed the 11-step plan through the night: domain →
   ports → adapters → services → transports → tests.
9. **The pglite question.** The user asked for "pglite, if it exists for
   golang" — it doesn't (pglite is Node/browser WASM). zbot said so plainly
   and used **testcontainers-go** with real pgvector containers instead.
   Honesty over convenience.

**The takeaway:** every design decision in this repo traces back to a
question a human asked, an answer an agent gave, and a debate that made both
better. The code is the transcript's conclusion, not its replacement.

See **[docs/HOW_IT_WAS_BUILT.md](docs/HOW_IT_WAS_BUILT.md)** for the full
document: origin story, design rationale, adversarial findings, the 11-step
plan, and timeline.

---

## 🤝 Contributing

Contributions are welcome! Please follow these guidelines:

1. **Read [AGENTS.md](AGENTS.md)** — project doctrine and workflow
2. **Follow Clean Architecture** — domain → port → adapter → service → transport
3. **Write tests first** — TDD approach, use testcontainers for integration tests
4. **Boy Scout Rule** — leave the code cleaner than you found it
5. **Run linting** — `go vet ./...` and `golangci-lint run`
6. **Update docs** — if you change APIs or architecture, update the corresponding doc

---

## 📜 License

MIT License — see [LICENSE](LICENSE) for details.

---

## 🙏 Credits

**mneme** is the brainchild of **zbot**, an autonomous AI agent.

This project was designed and implemented by zbot, an autonomous AI agent, under human direction. All design decisions were reviewed and approved by the human. The adversarial review was run to catch design flaws before implementation.

The research, debates, and reviews that produced this architecture are preserved in the documentation under `docs/`.
