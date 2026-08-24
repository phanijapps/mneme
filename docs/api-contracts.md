# API Contracts — REST + MCP for AI Agent Memory & Recall

Design-only specification of the external interfaces for the memory system defined in
[[pages/data-models|Data Models]] (8 entities) and [[pages/architecture|Architecture]]
(7 logical components). No implementation code. Every operation below is traced to the
component that owns it, and every schema field is traced to the entity field it carries.

**Why two protocols.** A REST API serves deterministic service-to-service integration
(dashboards, batch tooling, CI jobs). An MCP server serves the consumers the research
identifies as the primary clients: coding agents (Codex CLI, Claude Code, Cursor) whose
persistent memory is added via MCP integrations [source: 08-codex-copilot-persistent-memory].
Both fronts share one internal surface — the 7 components — so capabilities stay in lockstep.

## Component Backing Map

Every interface group is a thin façade over exactly one logical component (architecture §2.2):

| API group | Owning component | Architecture flow |
|---|---|---|
| Memory CRUD (`/memories*`) | Memory Encoder (writes) + Memory Store (reads/mutations) + Index Manager (links, re-index) | Flow A — Encode |
| Recall (`/recall*`) | Recall Router (policy) → Retrieval Engine (execution) | Flow B — Recall |
| Sessions (`/sessions*`) | Recall Router (injection plan) + AgentSession state | Flow D — Bootstrap |
| Shared spaces (`/spaces*`) | Shared Memory Sync Layer (+ Lifecycle Manager for promotion candidates) | Flow C — Session end |
| Lifecycle (`/lifecycle*`) | Memory Lifecycle Manager | Flow C — Consolidate/decay |

---

# Part 1 — REST API Contract

## 1.1 API Overview

### Base URL

```
https://{host}/api/v{major}
```

- Current: `https://memory.example.org/api/v1`
- All paths below are relative to the base URL.
- All timestamps are RFC 3339 UTC (`2026-08-24T09:03:11Z`). All identifiers are UUID v7.

### Authentication

Generic bearer-token model; the concrete identity provider is deployment-specific.

```
Authorization: Bearer <token>          # primary
X-API-Key: <key>                       # accepted alternative for machine clients
```

The token resolves to a principal `{principal_type: user|agent|service, principal_id}`,
which is the access-control subject for every space-scoped check (SharedMemorySpace
`access_policy` + `participants`, data-models §3.5). Requests carry the principal in the
audit trail via `provenance.actor` on writes.

### Content negotiation

- Requests and responses: `Content-Type: application/json; charset=utf-8`
- `Accept` is optional; only `application/json` is produced in v1.
- Unknown fields in request bodies are rejected with `VALIDATION_ERROR` (strict mode) so
  schema evolution is explicit.

### Versioning

- **URI major version** (`/v1`) — breaking changes bump the path.
- **Non-breaking additions** (new optional fields, new enum values flagged `x-extensible`,
  new endpoints) ship continuously within a major.
- Deprecation: responses for deprecated endpoints add
  `Deprecation: true` and `Sunset: <date>` (RFC 8594) headers; two major versions are
  supported concurrently at most.
- Entity-level evolution uses the `version` field of Memory (monotonic, supersession-aware),
  not API versions [source: 06-hybrid-retrieval-strategies].

### Standard error envelope

Every non-2xx response body:

```json
{
  "error": {
    "code": "MEMORY_NOT_FOUND",
    "message": "Memory 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21 does not exist",
    "details": { "resource": "memory", "id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21" },
    "request_id": "req_01J8ZQ...",
    "doc_url": "https://memory.example.org/docs/errors/MEMORY_NOT_FOUND"
  }
}
```

| Field | Type | Notes |
|---|---|---|
| `code` | string | stable machine code from §1.3 |
| `message` | string | human-readable, safe to log |
| `details` | object | optional structured context (constraint name, conflicting version, …) |
| `request_id` | string | echo of `X-Request-Id` or server-generated; use for support |
| `doc_url` | string | versioned documentation anchor |

### Pagination envelope

All list endpoints use opaque cursor pagination (see §4.2):

```json
{
  "items": [ /* entity objects */ ],
  "next_cursor": "eyJvIjoxMDB9",
  "has_more": true,
  "total_estimate": null
}
```

### Common conventions

- **Idempotency:** all POSTs that create state accept `Idempotency-Key: <uuid>`; replays
  return the original response with `Idempotent-Replay: true`.
- **Optimistic concurrency:** mutating a Memory requires `expected_version` (body) or
  `If-Match: "v<n>"`; mismatch → `409 VERSION_CONFLICT`.
- **Async jobs:** long-running operations (async recall, sync, consolidation, decay)
  return `202 Accepted` with a job object and are polled via status endpoints.
- **Access logs:** every request records `{principal_id, method, path, status, latency_ms}`
  — provenance is mandatory in this architecture [source: 02-mem0-universal-memory-layer].
- **Sparse-strategy naming:** the `bm25` strategy is **BM25-like (`ts_rank_cd`)** —
  cover-density ranking, not true BM25 with IDF weighting (F13). Sufficient for most
  recall cases; evaluate ParadeDB `pg_search` only if lexical quality disappoints.
- **Recall budget:** `top_k` **default 50, max 200**, configurable per recall request
  (F6) — 50 covers typical recall depth; 200 accommodates the ~150–200 instruction-slot
  upper bound of agent context budgets.

---

## 1.2 Core Endpoints

Reusable schema fragments (referenced below as `$ref`-style shorthand):

- **`Memory`** — the full entity schema, data-models §3.1 (`id`, `type`, `content`,
  `content_format`, `entities`, `tags`, `embedding`, `provenance`, `source_ref`,
  `created_at`, `updated_at`, `last_accessed_at`, `confidence`, `decay_score`,
  `validity`, `ttl_expires_at`, `version`, `access_scope`, `shared_space_id`,
  `origin_session_id`; `last_accessed_at` is **not** a stored column — it is the
  `memory_access_log` aggregate (F8)).
- **`MemoryLink`** — data-models §3.2 (`id`, `source_id`, `target_id`,
  `relationship_type`, `weight`, `evidence`, `created_at`).
- **`AgentSession`** — data-models §3.4 (`session_id`, `agent_type`, `user_id`,
  `shared_space_id`, `context_window`, `active_memories`, `injection_order`,
  `created_at`, `ended_at`, `summary`).
- **`SharedMemorySpace`** — data-models §3.5 (`id`, `name`, `description`, `owner_type`,
  `owner_id`, `scope`, `participants`, `access_policy`, `storage_backend`, `artifacts`,
  `sync_state`, `last_synced_at`, `retention_policy`, timestamps).
- **`RecallRequest`** — data-models §3.6 (`request_id`, `query`, `context`, `trigger`,
  `agent_id`, `session_id`, `retrieval_params`, `requested_at`).
- **`RecallResult`** — data-models §3.7 (`result_id`, `request_id`, `candidates`,
  `injection_plan`, `slots_used`, `tokens_used`, `latency_ms`).
- **`PromotionProposal`** — data-models §3.8 (`proposal_id`, `shared_space_id`,
  `candidate_memory_id`, `target_artifact`, `diff`, `status`, `proposed_at`,
  `resolved_at`, `reviewer`).

### 1.2.1 Memory Operations

Owned by Memory Encoder + Memory Store + Index Manager (Flow A).

---

#### `POST /memories` — encode and store a new memory

Runs the Memory Encoder: classifies type, extracts entities (unless suppressed), computes
the embedding, persists via the Memory Store, and emits a change event to the Index
Manager for vector/BM25-like (ts_rank_cd)/graph/temporal indexing.

**Request**

| Part | Value |
|---|---|
| Headers | `Authorization`, `Idempotency-Key` (recommended) |
| Body | `MemoryInput` |

```json
{
  "type": "episodic",
  "content": "User corrected the decay policy: supersession-first, deletion reserved for TTL expiry.",
  "content_format": "markdown",
  "tags": ["decay-policy", "correction"],
  "entities": [],
  "extract_entities": true,
  "confidence": 0.72,
  "provenance": {
    "origin": "user_instruction",
    "agent_id": "claude-code",
    "actor": "videogamer"
  },
  "source_ref": { "kind": "file", "path": ".zbot/specs/ai-agent-memory-architecture/plan.md", "hash": "sha256:4f2c…" },
  "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80",
  "ttl_expires_at": null,
  "access_scope": "individual"
}
```

`MemoryInput` schema (create):

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["type", "content", "provenance"],
  "properties": {
    "type": { "enum": ["episodic", "semantic", "procedural"] },
    "content": { "type": "string", "minLength": 1 },
    "content_format": { "enum": ["markdown", "plain", "json"], "default": "markdown" },
    "tags": { "type": "array", "items": { "type": "string", "pattern": "^[a-z0-9][a-z0-9_-]*$" } },
    "entities": { "type": "array", "items": { "type": "string", "format": "uuid" } },
    "extract_entities": { "type": "boolean", "default": true },
    "embedding": {
      "type": "object",
      "description": "Client-supplied embedding; server computes one when omitted",
      "properties": { "vector": { "type": "array", "items": { "type": "number" } }, "model": { "type": "string" }, "dims": { "type": "integer" } }
    },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "provenance": {
      "type": "object",
      "required": ["origin"],
      "description": "origin is client-asserted; agent_id/actor are SERVER-STAMPED from the bearer token / API key principal — clients cannot set them (F21)",
      "properties": {
        "origin": { "enum": ["agent_observation", "user_instruction", "file_artifact", "consolidation"] },
        "agent_id": { "type": "string", "readOnly": true },
        "actor": { "type": "string", "readOnly": true }
      }
    },
    "source_ref": {
      "type": "object",
      "properties": {
        "kind": { "enum": ["file", "url", "tool_call", "message"] },
        "path": { "type": "string" },
        "uri": { "type": "string", "format": "uri" },
        "hash": { "type": "string" }
      }
    },
    "session_id": { "type": "string", "format": "uuid", "description": "sets origin_session_id" },
    "validity": { "type": "object", "properties": { "valid_from": { "type": "string", "format": "date-time" } } },
    "ttl_expires_at": { "type": "string", "format": "date-time" },
    "access_scope": { "enum": ["individual", "shared"], "default": "individual" },
    "shared_space_id": { "type": "string", "format": "uuid", "description": "required iff access_scope=shared; must pass space write policy" }
  }
}
```

**Response**

| Status | Body | Notes |
|---|---|---|
| `201 Created` | `Memory` | `Location: /memories/{id}` |
| `400` | error | `VALIDATION_ERROR` |
| `401` / `403` | error | auth / scope write denied (`SPACE_WRITE_DENIED`) |

**Example response (201)** — truncated to the fields the server fills:

```json
{
  "id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
  "type": "episodic",
  "content": "User corrected the decay policy: supersession-first, deletion reserved for TTL expiry.",
  "entities": ["018f5a2e-6d10-7c3f-9a1b-2e4c8d5f7a30"],
  "embedding": { "vector": [0.021, -0.113], "model": "text-embedding-3-small", "dims": 1536 },
  "confidence": 0.72,
  "decay_score": 1.0,
  "validity": { "valid_from": "2026-08-24T09:00:00Z", "valid_until": null, "superseded_by": null },
  "version": 1,
  "access_scope": "individual",
  "origin_session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80",
  "created_at": "2026-08-24T09:00:00Z",
  "updated_at": "2026-08-24T09:00:00Z"
}
```

---

#### `GET /memories` — list and filter memories

Deterministic listing over the Memory Store (no scoring — use `/recall` for relevance). `sort=last_accessed_at` is served from the `memory_access_log` aggregate (`MAX(accessed_at) GROUP BY memory_id`), not a `memories` column (F8).

**Request**

| Part | Values |
|---|---|
| Query | see table |

| Param | Type | Notes |
|---|---|---|
| `type` | enum list (csv) | `episodic,semantic,procedural` |
| `access_scope` | enum | `individual` \| `shared` |
| `shared_space_id` | uuid | implies `access_scope=shared` |
| `tags` | csv + `tags_match=any\|all` | tag pattern per data-models §3.1 |
| `entity_id` | uuid | entity-anchored filter [source: 02-mem0-universal-memory-layer] |
| `q` | string | full-text (BM25-like path — `ts_rank_cd`, not true BM25; see note below) |
| `created_from` / `created_to` | date-time | inclusive range |
| `updated_from` / `updated_to` | date-time | |
| `min_confidence` / `min_decay_score` | number 0–1 | |
| `valid_at` | date-time | point-in-time validity filter — returns only records whose `validity` interval covers it [source: 03-zep-temporal-knowledge-graph] |
| `include_expired` | boolean (default `false`) | expired = `ttl_expires_at` passed or `validity.valid_until` set |
| `sort` | enum (default `created_at`) | `created_at\|updated_at\|last_accessed_at\|confidence\|decay_score` |
| `order` | enum (default `desc`) | `asc\|desc` |
| `limit` | int (default `50`, max `200`) | |
| `cursor` | opaque | from a prior response |

**Response**

| Status | Body |
|---|---|
| `200 OK` | pagination envelope, `items: Memory[]` |

**Example**

```http
GET /memories?type=semantic&tags=decay-policy&valid_at=2026-08-24T00:00:00Z&limit=2
Authorization: Bearer …
```

```json
{
  "items": [
    {
      "id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
      "type": "semantic",
      "content": "The memory architecture decay policy is supersession-first…",
      "tags": ["architecture", "decay-policy"],
      "confidence": 0.86,
      "decay_score": 0.94,
      "version": 1,
      "access_scope": "individual",
      "created_at": "2026-08-23T23:45:12Z"
    }
  ],
  "next_cursor": null,
  "has_more": false,
  "total_estimate": null
}
```

---

#### `GET /memories/{id}` — fetch a single memory

Does **not** write any `memories` column on read: access is recorded as an append-only row in `memory_access_log` (`access_type='direct_get'`), which feeds recency scoring without write-amplifying the hottest table (F8) [source: 06-hybrid-retrieval-strategies].

**Request**

| Part | Value |
|---|---|
| Path | `id` (uuid) |
| Query | `include_embedding=false`, `include_links=false`, `at_version` (int, optional — read a superseded version) |

**Response**

| Status | Body |
|---|---|
| `200 OK` | `Memory` (+ `links: MemoryLink[]` when `include_links=true`) |
| `404` | error `MEMORY_NOT_FOUND` |
| `410` | error `MEMORY_EXPIRED` (TTL passed or superseded; body still carries `superseded_by` in `details`) |

**Example response (200)** — see §1.2.1 `POST /memories` example for the full shape.

---

#### `PUT /memories/{id}` — update memory attributes

Two modes, both through the Memory Store + Index Manager:

1. **Metadata update** (`confidence`, `decay_score`, `tags`, `ttl_expires_at`) — in-place,
   `version` unchanged.
2. **Content update** (`content`, `entities`, `type`) — supersession semantics: the prior
   content version's validity interval closes, `version` increments, history is retained;
   never a silent overwrite [source: 03-zep-temporal-knowledge-graph, 06-hybrid-retrieval-strategies].

**Request**

| Part | Value |
|---|---|
| Path | `id` (uuid) |
| Headers | `If-Match: "v3"` **or** body `expected_version` |
| Body | `MemoryUpdate` |

```json
{
  "expected_version": 1,
  "confidence": 0.91,
  "decay_score": 0.88,
  "tags": ["architecture", "decay-policy", "canonical"],
  "ttl_expires_at": "2026-11-01T00:00:00Z"
}
```

`MemoryUpdate` schema:

```json
{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "expected_version": { "type": "integer", "minimum": 1 },
    "content": { "type": "string", "minLength": 1 },
    "content_format": { "enum": ["markdown", "plain", "json"] },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "decay_score": { "type": "number", "minimum": 0, "maximum": 1 },
    "tags": { "type": "array", "items": { "type": "string", "pattern": "^[a-z0-9][a-z0-9_-]*$" } },
    "entities": { "type": "array", "items": { "type": "string", "format": "uuid" } },
    "ttl_expires_at": { "type": "string", "format": "date-time" },
    "close_validity": { "type": "boolean", "default": false, "description": "set validity.valid_until=now (manual supersession)" }
  }
}
```

**Response**

| Status | Body | Notes |
|---|---|---|
| `200 OK` | `Memory` | `version` bumped iff content changed |
| `409` | error `VERSION_CONFLICT` | `details.expected_version`, `details.current_version` |
| `404` | error `MEMORY_NOT_FOUND` | |

---

#### `DELETE /memories/{id}` — delete a memory

Not a raw destroy by default — the retention policy of this architecture is
supersession-first [source: 03-zep-temporal-knowledge-graph, 06-hybrid-retrieval-strategies].

**Request**

| Part | Value |
|---|---|
| Path | `id` (uuid) |
| Query | `mode=expire` (default) \| `purge` |
| Headers | `If-Match` (optional; required for `purge`) |

- `expire` — closes `validity.valid_until = now`, keeps record + indexes for audit. Allowed to
  memory owners.
- `purge` — hard delete of record, links, and index entries. Requires `admin` on the owning
  scope; every purge is audit-logged with principal and reason.

**Response**

| Status | Body |
|---|---|
| `204 No Content` | — |
| `403` | error `PURGE_FORBIDDEN` |
| `409` | error `MEMORY_SUPERSEDED` (already closed) |

---

#### `POST /memories/{id}/links` — create a memory link

Creates a directed `MemoryLink` edge (Index Manager). Referential integrity: both endpoints
must exist and be visible to the caller.

**Request**

| Part | Value |
|---|---|
| Path | `id` (uuid) — becomes `source_id` |
| Body | `MemoryLinkInput` |

```json
{
  "target_id": "018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42",
  "relationship_type": "derived_from",
  "weight": 0.91,
  "evidence": "semantic summary consolidated from 4 episodic session records"
}
```

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["target_id", "relationship_type"],
  "properties": {
    "target_id": { "type": "string", "format": "uuid" },
    "target_entity_id": { "type": "string", "format": "uuid", "description": "alternative target when relationship_type=anchors_entity" },
    "relationship_type": { "enum": ["derived_from", "supersedes", "similar_to", "co_occurs_with", "causal_next", "anchors_entity"] },
    "weight": { "type": "number", "minimum": 0, "maximum": 1, "default": 1.0 },
    "evidence": { "type": "string" }
  }
}
```

**Response**

| Status | Body |
|---|---|
| `201 Created` | `MemoryLink` |
| `404` | error `MEMORY_NOT_FOUND` (either endpoint) |
| `422` | error `LINK_INTEGRITY` (self-link, duplicate edge, Memory→Entity type mismatch) |

**Example response (201):** the `MemoryLink` instance from data-models §3.2 verbatim.

---

#### `GET /memories/{id}/links` — list links for a memory

**Request**

| Part | Value |
|---|---|
| Path | `id` (uuid) |
| Query | `direction=outgoing\|incoming\|both` (default `both`), `relationship_type` (csv), `min_weight` (0–1), `limit`, `cursor` |

**Response**

| Status | Body |
|---|---|
| `200 OK` | envelope, `items: MemoryLink[]` |
| `404` | error `MEMORY_NOT_FOUND` |

**Example response (200):**

```json
{
  "items": [
    {
      "id": "018f5a2e-b7c4-7d19-a3e8-5c6f1d2b4e90",
      "source_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
      "target_id": "018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42",
      "relationship_type": "derived_from",
      "weight": 0.91,
      "evidence": "semantic summary consolidated from 4 episodic session records",
      "created_at": "2026-08-23T23:45:12Z"
    }
  ],
  "next_cursor": null,
  "has_more": false,
  "total_estimate": null
}
```

---

### 1.2.2 Recall Operations

Owned by Recall Router + Retrieval Engine (Flow B). Sub-200 ms is the target for the sync
path [source: 03-zep-temporal-knowledge-graph].

---

#### `POST /recall` — submit a recall request

Runs Flow B: router validates trigger/budget → engine fans out over the requested
strategies → merges candidates → cross-encoder re-rank (mandatory in production
[source: 06-hybrid-retrieval-strategies]) → returns result + injection plan.

**Request**

| Part | Value |
|---|---|
| Body | `RecallSubmit` |

```json
{
  "query": "How does this project handle memory decay across sessions?",
  "context": {
    "entities": ["018f5a2e-6d10-7c3f-9a1b-2e4c8d5f7a30"],
    "task_signature": "architecture-design",
    "time_bounds": { "from": "2026-07-01T00:00:00Z", "to": null },
    "mentioned_files": ["pages/architecture.md"]
  },
  "trigger": "user_query",
  "agent_id": "claude-code",
  "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80",
  "retrieval_params": {
    "strategies": ["vector", "bm25", "graph", "temporal"],
    "top_k": 25,
    "rerank": "cross_encoder",
    "min_score": 0.35,
    "slot_budget": 40,
    "token_budget": 4000
  },
  "mode": "sync"
}
```

`RecallSubmit` schema — identical to `RecallRequest` (data-models §3.6) minus server-assigned
fields (`request_id`, `requested_at`), plus `mode`:

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["query", "context", "trigger", "agent_id", "session_id", "retrieval_params"],
  "properties": {
    "query": { "type": "string", "minLength": 1 },
    "context": {
      "type": "object",
      "properties": {
        "entities": { "type": "array", "items": { "type": "string", "format": "uuid" } },
        "task_signature": { "type": "string" },
        "time_bounds": { "type": "object", "properties": { "from": { "type": "string", "format": "date-time" }, "to": { "type": "string", "format": "date-time" } } },
        "mentioned_files": { "type": "array", "items": { "type": "string" } }
      }
    },
    "trigger": { "enum": ["task_context", "user_query", "temporal", "associative", "session_start"] },
    "agent_id": { "type": "string" },
    "session_id": { "type": "string", "format": "uuid" },
    "retrieval_params": {
      "type": "object",
      "required": ["strategies", "top_k", "rerank"],
      "properties": {
        "strategies": { "type": "array", "minItems": 1, "items": { "enum": ["vector", "bm25", "graph", "temporal"] } },
        "top_k": { "type": "integer", "minimum": 1, "maximum": 200, "default": 50 },
        "rerank": { "enum": ["cross_encoder", "none"], "default": "cross_encoder" },
        "min_score": { "type": "number", "minimum": 0, "maximum": 1 },
        "slot_budget": { "type": "integer", "minimum": 0 },
        "token_budget": { "type": "integer", "minimum": 0 }
      }
    },
    "mode": { "enum": ["sync", "async"], "default": "sync" }
  }
}
```

**Response**

| Status | Body | Notes |
|---|---|---|
| `200 OK` | `{ "request": RecallRequest, "result": RecallResult }` | sync path |
| `202 Accepted` | `{ "request": RecallRequest, "result": null, "status": "queued", "poll_after_ms": 250 }` | async path (`mode=async`, or server-upgraded — see §4.1) |
| `422` | error `RECALL_BUDGET_INVALID` | slot/token budget ≤ 0, or exceeds session budget |
| `404` | error `SESSION_NOT_FOUND` | unknown `session_id` |

**Example response (200, sync):**

```json
{
  "request": {
    "request_id": "018f5a2e-f1d5-74c8-b2a7-3d9e5c1f7a62",
    "query": "How does this project handle memory decay across sessions?",
    "trigger": "user_query",
    "requested_at": "2026-08-24T09:03:11Z"
  },
  "result": {
    "result_id": "018f5a2e-02b9-76da-c8f4-1a3e7b5c9d84",
    "request_id": "018f5a2e-f1d5-74c8-b2a7-3d9e5c1f7a62",
    "candidates": [
      { "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "score": 0.81, "rerank_score": 0.94, "source_strategies": ["vector", "graph"] },
      { "memory_id": "018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42", "score": 0.62, "rerank_score": 0.71, "source_strategies": ["bm25", "temporal"] }
    ],
    "injection_plan": [
      { "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "position": 1, "slot_cost": 2 },
      { "memory_id": "018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42", "position": 2, "slot_cost": 1 }
    ],
    "slots_used": 3,
    "tokens_used": 412,
    "latency_ms": 187
  }
}
```

The response intentionally does **not** inline memory contents: the client hydrates via
`GET /memories/{id}` or the MCP `recall` tool (which does inline — see §2 / §4.2).

---

#### `GET /recall/{request_id}` — poll async recall status/result

**Request**

| Part | Value |
|---|---|
| Path | `request_id` (uuid) |
| Query | `include_memories=false` (inline hydrated memories into `result`) |

**Response**

| Status | Body |
|---|---|
| `200 OK` | `{ "request": RecallRequest, "status": "queued\|running\|completed\|failed", "result": RecallResult \| null, "error": error \| null }` |
| `404` | error `RECALL_REQUEST_NOT_FOUND` |

**Example response (200, running):**

```json
{
  "request": { "request_id": "018f5a2e-f1d5-74c8-b2a7-3d9e5c1f7a62", "query": "…", "requested_at": "2026-08-24T09:03:11Z" },
  "status": "running",
  "result": null,
  "error": null
}
```

---

### 1.2.3 Session Operations

Owned by the Recall Router (injection plan + budgets) over AgentSession state.

---

#### `POST /sessions` — create an agent session

Creates the `AgentSession` and fires the session-start bootstrap (Flow D): fixed-order
procedural → conditional → semantic → episodic injection [source: 07-claude-code-memory,
02-mem0-universal-memory-layer]. The bootstrap injection plan is returned alongside the session.

**Request**

| Part | Value |
|---|---|
| Body | `SessionInput` |

```json
{
  "agent_type": "claude-code",
  "user_id": "videogamer",
  "shared_space_id": null,
  "context_window": {
    "model": "claude-sonnet-4.6",
    "max_tokens": 200000,
    "instruction_slot_budget": 150
  },
  "bootstrap": true
}
```

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["agent_type", "user_id", "context_window"],
  "properties": {
    "agent_type": { "enum": ["claude-code", "codex", "cursor", "letta", "custom"] },
    "user_id": { "type": "string", "minLength": 1 },
    "shared_space_id": { "type": "string", "format": "uuid" },
    "context_window": {
      "type": "object",
      "required": ["model", "max_tokens", "instruction_slot_budget"],
      "properties": {
        "model": { "type": "string" },
        "max_tokens": { "type": "integer", "minimum": 1 },
        "instruction_slot_budget": { "type": "integer", "minimum": 0 }
      }
    },
    "bootstrap": { "type": "boolean", "default": true, "description": "run Flow D session-start retrieval" }
  }
}
```

**Response**

| Status | Body |
|---|---|
| `201 Created` | `{ "session": AgentSession, "bootstrap_plan": InjectionPlanItem[] \| null }` |
| `403` | error `SPACE_ACCESS_DENIED` (cannot join `shared_space_id`) |

**Example response (201):**

```json
{
  "session": {
    "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80",
    "agent_type": "claude-code",
    "user_id": "videogamer",
    "shared_space_id": null,
    "context_window": { "model": "claude-sonnet-4.6", "max_tokens": 200000, "used_tokens": 84512, "instruction_slot_budget": 150, "slots_used": 61 },
    "active_memories": ["018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21"],
    "injection_order": ["global_claude_md", "project_claude_md", "project_rules", "conditional_rules"],
    "created_at": "2026-08-24T09:01:58Z",
    "ended_at": null,
    "summary": null
  },
  "bootstrap_plan": [
    { "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "position": 1, "slot_cost": 2 }
  ]
}
```

---

#### `GET /sessions/{session_id}` — get session state

**Request**

| Part | Value |
|---|---|
| Path | `session_id` (uuid) |

**Response**

| Status | Body |
|---|---|
| `200 OK` | `{ "session": AgentSession, "usage": { "tokens_remaining", "slots_remaining" } }` |
| `404` | error `SESSION_NOT_FOUND` |

**Example response (200):**

```json
{
  "session": {
    "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80",
    "agent_type": "claude-code",
    "user_id": "videogamer",
    "context_window": { "model": "claude-sonnet-4.6", "max_tokens": 200000, "used_tokens": 84512, "instruction_slot_budget": 150, "slots_used": 61 },
    "active_memories": ["018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "018f5a2e-aa57-7f22-d5e9-4c8b2a6d1f03"],
    "injection_order": ["global_claude_md", "project_claude_md", "project_rules", "conditional_rules"],
    "created_at": "2026-08-24T09:01:58Z",
    "ended_at": null,
    "summary": null
  },
  "usage": { "tokens_remaining": 115488, "slots_remaining": 89 }
}
```

---

#### `POST /sessions/{session_id}/memories` — activate a memory in the session

Adds `memory_id` to `active_memories` and returns its injection placement. Enforces the
dual budget — instruction slots, not just tokens [source: 07-claude-code-memory].

**Request**

| Part | Value |
|---|---|
| Path | `session_id` (uuid) |
| Body | `{ "memory_id": uuid, "position": int?, "force": false }` |

```json
{ "memory_id": "018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42", "position": 2 }
```

**Response**

| Status | Body | Notes |
|---|---|---|
| `200 OK` | `{ "session": AgentSession, "injection": { "memory_id", "position", "slot_cost" } }` | |
| `409` | error `SLOT_BUDGET_EXCEEDED` / `TOKEN_BUDGET_EXCEEDED` | `details.{needed, remaining}`; bypass only with `force: true` |
| `404` | error `SESSION_NOT_FOUND` / `MEMORY_NOT_FOUND` | |

---

#### `DELETE /sessions/{session_id}/memories/{memory_id}` — deactivate a memory

Removes from `active_memories`; the memory record itself is untouched (working tier only).

**Request**

| Part | Value |
|---|---|
| Path | `session_id`, `memory_id` |

**Response**

| Status | Body |
|---|---|
| `204 No Content` | — |
| `404` | error `SESSION_NOT_FOUND` / `MEMORY_NOT_ACTIVE` |

---

#### `POST /sessions/{session_id}/end` — end session, trigger consolidation

Closes the session and enqueues the Flow C pipeline: compact conversation (recursive
summary — nothing is truly lost [source: 01-letta-memgpt-stateful-agents]), consolidate
episodic set → semantic candidates, expire/supersede, then hand promotion candidates to
the Shared Memory Sync Layer.

**Request**

| Part | Value |
|---|---|
| Path | `session_id` (uuid) |
| Body | `{ "summary": string?, "consolidate": true }` |

```json
{ "summary": "Session designed API contracts for the memory service.", "consolidate": true }
```

**Response**

| Status | Body |
|---|---|
| `202 Accepted` | `{ "session": AgentSession (ended_at set), "consolidation_job": JobRef \| null }` |
| `404` | error `SESSION_NOT_FOUND` |
| `409` | error `SESSION_ALREADY_ENDED` |

`JobRef` = `{ "job_id": uuid, "kind": "consolidation", "status": "queued", "poll_url": "/lifecycle/jobs/{job_id}" }`.

**Example response (202):**

```json
{
  "session": { "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80", "ended_at": "2026-08-24T10:44:02Z", "summary": "Session designed API contracts for the memory service." },
  "consolidation_job": { "job_id": "018f5a2e-6c1f-72ab-9e5d-4b7c8d9e0f11", "kind": "consolidation", "status": "queued", "poll_url": "/lifecycle/jobs/018f5a2e-6c1f-72ab-9e5d-4b7c8d9e0f11" }
}
```

---

### 1.2.4 SharedMemorySpace Operations

Owned by the Shared Memory Sync Layer. Shared memory is review-gated by default
[source: 10-spec-driven-development-memory].

---

#### `POST /spaces` — create a shared memory space

**Request**

| Part | Value |
|---|---|
| Body | `SpaceInput` |

```json
{
  "name": "acme/backend",
  "description": "Shared engineering memory for the backend repo: specs, agent rules, decisions",
  "owner_type": "team",
  "owner_id": "team:acme-backend",
  "scope": "software-development",
  "participants": [
    { "principal_type": "group", "principal_id": "acme/backend-committers", "access_level": "promote" },
    { "principal_type": "user", "principal_id": "videogamer", "access_level": "write" }
  ],
  "access_policy": { "default_access": "read", "write": "owner_approved", "promote": "human_review" },
  "storage_backend": { "kind": "files", "config_ref": "git:acme/backend" },
  "retention_policy": { "supersede_not_delete": true }
}
```

`SpaceInput` = `SharedMemorySpace` minus server-assigned `id`, `sync_state`,
`last_synced_at`, timestamps (all initialized: `sync_state.status: "in_sync"`,
`pending_proposals: 0`, `revision: null`).

**Response**

| Status | Body |
|---|---|
| `201 Created` | `SharedMemorySpace` — the instance from data-models §3.5 verbatim |
| `422` | error `SPACE_POLICY_INVALID` (e.g. `promote: auto` with `write: proposal_only` and no reviewers configured) |

---

#### `GET /spaces` — list spaces

**Request**

| Part | Value |
|---|---|
| Query | `owner_type`, `scope` (csv), `participant_id` (filter to spaces a principal can see), `sync_status`, `limit`, `cursor` |

Only spaces the caller's principal can read (via `participants` or `default_access: read`)
are returned.

**Response:** `200` — envelope, `items: SharedMemorySpace[]`.

**Example response (200, item truncated):**

```json
{
  "items": [
    { "id": "018f5a2e-c0de-71ab-93f5-6e2a8c4d0b17", "name": "acme/backend", "owner_type": "team", "scope": "software-development", "sync_state": { "status": "in_sync", "pending_proposals": 0, "revision": "main@9f3c2ab" }, "last_synced_at": "2026-08-24T08:55:00Z" }
  ],
  "next_cursor": null,
  "has_more": false,
  "total_estimate": null
}
```

---

#### `GET /spaces/{space_id}` — get space details

**Response:** `200` — full `SharedMemorySpace` (artifacts manifest included); `404` — `SPACE_NOT_FOUND` / `SPACE_ACCESS_DENIED`.

---

#### `PUT /spaces/{space_id}` — update space policy

Only `admin`-level principals (or the owner). Policy changes are themselves auditable events.

**Request**

| Part | Value |
|---|---|
| Body | `SpaceUpdate` |

```json
{
  "access_policy": { "default_access": "none", "write": "proposal_only", "promote": "human_review" },
  "retention_policy": { "supersede_not_delete": true, "archive_after_days": 365 },
  "participants_append": [{ "principal_type": "user", "principal_id": "dana", "access_level": "promote" }]
}
```

```json
{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "description": { "type": "string" },
    "access_policy": {
      "type": "object",
      "required": ["default_access", "write", "promote"],
      "properties": {
        "default_access": { "enum": ["read", "write", "none"] },
        "write": { "enum": ["owner_approved", "participant_free", "proposal_only"] },
        "promote": { "enum": ["human_review", "auto"] }
      }
    },
    "retention_policy": {
      "type": "object",
      "properties": { "supersede_not_delete": { "type": "boolean" }, "ttl_days": { "type": "integer", "minimum": 1 }, "archive_after_days": { "type": "integer", "minimum": 1 } }
    },
    "participants_append": { "type": "array", "items": { "$comment": "participant object, data-models §3.5" } },
    "participants_remove": { "type": "array", "items": { "type": "string" } }
  }
}
```

**Response:** `200` — updated `SharedMemorySpace`; `403` — `SPACE_ADMIN_REQUIRED`; `404` — `SPACE_NOT_FOUND`.

---

#### `POST /spaces/{space_id}/memories` — propose a promotion

Promotes an individual (usually consolidated semantic) memory into the shared space by
creating a `PromotionProposal` carrying a reviewable diff [source: 10-spec-driven-development-memory].
Behavior depends on `access_policy.promote`:

- `human_review` (default) → proposal created `in_review`; merge only via approve.
- `auto` → proposal created and immediately `merged` (still recorded, for audit).

**Request**

| Part | Value |
|---|---|
| Path | `space_id` (uuid) |
| Body | `PromotionInput` |

```json
{
  "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
  "target_artifact": { "path": "specs/002-agent-memory/spec.md", "kind": "spec", "role": "semantic" },
  "note": "Consolidated from 4 episodic records across 2 sessions"
}
```

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["memory_id"],
  "properties": {
    "memory_id": { "type": "string", "format": "uuid" },
    "target_artifact": {
      "type": "object",
      "properties": {
        "path": { "type": "string" },
        "kind": { "enum": ["spec", "rule", "agent_doc", "memory_doc"] },
        "role": { "enum": ["procedural", "semantic", "episodic"] }
      }
    },
    "note": { "type": "string" }
  }
}
```

When `target_artifact` is omitted, the Sync Layer infers it from the memory `type`
(semantic → spec/memory_doc, procedural → rule/agent_doc).

**Response**

| Status | Body |
|---|---|
| `201 Created` | `PromotionProposal` (`in_review`, or `merged` under `auto`) |
| `403` | error `SPACE_PROMOTE_DENIED` (no `promote`/`write` access, or policy is `proposal_only` for direct writes) |
| `422` | error `PROMOTION_NOT_CONSOLIDATED` (episodic candidate without consolidation lineage — see §4.4) |

---

#### `GET /spaces/{space_id}/proposals` — list proposals

**Request**

| Part | Value |
|---|---|
| Query | `status=draft\|in_review\|merged\|rejected` (default `in_review`), `limit`, `cursor` |

**Response:** `200` — envelope, `items: PromotionProposal[]`; `404`/`403` as above.

---

#### `POST /spaces/{space_id}/proposals/{proposal_id}/approve` — approve a promotion

Reviewer action. Merges the diff: commits/updates the target artifact, flips the candidate
memory to `access_scope: shared` + `shared_space_id`, bumps `sync_state.revision`, and
notifies active agents to invalidate caches (Flow C tail).

**Request**

| Part | Value |
|---|---|
| Body | `{ "reviewer": "videogamer", "note": "LGTM" }` |

**Response**

| Status | Body |
|---|---|
| `200 OK` | `PromotionProposal` (`status: "merged"`, `resolved_at`, `reviewer` set) |
| `409` | error `PROPOSAL_ALREADY_RESOLVED` |
| `403` | error `SPACE_PROMOTE_DENIED` (caller lacks promote/admin) |

---

#### `POST /spaces/{space_id}/proposals/{proposal_id}/reject` — reject a promotion

**Request:** body `{ "reviewer": "videogamer", "reason": "superseded by specs/003" }` — `reason` required (audit trail).

**Response:** `200` — `PromotionProposal` (`status: "rejected"`); same `409`/`403` errors as approve.

---

#### `POST /spaces/{space_id}/sync` — trigger sync

Reconciles the space with its `storage_backend` (git pull/push for files backends,
graph/vector epoch sync for hybrid) and refreshes `sync_state`. Inherently slow → async.

**Request:** body `{ "direction": "pull|push|both" }` (default `both`).

**Response**

| Status | Body |
|---|---|
| `202 Accepted` | `{ "job": { "job_id", "kind": "space_sync", "status": "queued", "poll_url": "/lifecycle/jobs/{job_id}" }, "space_id": uuid }` |
| `409` | error `SPACE_SYNC_IN_PROGRESS` |
| `422` | error `SPACE_DIVERGED` (backend conflict — requires manual resolution; `details.revision_local`, `details.revision_remote`) |

---

### 1.2.5 Lifecycle Operations

Owned by the Memory Lifecycle Manager. Batch operations over a corpus → always async jobs.

---

#### `POST /lifecycle/consolidate` — trigger consolidation

Merges similar memories into semantic candidates, updates decay scores, expires TTL'd
records, supersedes invalidated facts (Flow C).

**Request**

| Part | Value |
|---|---|
| Body | `ConsolidateInput` |

```json
{
  "scope": { "kind": "session", "id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80" },
  "memory_types": ["episodic"],
  "dry_run": false
}
```

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["scope"],
  "properties": {
    "scope": {
      "type": "object",
      "required": ["kind"],
      "properties": {
        "kind": { "enum": ["session", "user", "space", "all"] },
        "id": { "type": "string", "description": "session_id / user_id / space_id; omitted for all" }
      }
    },
    "memory_types": { "type": "array", "items": { "enum": ["episodic", "semantic", "procedural"] } },
    "dry_run": { "type": "boolean", "default": false, "description": "return planned MutationOps without applying" }
  }
}
```

**Response**

| Status | Body |
|---|---|
| `202 Accepted` | `{ "job": JobRef (kind=consolidation) }` |
| `403` | error `LIFECYCLE_SCOPE_FORBIDDEN` |

---

#### `POST /lifecycle/decay` — trigger decay pass

Reduces `confidence`/`decay_score` on stale memories per the decay profile; TTL'd records
expire (supersession-first — intervals close, records persist).

**Request**

| Part | Value |
|---|---|
| Body | `{ "scope": {…as above}, "decay_profile": "standard|aggressive", "min_age_days": 7 }` |

**Response:** `202` — `{ "job": JobRef (kind=decay) }`.

---

#### `GET /lifecycle/jobs/{job_id}` — poll a lifecycle job *(auxiliary)*

Shared status endpoint for consolidation, decay, sync, and session-end jobs.

**Response (200):**

```json
{
  "job_id": "018f5a2e-6c1f-72ab-9e5d-4b7c8d9e0f11",
  "kind": "consolidation",
  "status": "completed",
  "created_at": "2026-08-24T10:44:03Z",
  "finished_at": "2026-08-24T10:44:41Z",
  "result": {
    "merged": 12,
    "superseded": 4,
    "expired_ttl": 2,
    "semantic_candidates": 5,
    "promotion_proposals_created": 1
  }
}
```

`status`: `queued | running | completed | failed`; `result` shape varies by `kind`.

---

#### `GET /lifecycle/stats` — memory statistics

**Request**

| Part | Value |
|---|---|
| Query | `scope=global\|user:<id>\|space:<uuid>` (default `global`), `include_index_health=true` |

**Response (200):**

```json
{
  "scope": "global",
  "generated_at": "2026-08-24T11:00:00Z",
  "counts": {
    "total": 15231,
    "by_type": { "episodic": 9812, "semantic": 4103, "procedural": 1316 },
    "by_access_scope": { "individual": 12988, "shared": 2243 },
    "expired": 187,
    "superseded": 402
  },
  "quality": {
    "avg_confidence": 0.74,
    "avg_decay_score": 0.81,
    "low_decay_below_03": 611
  },
  "links": { "total": 41290, "by_relationship_type": { "similar_to": 22110, "co_occurs_with": 11005, "derived_from": 5312, "supersedes": 1601, "causal_next": 902, "anchors_entity": 360 } },
  "spaces": { "total": 14, "pending_proposals": 3 },
  "index_health": {
    "vector": { "status": "fresh", "lag_seconds": 2 },
    "bm25": { "status": "fresh", "lag_seconds": 1 },
    "graph": { "status": "fresh", "lag_seconds": 4 },
    "temporal": { "status": "stale", "lag_seconds": 92 }
  }
}
```

---

## 1.3 Error Codes

| HTTP | `code` | Meaning |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Body/query failed schema validation; `details.fields[]` |
| 401 | `UNAUTHENTICATED` | Missing/invalid/expired token |
| 403 | `SPACE_ACCESS_DENIED` | Principal cannot read the space |
| 403 | `SPACE_WRITE_DENIED` | Write not permitted by `access_policy.write` |
| 403 | `SPACE_PROMOTE_DENIED` | No promote/admin right on the space |
| 403 | `SPACE_ADMIN_REQUIRED` | Space update requires admin/owner |
| 403 | `PURGE_FORBIDDEN` | Hard delete requires admin |
| 403 | `LIFECYCLE_SCOPE_FORBIDDEN` | Batch scope exceeds principal's reach |
| 404 | `MEMORY_NOT_FOUND` / `SESSION_NOT_FOUND` / `SPACE_NOT_FOUND` / `RECALL_REQUEST_NOT_FOUND` / `PROPOSAL_NOT_FOUND` | Unknown resource id |
| 409 | `VERSION_CONFLICT` | `expected_version` ≠ current `version` |
| 409 | `SESSION_ALREADY_ENDED` | Operation on ended session |
| 409 | `PROPOSAL_ALREADY_RESOLVED` | Approve/reject after merge/reject |
| 409 | `SPACE_SYNC_IN_PROGRESS` | Sync job already running |
| 409 | `SPACE_DIVERGED` | Local/backend revision conflict |
| 409 | `SLOT_BUDGET_EXCEEDED` / `TOKEN_BUDGET_EXCEEDED` | Session budget exhausted (`details.{needed, remaining}`) |
| 410 | `MEMORY_EXPIRED` | TTL passed or superseded; `details.superseded_by` |
| 410 | `MEMORY_SUPERSEDED` | Delete/expire on already-closed record |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Not `application/json` |
| 422 | `LINK_INTEGRITY` | Self-link, duplicate edge, or endpoint-type mismatch |
| 422 | `SPACE_POLICY_INVALID` | Contradictory access/retention policy |
| 422 | `PROMOTION_NOT_CONSOLIDATED` | Episodic candidate lacking consolidation lineage |
| 422 | `RECALL_BUDGET_INVALID` | Retrieval budgets missing/negative/over session budget |
| 429 | `RATE_LIMITED` | Quota exceeded; `Retry-After` header set |
| 500 | `INTERNAL` | Unspecified server fault; `request_id` for tracing |
| 503 | `STORE_UNAVAILABLE` | Memory Store / index backend unreachable |
| 503 | `SYNC_BACKEND_UNAVAILABLE` | Space storage backend (git, graph) unreachable |

Error bodies always use the §1.1 envelope. Codes are stable strings; new codes may be
added within a major version, clients must tolerate unknown codes.

---

# Part 2 — MCP Server Contract

## 2.0 Server Overview

The MCP server exposes the memory system to AI agents as tools and read-only resources.
This is the interface the research identifies as the primary integration path for coding
agents: persistent memory for Codex CLI is added via MCP integrations, and GitHub
Copilot's memory feature addresses the same "start each session from zero" limitation
[source: 08-codex-copilot-persistent-memory].

**Transport & session context.** The server speaks MCP over `stdio` (local agent
processes) or `streamable-http` (remote). Unlike REST, MCP tools do not take a token per
call: identity is established once at connection initialization (client info +
`x-principal` handshake or OAuth on http transport) and becomes the *implicit principal*
for every tool call and resource read. Session-scoped tools (`activate_memory`,
`deactivate_memory`) operate on the **active session** bound at the connection — the
`session_id` parameter is omitted entirely (see §4.5).

**Tool naming.** All tools are `snake_case`; all inputs/outputs are JSON Schema
(2020-12), matching the entity schemas of [[pages/data-models|Data Models]].

**Result shape.** MCP tool results in this contract are returned as structured content:

```json
{ "content": [{ "type": "text", "text": "<compact summary>" }], "structuredContent": { …output schema… } }
```

Examples below show only `structuredContent`. Errors are MCP tool errors carrying the
REST error `code` from §1.3 as `data.error.code` — one error taxonomy across protocols.

## 2.1 Memory Tools

---

### `save_memory`

Encode and store a new memory. Use when the agent observes a durable fact, correction, or
pattern worth persisting. Episodic observations are cheap and should be saved liberally;
semantic claims require repetition or explicit consolidation (encoder policy).

**Input schema:**

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["type", "content"],
  "properties": {
    "type": { "enum": ["episodic", "semantic", "procedural"] },
    "content": { "type": "string", "minLength": 1 },
    "content_format": { "enum": ["markdown", "plain", "json"], "default": "markdown" },
    "tags": { "type": "array", "items": { "type": "string", "pattern": "^[a-z0-9][a-z0-9_-]*$" } },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "origin": { "enum": ["agent_observation", "user_instruction", "file_artifact", "consolidation"], "default": "agent_observation" },
    "source_ref": {
      "type": "object",
      "properties": { "kind": { "enum": ["file", "url", "tool_call", "message"] }, "path": { "type": "string" }, "hash": { "type": "string" } }
    },
    "access_scope": { "enum": ["individual", "shared"], "default": "individual" },
    "shared_space_id": { "type": "string", "format": "uuid" }
  }
}
```

(`provenance.agent_id`, `provenance.actor`, and `provenance.session_id` are filled from connection context, not client input — the input schema deliberately excludes them; agents cannot forge provenance. `origin` remains client-asserted.)

**Output schema:**

```json
{
  "type": "object",
  "required": ["memory"],
  "properties": {
    "memory": { "$ref": "memory.schema.json", "description": "the full stored Memory record" }
  }
}
```

**Example invocation:**

```json
{ "type": "semantic", "content": "User prefers supersession-first decay policy; deletion only on TTL expiry.", "tags": ["decay-policy", "user-preference"], "confidence": 0.72 }
```

**Example result (structuredContent):**

```json
{
  "memory": {
    "id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
    "type": "semantic",
    "content": "User prefers supersession-first decay policy; deletion only on TTL expiry.",
    "tags": ["decay-policy", "user-preference"],
    "confidence": 0.72,
    "decay_score": 1.0,
    "version": 1,
    "access_scope": "individual",
    "provenance": { "origin": "agent_observation", "agent_id": "claude-code", "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80", "actor": "agent" },
    "created_at": "2026-08-24T09:00:00Z"
  }
}
```

---

### `get_memory`

Retrieve one memory by ID with full metadata. Use when a recall result or link references
an ID whose content you need.

**Input:** `{ "memory_id": uuid, "include_links": boolean (default false) }`

**Output:** `{ "memory": Memory, "links"?: MemoryLink[] }`

**Example:** input `{ "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21" }` → output is
the Memory instance of data-models §3.1 under `memory`.

---

### `list_memories`

Filter and list memories deterministically (no relevance scoring — use `recall` for that).
Use for browsing a tag, entity, or time window.

**Input schema:**

```json
{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "type": { "enum": ["episodic", "semantic", "procedural"] },
    "tags": { "type": "array", "items": { "type": "string" } },
    "tags_match": { "enum": ["any", "all"], "default": "any" },
    "entity_id": { "type": "string", "format": "uuid" },
    "access_scope": { "enum": ["individual", "shared"] },
    "shared_space_id": { "type": "string", "format": "uuid" },
    "created_after": { "type": "string", "format": "date-time" },
    "created_before": { "type": "string", "format": "date-time" },
    "min_confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "batch_size": { "type": "integer", "minimum": 1, "maximum": 50, "default": 20 },
    "cursor": { "type": "string", "description": "opaque; pass next_cursor from a prior call" }
  }
}
```

**Output schema:**

```json
{
  "type": "object",
  "required": ["items", "has_more"],
  "properties": {
    "items": { "type": "array", "items": { "$ref": "memory.schema.json" } },
    "has_more": { "type": "boolean" },
    "next_cursor": { "type": "string" },
    "batch_size": { "type": "integer" }
  }
}
```

**Example:** input `{ "type": "semantic", "tags": ["decay-policy"], "batch_size": 10 }` →

```json
{ "items": [ { "id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "type": "semantic", "content": "The memory architecture decay policy is supersession-first…", "tags": ["architecture", "decay-policy"], "confidence": 0.86, "created_at": "2026-08-23T23:45:12Z" } ], "has_more": false, "batch_size": 10 }
```

---

### `update_memory`

Update mutable attributes. Content edits trigger supersession (new version, old validity
interval closed — history preserved). Use sparingly; prefer saving a new memory and
linking `supersedes` when lineage matters.

**Input:** `{ "memory_id": uuid, "expected_version": int, "confidence"?: 0–1, "decay_score"?: 0–1, "tags"?: string[], "content"?: string, "ttl_expires_at"?: date-time, "close_validity"?: bool }`

**Output:** `{ "memory": Memory }` — post-update record, `version` bumped iff content changed.

**Example:** input `{ "memory_id": "018f5a2e-…9d21", "expected_version": 1, "confidence": 0.91 }` → output `{ "memory": { …, "confidence": 0.91, "version": 1 } }`.

---

### `delete_memory`

Soft-expire a memory by default (closes its validity interval; retained for audit).
Hard purge requires elevated context and is normally a human/REST action — see §4.3.

**Input:** `{ "memory_id": uuid, "mode": { "enum": ["expire", "purge"], "default": "expire" }, "reason"?: string }`

**Output:** `{ "memory_id": uuid, "mode": "expire|purge", "expired_at"?: date-time, "purged": bool }`

**Example:** input `{ "memory_id": "018f5a2e-…9d21", "mode": "expire", "reason": "user retracted preference" }` →

```json
{ "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "mode": "expire", "expired_at": "2026-08-24T11:20:00Z", "purged": false }
```

---

### `link_memories`

Create a directed relationship between two memories (or memory → entity). Use to record
that one memory derives from, supersedes, or co-occurs with another — these edges feed the
graph retrieval path.

**Input schema:**

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["source_id", "target_id", "relationship_type"],
  "properties": {
    "source_id": { "type": "string", "format": "uuid" },
    "target_id": { "type": "string", "format": "uuid" },
    "target_entity_id": { "type": "string", "format": "uuid" },
    "relationship_type": { "enum": ["derived_from", "supersedes", "similar_to", "co_occurs_with", "causal_next", "anchors_entity"] },
    "weight": { "type": "number", "minimum": 0, "maximum": 1, "default": 1.0 },
    "evidence": { "type": "string" }
  }
}
```

**Output:** `{ "link": MemoryLink }`

**Example:** input `{ "source_id": "018f5a2e-…9d21", "target_id": "018f5a2e-…7d42", "relationship_type": "derived_from", "weight": 0.91, "evidence": "consolidated from 4 episodic records" }` → output carries the §3.2 `MemoryLink` instance under `link`.

## 2.2 Recall Tools

---

### `recall`

Submit a recall request and get relevant memories, hydrated. This is the agent's primary
read path: hybrid retrieval (vector ∥ BM25-like ts_rank_cd ∥ graph ∥ temporal) with cross-encoder
re-ranking, sized to the session's slot/token budgets. Use at session start
(`trigger: session_start`), on user queries, or when a task context suggests it.

Unlike REST `POST /recall`, this tool **inlines memory contents** in the result so the
agent consumes one tool result instead of N follow-up `get_memory` calls (§4.2).

**Input schema:** same as REST `RecallSubmit` minus `agent_id`/`session_id`
(connection-context-bound) and minus `mode`:

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["query", "trigger"],
  "properties": {
    "query": { "type": "string", "minLength": 1 },
    "context": {
      "type": "object",
      "properties": {
        "entities": { "type": "array", "items": { "type": "string", "format": "uuid" } },
        "task_signature": { "type": "string" },
        "time_bounds": { "type": "object", "properties": { "from": { "type": "string", "format": "date-time" }, "to": { "type": "string", "format": "date-time" } } },
        "mentioned_files": { "type": "array", "items": { "type": "string" } }
      }
    },
    "trigger": { "enum": ["task_context", "user_query", "temporal", "associative", "session_start"] },
    "retrieval_params": {
      "type": "object",
      "properties": {
        "strategies": { "type": "array", "minItems": 1, "items": { "enum": ["vector", "bm25", "graph", "temporal"] }, "default": ["vector", "bm25", "graph", "temporal"] },
        "top_k": { "type": "integer", "minimum": 1, "maximum": 200, "default": 50 },
        "rerank": { "enum": ["cross_encoder", "none"], "default": "cross_encoder" },
        "min_score": { "type": "number", "minimum": 0, "maximum": 1 },
        "slot_budget": { "type": "integer", "minimum": 0 },
        "token_budget": { "type": "integer", "minimum": 0 }
      }
    }
  }
}
```

**Output schema:**

```json
{
  "type": "object",
  "required": ["request_id", "candidates"],
  "properties": {
    "request_id": { "type": "string", "format": "uuid" },
    "candidates": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["memory", "score"],
        "properties": {
          "memory": { "$ref": "memory.schema.json", "description": "hydrated memory record, embedding omitted" },
          "score": { "type": "number" },
          "rerank_score": { "type": "number" },
          "source_strategies": { "type": "array", "items": { "enum": ["vector", "bm25", "graph", "temporal"] } }
        }
      }
    },
    "injection_plan": { "type": "array", "items": { "$ref": "injection-plan-item.schema.json" } },
    "slots_used": { "type": "integer" },
    "tokens_used": { "type": "integer" },
    "latency_ms": { "type": "integer" }
  }
}
```

**Example invocation:**

```json
{
  "query": "How does this project handle memory decay across sessions?",
  "trigger": "user_query",
  "context": { "entities": ["018f5a2e-6d10-7c3f-9a1b-2e4c8d5f7a30"], "mentioned_files": ["pages/architecture.md"] },
  "retrieval_params": { "strategies": ["vector", "bm25", "graph", "temporal"], "top_k": 10, "rerank": "cross_encoder", "min_score": 0.35, "slot_budget": 40 }
}
```

**Example result (structuredContent):**

```json
{
  "request_id": "018f5a2e-f1d5-74c8-b2a7-3d9e5c1f7a62",
  "candidates": [
    {
      "memory": {
        "id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
        "type": "semantic",
        "content": "The memory architecture decay policy is supersession-first: close the temporal validity interval and open a new version; deletion is reserved for TTL expiry.",
        "tags": ["architecture", "decay-policy"],
        "confidence": 0.86,
        "decay_score": 0.94,
        "access_scope": "individual",
        "created_at": "2026-08-23T23:45:12Z"
      },
      "score": 0.81,
      "rerank_score": 0.94,
      "source_strategies": ["vector", "graph"]
    }
  ],
  "injection_plan": [ { "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "position": 1, "slot_cost": 2 } ],
  "slots_used": 2,
  "tokens_used": 214,
  "latency_ms": 187
}
```

---

### `recall_async`

Submit a recall request for complex retrieval without blocking the agent loop — deep
multi-hop graph expansion, corpus-wide temporal sweeps, or oversized candidate sets. Use
when `recall` would exceed a tool-execution timeout or when strategies include multi-hop
`graph` with large `top_k`.

**Input:** `{ …same as recall…, "poll_hint_ms"?: int }`

**Output:**

```json
{
  "type": "object",
  "required": ["request_id", "status"],
  "properties": {
    "request_id": { "type": "string", "format": "uuid" },
    "status": { "enum": ["queued", "running", "completed", "failed"] },
    "result"?: { "$ref": "recall-output.schema.json", "description": "same hydrated shape as recall output; present when status=completed" },
    "error"?: { "type": "object", "properties": { "code": { "type": "string" }, "message": { "type": "string" } } },
    "poll_after_ms"?: { "type": "integer" }
  }
}
```

Calling `recall_async` again with the same `request_id` acts as the poll operation (and is
equivalent to REST `GET /recall/{request_id}`). **Example:** first call returns
`{ "request_id": "018f5a2e-f1d5-…a62", "status": "queued", "poll_after_ms": 250 }`; the next
call with `{"request_id": …}` returns `status: "completed"` with the full hydrated result.

## 2.3 Session Tools

Session-scoped: `session_id` comes from connection context, never from parameters.

---

### `start_session`

Create a new agent session and bootstrap it (Flow D fixed-order injection). Use at the
start of an agent run, before any recall.

**Input schema:**

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["agent_type", "user_id", "context_window"],
  "properties": {
    "agent_type": { "enum": ["claude-code", "codex", "cursor", "letta", "custom"] },
    "user_id": { "type": "string", "minLength": 1 },
    "shared_space_id": { "type": "string", "format": "uuid" },
    "context_window": {
      "type": "object",
      "required": ["model", "max_tokens", "instruction_slot_budget"],
      "properties": {
        "model": { "type": "string" },
        "max_tokens": { "type": "integer", "minimum": 1 },
        "instruction_slot_budget": { "type": "integer", "minimum": 0 }
      }
    },
    "bootstrap": { "type": "boolean", "default": true }
  }
}
```

**Output:** `{ "session": AgentSession, "bootstrap_plan": InjectionPlanItem[] | null }`

**Example:** input `{ "agent_type": "claude-code", "user_id": "videogamer", "context_window": { "model": "claude-sonnet-4.6", "max_tokens": 200000, "instruction_slot_budget": 150 } }` → output carries the §3.4 `AgentSession` instance plus bootstrap plan (identical to REST `POST /sessions`).

---

### `get_session`

Get the active session's state — working set, context budget consumption. Use before
large injections to check remaining slots/tokens.

**Input:** `{}` (empty — active session from context)

**Output:** `{ "session": AgentSession, "usage": { "tokens_remaining": int, "slots_remaining": int } }`

**Example result:**

```json
{
  "session": {
    "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80",
    "agent_type": "claude-code",
    "user_id": "videogamer",
    "context_window": { "model": "claude-sonnet-4.6", "max_tokens": 200000, "used_tokens": 84512, "instruction_slot_budget": 150, "slots_used": 61 },
    "active_memories": ["018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "018f5a2e-aa57-7f22-d5e9-4c8b2a6d1f03"],
    "injection_order": ["global_claude_md", "project_claude_md", "project_rules", "conditional_rules"],
    "created_at": "2026-08-24T09:01:58Z"
  },
  "usage": { "tokens_remaining": 115488, "slots_remaining": 89 }
}
```

---

### `activate_memory`

Attach a memory to the active session's working set. Use when the agent explicitly wants
a memory resident in context (e.g. a procedural rule set) rather than waiting for recall.

**Input:** `{ "memory_id": uuid, "position"?: int, "force"?: bool (default false) }`

**Output:** `{ "session": AgentSession, "injection": { "memory_id": uuid, "position": int, "slot_cost": int } }`

Tool error `SLOT_BUDGET_EXCEEDED` (§1.3) if the dual budget is exhausted and `force` is false.

---

### `deactivate_memory`

Detach a memory from the active session (record itself is untouched).

**Input:** `{ "memory_id": uuid }`

**Output:** `{ "session": AgentSession }`

---

### `end_session`

End the session and trigger consolidation (Flow C): compact, consolidate episodic →
semantic candidates, expire/supersede, hand promotion candidates to the Sync Layer. Use
at the natural end of an agent run.

**Input:** `{ "summary"?: string, "consolidate"?: bool (default true) }`

**Output:**

```json
{
  "type": "object",
  "required": ["session"],
  "properties": {
    "session": { "$ref": "agent-session.schema.json" },
    "consolidation": {
      "type": "object",
      "properties": {
        "job_id": { "type": "string", "format": "uuid" },
        "status": { "enum": ["queued", "running", "completed", "failed"] },
        "result"?: { "type": "object", "properties": { "merged": { "type": "integer" }, "superseded": { "type": "integer" }, "semantic_candidates": { "type": "integer" }, "promotion_proposals_created": { "type": "integer" } } }
      }
    }
  }
}
```

**Example:** input `{ "summary": "Session designed API contracts for the memory service." }` → `{ "session": { …, "ended_at": "2026-08-24T10:44:02Z" }, "consolidation": { "job_id": "018f5a2e-6c1f-…", "status": "queued" } }`.

## 2.4 SharedMemorySpace Tools

---

### `create_space`

Create a shared memory space with its access policy and storage backend.

**Input:** `SpaceInput` (§1.2.4) — `{ name, description?, owner_type, owner_id, scope, participants?, access_policy, storage_backend, retention_policy? }`

**Output:** `{ "space": SharedMemorySpace }` — the §3.5 instance.

---

### `list_spaces`

List spaces visible to the connection principal. Use to discover which shared memories an
agent may read before recalling from them.

**Input:** `{ "owner_type"?: enum, "scope"?: string[], "batch_size"?: int (default 20, max 50), "cursor"?: string }`

**Output:** `{ "items": SharedMemorySpace[], "has_more": bool, "next_cursor"?: string }`

---

### `get_space`

Get full space details — participants, access policy, artifacts manifest, sync state.

**Input:** `{ "space_id": uuid }`

**Output:** `{ "space": SharedMemorySpace }`

---

### `promote_memory`

Propose promoting an individual memory into a shared space. Creates a `PromotionProposal`
with a reviewable diff. Use after consolidation produces a high-confidence semantic
candidate worth sharing. Under `promote: auto` the proposal merges immediately (recorded
for audit).

**Input schema:**

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["memory_id", "space_id"],
  "properties": {
    "memory_id": { "type": "string", "format": "uuid" },
    "space_id": { "type": "string", "format": "uuid" },
    "target_artifact": {
      "type": "object",
      "properties": {
        "path": { "type": "string" },
        "kind": { "enum": ["spec", "rule", "agent_doc", "memory_doc"] },
        "role": { "enum": ["procedural", "semantic", "episodic"] }
      }
    },
    "note": { "type": "string" }
  }
}
```

**Output:** `{ "proposal": PromotionProposal }`

**Example result:**

```json
{
  "proposal": {
    "proposal_id": "018f5a2e-5e40-78f1-a9c3-7b2d6e4f8a15",
    "shared_space_id": "018f5a2e-c0de-71ab-93f5-6e2a8c4d0b17",
    "candidate_memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
    "target_artifact": { "path": "specs/002-agent-memory/spec.md", "kind": "spec", "role": "semantic" },
    "diff": "--- a/specs/002-agent-memory/spec.md\n+++ b/specs/002-agent-memory/spec.md\n@@ -12,6 +12,9 @@\n+Decay policy: supersession-first…\n",
    "status": "in_review",
    "proposed_at": "2026-08-24T09:12:00Z"
  }
}
```

---

### `review_proposals`

List proposals for a space (default: pending). Use by reviewing agents/humans to see what
awaits decision.

**Input:** `{ "space_id": uuid, "status"?: "draft"|"in_review"|"merged"|"rejected" (default "in_review"), "batch_size"?: int, "cursor"?: string }`

**Output:** `{ "items": PromotionProposal[], "has_more": bool, "next_cursor"?: string }`

---

### `approve_proposal`

Approve a promotion: merges the diff, flips the memory to `access_scope: shared`, bumps
the space revision, notifies active agents. Requires promote/admin in connection context
— otherwise tool error `SPACE_PROMOTE_DENIED`.

**Input:** `{ "proposal_id": uuid, "note"?: string }` (`space_id` inferred from the proposal)

**Output:** `{ "proposal": PromotionProposal }` — `status: "merged"`, `resolved_at`, `reviewer` (from context) set.

---

### `reject_proposal`

Reject a promotion with a mandatory reason (audit trail).

**Input:** `{ "proposal_id": uuid, "reason": string (minLength 1) }`

**Output:** `{ "proposal": PromotionProposal }` — `status: "rejected"`.

---

### `sync_space`

Trigger backend sync for a space (git or graph/vector epoch). Async: returns a job the
agent may re-poll by calling `sync_space` again with the job id.

**Input:** `{ "space_id": uuid, "direction"?: "pull"|"push"|"both" (default "both"), "job_id"?: uuid (poll mode) }`

**Output:** `{ "job": { "job_id": uuid, "kind": "space_sync", "status": "queued|running|completed|failed", "result"?: { "revision": string, "commits"?: int, "caches_invalidated": int } }, "space_id": uuid }`

## 2.5 Lifecycle Tools

---

### `consolidate`

Trigger consolidation for a scope. Use after session ends (automatic via `end_session`)
or periodically for a user/space. `dry_run` returns planned mutations without applying.

**Input schema:**

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["scope"],
  "properties": {
    "scope": {
      "type": "object",
      "required": ["kind"],
      "properties": {
        "kind": { "enum": ["session", "user", "space", "all"] },
        "id": { "type": "string" }
      }
    },
    "memory_types": { "type": "array", "items": { "enum": ["episodic", "semantic", "procedural"] } },
    "dry_run": { "type": "boolean", "default": false },
    "job_id": { "type": "string", "format": "uuid", "description": "poll mode: pass a previously returned job id" }
  }
}
```

**Output:** `{ "job": { "job_id": uuid, "kind": "consolidation", "status": "…", "result"?: { "merged": int, "superseded": int, "expired_ttl": int, "semantic_candidates": int, "promotion_proposals_created": int } } }`

---

### `decay`

Trigger a decay pass — reduce decay/confidence on stale memories, expire TTL'd records.
Use on a schedule (cron-driven agent) or after long idle periods.

**Input:** `{ "scope": {…}, "decay_profile"?: "standard"|"aggressive", "min_age_days"?: int, "job_id"?: uuid (poll) }`

**Output:** `{ "job": { "job_id": uuid, "kind": "decay", "status": "…", "result"?: { "decayed": int, "expired_ttl": int, "avg_decay_delta": number } } }`

---

### `memory_stats`

Get memory statistics. Use for monitoring/dashboards or an agent self-reporting its
memory health.

**Input:** `{ "scope"?: "global" | "user:<id>" | "space:<uuid>" (default "global"), "include_index_health"?: bool }`

**Output:** the `GET /lifecycle/stats` body schema verbatim (§1.2.5).

**Example result:** the stats JSON from §1.2.5.

## 2.6 MCP Resources (read-only)

Resources are addressable, read-only views an MCP client can list/read without a tool
call. All read access applies the same principal checks as the corresponding tools.

| URI template | Content | Backing |
|---|---|---|
| `memory://spaces/{space_id}` | `SharedMemorySpace` metadata (JSON): participants, access_policy, storage_backend, artifacts manifest, sync_state, last_synced_at | Shared Memory Sync Layer |
| `memory://sessions/{session_id}` | `AgentSession` state (JSON): context_window budget, active_memories, injection_order, summary | Recall Router |
| `memory://stats` | global `GET /lifecycle/stats` document (JSON), refreshed ≤ 60 s | Memory Lifecycle Manager |

Resource examples return the same JSON instances shown in §1.2 (`SharedMemorySpace` §3.5,
`AgentSession` §3.4, stats §1.2.5). Resources are strictly read-only: every mutation goes
through a tool.

---

# Part 3 — Cross-Protocol Mapping

## 3.1 REST ↔ MCP mapping table

| Capability | REST endpoint | MCP tool | Differences |
|---|---|---|---|
| Create memory | `POST /memories` | `save_memory` | MCP fills `provenance.agent_id`/`session_id` from connection context (unforgeable); REST **server-stamps `provenance.agent_id`/`actor` from the bearer token / API key principal** — client-supplied values are ignored (F21); REST keeps client `origin` for service integrators |
| List memories | `GET /memories` | `list_memories` | REST: cursor + `limit` (≤200) + rich sort/filter (`valid_at`, `q`, ranges); MCP: `batch_size` ≤ 50, filter subset, no sort param — agents get relevance-ordered data from `recall` instead |
| Get memory | `GET /memories/{id}` | `get_memory` | MCP has no `at_version` history read; REST supports point-in-time version reads |
| Update memory | `PUT /memories/{id}` | `update_memory` | Same supersession semantics; MCP requires `expected_version` (REST also allows `If-Match`) |
| Delete memory | `DELETE /memories/{id}` | `delete_memory` | MCP exposes `expire` only; `purge` is REST-admin-only (see §4.3). MCP adds `reason` |
| Create link | `POST /memories/{id}/links` | `link_memories` | REST derives `source_id` from path; MCP takes both endpoints as parameters |
| List links | `GET /memories/{id}/links` | `get_memory` with `include_links: true` | REST has direction/weight filters; MCP returns the direct edge set only |
| Recall (sync) | `POST /recall` (`mode=sync`) | `recall` | MCP inlines hydrated memories; REST returns IDs + scores (hydration is a separate call) |
| Recall (async) | `POST /recall` (`mode=async`) + `GET /recall/{request_id}` | `recall_async` (submit + poll in one tool) | MCP self-polling by `request_id`; REST polls a dedicated GET |
| Create session | `POST /sessions` | `start_session` | MCP binds the session to the connection; REST returns a free-floating `session_id` |
| Get session | `GET /sessions/{session_id}` | `get_session` | MCP: active session only (no `{id}` param); REST can read any authorized session |
| Activate memory | `POST /sessions/{session_id}/memories` | `activate_memory` | Identical budget enforcement; MCP omits `session_id` |
| Deactivate memory | `DELETE /sessions/{session_id}/memories/{memory_id}` | `deactivate_memory` | Identical |
| End session | `POST /sessions/{session_id}/end` | `end_session` | REST returns a pollable `JobRef`; MCP embeds the job object for re-poll via the tool |
| Create space | `POST /spaces` | `create_space` | Identical body |
| List spaces | `GET /spaces` | `list_spaces` | Pagination: REST cursor `limit` ≤200 vs MCP `batch_size` ≤50 |
| Get space | `GET /spaces/{space_id}` | `get_space` + resource `memory://spaces/{id}` | MCP offers both tool and resource views |
| Update space | `PUT /spaces/{space_id}` | — (none) | Policy mutation is deliberately REST-admin-only (§4.3) |
| Propose promotion | `POST /spaces/{space_id}/memories` | `promote_memory` | Identical semantics; MCP infers `space_id` from parameters rather than path |
| List proposals | `GET /spaces/{space_id}/proposals` | `review_proposals` | Same status filter |
| Approve / reject | `POST …/proposals/{id}/approve` \| `/reject` | `approve_proposal` \| `reject_proposal` | MCP takes only `proposal_id` (space inferred); reject requires `reason` in both |
| Trigger sync | `POST /spaces/{space_id}/sync` | `sync_space` | MCP poll-by-`job_id` in-tool; REST `JobRef` + shared jobs endpoint |
| Consolidate | `POST /lifecycle/consolidate` | `consolidate` | Identical; MCP adds in-tool poll |
| Decay pass | `POST /lifecycle/decay` | `decay` | Identical |
| Job status | `GET /lifecycle/jobs/{job_id}` | in-tool `job_id` poll (consolidate/decay/sync_space) | REST has one generic jobs endpoint; MCP folds polling into each tool |
| Stats | `GET /lifecycle/stats` | `memory_stats` + resource `memory://stats` | Resource form is cached ≤60 s; tool form is live |

## 3.2 Capability asymmetries (intentional)

1. **No MCP space-policy mutation.** `PUT /spaces/{id}` (access policy, retention,
   participants) has no MCP counterpart: access control is human/operator territory, and
   letting an agent rewrite the policy that gates its own writes is a privilege-escalation
   path.
2. **No MCP hard purge by default.** `delete_memory` is expire-only; `purge` stays behind
   REST admin auth. Memory loss is irreversible; expiry is not.
3. **No MCP memory-history reads.** `at_version` and `include_expired` browsing are
   REST-only — agent contexts need current truth, not archaeology.
4. **REST has no inline hydration on recall.** REST returns IDs (composable, cacheable,
   small payloads); MCP returns hydrated content because each MCP round-trip costs the
   agent a tool call.
5. **REST exposes raw listing power; MCP pushes agents to `recall`.** `GET /memories`
   sorting/ranges exist for operators; MCP `list_memories` is deliberately limited so
   agents prefer the relevance-ranked, budget-aware path.

---

# Part 4 — Design Decisions & Constraints

## 4.1 Sync vs async

| Operation | Mode | Why |
|---|---|---|
| Memory CRUD, links | sync | Single-record store + index-delta writes; bounded latency (architecture §2.2 Store/Index contracts) |
| `recall` | sync (default) | Interactive path; sub-200 ms is an explicit production target [source: 03-zep-temporal-knowledge-graph] |
| `recall` async | opt-in, or server-upgraded | Deep multi-hop graph expansion or corpus-wide temporal sweeps can exceed interactive budgets; `mode=async` (or server upgrade when `strategies` includes `graph` with `top_k` > 25) keeps the agent loop unblocked |
| Session create/end | sync / 202 | Create is cheap; **end** enqueues consolidation (Flow C) — compaction, consolidation, expiry, proposal drafting are corpus-wide and must not block the caller |
| Promotion propose | sync | Creates one proposal record; the diff is drafted inline |
| Promotion approve | sync | One merge + notification fan-out |
| Space sync | async (202) | Backend-dependent (git push/pull, graph epochs); unbounded wall-clock |
| Consolidate / decay | async (202) | Batch MutationOps over many records; potentially minutes |

Rule of thumb applied: **single-record or budget-bounded work is sync; anything whose
latency depends on corpus size or an external backend is a job.**

## 4.2 Pagination strategy

- **REST: opaque cursor.** `next_cursor` encodes `(sort_key, last_id)`; stable under
  concurrent writes, no offset drift, cheap on composite indexes. `limit` ≤ 200.
  `total_estimate` is `null` by default (exact counts are O(corpus)); an explicit
  `count=true` opt-in returns an estimate for UIs.
- **MCP: `batch_size` + `next_cursor`.** Capped at **50** (20 default) — tool results are
  consumed directly by an LLM context window, so page size is a token-budget concern, not
  a UI concern. Cursor mechanics are identical to REST, so a client that outgrows MCP can
  graduate to REST pagination without relearning semantics.
- **Recall is not paginated.** `top_k` + slot/token budgets bound it — that *is* the
  paging mechanism for the retrieval path (architecture §2.2 Router budget policy).

## 4.3 Deletion, privilege, and the supersession-first constraint

The data model's retention rule (`supersede_not_delete`, default true) shapes the
interfaces: **there is no casual hard delete anywhere.**

- REST `DELETE` defaults to `mode=expire` (closes `validity.valid_until`); `purge`
  requires admin and an `If-Match`, and is audit-logged.
- MCP `delete_memory` is expire-only.
- Space policy mutation (`PUT /spaces`) is REST-admin-only — no MCP tool exists
  (§3.2). Approve/reject of promotions require `promote`/`admin` on the space under both
  protocols; under MCP the reviewer identity is the connection principal (unforgeable,
  whereas REST reviewers are asserted then authorized).

## 4.4 Retrieval params → the 4-way hybrid

`retrieval_params.strategies` is a direct lever on the Retrieval Engine's parallel paths
(architecture §2.4):

| Param value | Engine path | What it triggers |
|---|---|---|
| `vector` | dense | embedding similarity over Index Manager vector structures (or FalkorDB-stored node embeddings) |
| `bm25` | sparse | lexical terms incl. Entity `aliases`; **BM25-like (ts_rank_cd)** — PostgreSQL cover-density ranking, not true BM25 with IDF weighting; if true BM25 is needed evaluate ParadeDB `pg_search` as a drop-in replacement [source: 06-hybrid-retrieval-strategies] |
| `graph` | graph | vector entry points → MemoryLink expansion (≤2 hops) → vector ranking of associated chunks |
| `temporal` | temporal | `validity`-interval filtering + spreading activation over event order |

Defaults: all four strategies, `rerank: cross_encoder` (mandatory in production
[source: 06-hybrid-retrieval-strategies]; `rerank: none` is accepted but flagged in
telemetry). `context.entities` (router-side entity extraction) boosts the entity-centric
path [source: 02-mem0-universal-memory-layer]; `context.time_bounds` narrows the temporal
path; `context.mentioned_files` matches `source_ref.path` on the sparse path. The
`slot_budget`/`token_budget` pair is the Router's dual budget — injections are counted
against instruction slots *and* tokens before entering working memory
[source: 07-claude-code-memory].

## 4.5 Authentication differences

- **REST:** per-request bearer token / API key → principal. Stateless; every request is
  authorized independently. Suitable for services, CI, dashboards.
- **MCP:** identity at connection initialization (client handshake on stdio; OAuth on
  http transport) → the connection's implicit principal for all tools/resources, plus an
  optional bound `session_id`. Agents never handle tokens mid-conversation (tokens leaking
  into transcripts is a real hazard), and provenance fields (`agent_id`, `session_id`,
  `reviewer`) are server-derived — agents cannot claim to be someone else.
- Shared core: both fronts resolve to the same principal model checked against
  `access_policy` + `participants` on every space-scoped operation.

## 4.6 PromotionProposal flow across both interfaces

One state machine, two front doors (data-models §3.8, architecture §1.4 ownership rule —
individual memory is agent-written, shared memory is human-approved):

```
individual Memory ──consolidation──▶ semantic candidate
      │ (agent)                        │
      │  REST: POST /spaces/{id}/memories        MCP: promote_memory
      ▼                                          ▼
PromotionProposal [in_review] ──diff drafted by Sync Layer──▶ artifact
      │
      ├──approve (REST /approve | MCP approve_proposal)──▶ merged:
      │     artifact committed, memory.access_scope→shared,
      │     sync_state.revision bumped, agent caches invalidated
      └──reject  (REST /reject  | MCP reject_proposal)───▶ rejected (reason kept)
```

- `access_policy.promote: human_review` (default) → approve is required; an MCP agent
  *may* call `approve_proposal` only if its principal holds promote/admin — in practice a
  human reviewer session.
- `access_policy.promote: auto` → proposal is created and merged in one step, still
  recorded for audit.
- `PROMOTION_NOT_CONSOLIDATED` guards the pipeline: an episodic memory without
  consolidation lineage cannot jump straight into shared memory (single-interaction noise
  must not become durable shared "facts" — encoder policy, architecture §2.2).

## 4.7 Rate limiting

- **REST:** per-principal token bucket — defaults 100 req/min read, 30 req/min write,
  5/min for lifecycle batch jobs (they are expensive); `429 RATE_LIMITED` + `Retry-After`.
  Recall gets a separate, higher budget (it is the hot path) with a concurrency cap of 5
  in-flight async recalls per principal.
- **MCP:** no per-call auth overhead, so limits bind on *cost*: per-connection caps on
  recall frequency (e.g. 60/min) and on batch_size; lifecycle tools are operator-gated
  rather than rate-limited. An agent in a tight loop calling `recall` is the failure mode
  being prevented — budgets on the session (`slot_budget`) plus connection-level limits
  bound it from both sides.
- **Both:** lifecycle batch jobs (consolidate/decay/sync) are additionally serialized per
  scope (`SPACE_SYNC_IN_PROGRESS`, 409) to avoid write-write races on the same corpus.

## 4.8 Versioning & evolution constraints

- Additive changes only within `/v1`: new optional fields, new endpoints, new tool
  inputs/outputs. Breaking changes → `/v2` with ≥6 months dual-running.
- Entity `version` (Memory) and `sync_state.revision` (space) handle data-level evolution
  independent of API versions — the research's TTL + versioning pattern
  [source: 06-hybrid-retrieval-strategies].
- Error codes are stable strings; unknown-code tolerance is a client requirement.
- MCP tools may gain optional input fields but never repurpose existing ones — agent
  prompt-cached tool definitions make silent semantic drift costly.

## Uncertainty Notes

- MCP resource URIs, transports (stdio/streamable-http), and OAuth handshake are designed
  against the MCP model as described in [source: 08-codex-copilot-persistent-memory]
  (Codex persistent memory via MCP); exact MCP spec details (revision numbers, sampling
  features) were **not** in the source material and are left implementation-agnostic here.
- Sub-200 ms recall target and "rerank is mandatory" are production claims from single
  sources [source: 03-zep-temporal-knowledge-graph, 06-hybrid-retrieval-strategies]; the
  sync/async split in §4.1 hedges but does not eliminate this risk.
- Rate-limit numbers (§4.7) are illustrative defaults, not research-derived constants.
- The `auto` promotion mode weakens the human-review default; systems that never need it
  can remove the enum value without touching the rest of the contract.

## Related Pages

- [[pages/data-models|Data Models]] — the 8 entity schemas every request/response above carries
- [[pages/architecture|Architecture]] — the 7 components and 4 flows these operations façade
- [[pages/memory-taxonomy|Memory Taxonomy]] — the `type` enums (episodic/semantic/procedural)
- [[pages/retrieval-and-recall|Retrieval and Recall]] — the strategies behind `retrieval_params`
- [[ai-memory-research|Canonical Overview]]

## Tags

- #ai-agents
- #memory-systems
- #api-design
- #rest
- #mcp
- #context-management



