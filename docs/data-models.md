# Data Models — AI Agent Memory & Recall

Concrete data models (schemas, relationships, serialization formats, storage mapping) for the architecture defined in [[pages/architecture|Architecture]]. Every model binds to the logical components of that page (Memory Encoder, Memory Store, Index Manager, Retrieval Engine, Recall Router, Memory Lifecycle Manager, Shared Memory Sync Layer) and to the storage backends of its §3.1.

---

## 1. Design Principles

These constraints, carried over from the architecture page, shape every schema below:

1. **Type-first records** — every memory carries `type` (episodic | semantic | procedural), mapping to the CoALA taxonomy [source: 05-coala-memory-types-taxonomy] and the tiered stores of Letta/Mem0/Zep [source: 01-letta-memgpt-stateful-agents, 02-mem0-universal-memory-layer, 03-zep-temporal-knowledge-graph].
2. **Supersede, don't delete** — temporal validity intervals plus `superseded_by` pointers keep history answerable; deletion is reserved for TTL expiry [source: 03-zep-temporal-knowledge-graph, 06-hybrid-retrieval-strategies].
3. **Provenance is mandatory** — every record states where it came from (observation, user instruction, file artifact, consolidation) [source: 02-mem0-universal-memory-layer, 04-a-mem-agentic-memory-architecture].
4. **Links are first-class** — relationships between memories are stored records, not derived-only structure [source: 04-a-mem-agentic-memory-architecture, 03-zep-temporal-knowledge-graph].
5. **Budget-aware recall** — requests and results carry instruction-slot and token budgets, not just scores [source: 07-claude-code-memory].
6. **Files are the interop tier** — team/procedural memory must be representable as human-readable markdown/YAML under version control [source: 07-claude-code-memory, 09-cursor-cursorrules-memory, 10-spec-driven-development-memory].
7. **Separate individual from shared scope** — `access_scope` distinguishes agent-written memory from human-approved shared memory [source: 10-spec-driven-development-memory, 07-claude-code-memory].

---

## 2. Entity Catalog

| # | Entity | Role in architecture | Primary backend (§5) |
|---|--------|----------------------|----------------------|
| 1 | **Memory** | The unit stored by the Memory Store; produced by the Memory Encoder | Relational (metadata) + Vector/Graph (embedding) + Files (procedural/team) |
| 2 | **MemoryLink** | Edge record maintained by the Index Manager; drives graph retrieval | Graph DB |
| 3 | **Entity** | Entity-centric anchor (people, projects, tools) with temporal facts | Graph DB |
| 4 | **AgentSession** | Runtime binding: agent, user, context budget, active working set | Relational (metadata); working tier in-context only |
| 5 | **SharedMemorySpace** | The shared, access-controlled memory space (any owner type; files are one backend choice) | Files (git) or Graph/Vector, per `storage_backend` |
| 8 | **PromotionProposal** | Shared Memory Sync Layer record: candidate semantic memory → file diff for human review | Files (draft diff) + relational (status) |

**Not core entities:** RecallRequest (6) and RecallResult (7) are **operational log records**, not domain entities — see §3.9 "Operational Log Records" (F14). Supporting reference entity: **EmbeddingModel** (§3.10).

The five entities demanded by the plan (Memory, MemoryLink, AgentSession, SharedMemorySpace — renamed from `TeamMemorySpace` to reflect that the space may be owned by a user, agent, team, or organization — and RecallRequest) are 1, 2, 4, 5, plus the RecallRequest log record. Entity, RecallResult, and PromotionProposal were added because the architecture demands them: entity-centric indexing [source: 02-mem0-universal-memory-layer, 03-zep-temporal-knowledge-graph], re-ranked injection with budgets [source: 06-hybrid-retrieval-strategies, 07-claude-code-memory], and the human-review promotion path [source: 10-spec-driven-development-memory]. The catalog deliberately counts **6 entities** (Memory, MemoryLink, Entity, AgentSession, SharedMemorySpace, PromotionProposal); RecallRequest/RecallResult are observability events with no domain lifecycle beyond their log role (the async API polls them and stats read them, which is why they persist at all — as log tables, not entities).

---

## 3. Entity Definitions

All identifiers are UUIDv7 (time-ordered, index-friendly). All timestamps are ISO 8601 UTC. Vectors in examples are truncated to 4 dimensions for readability.

### 3.1 Memory

The core record. Field semantics:

| Field | Type | Req | Notes / Evidence |
|---|---|---|---|
| `id` | uuid | ✓ | stable identifier across all backends |
| `type` | enum | ✓ | `episodic` \| `semantic` \| `procedural` — CoALA core types [source: 05-coala-memory-types-taxonomy]; tiers map core→working, recall→episodic, archival→semantic [source: 01-letta-memgpt-stateful-agents] |
| `content` | string | ✓ | markdown-first so file projection is lossless [source: 10-spec-driven-development-memory] |
| `content_format` | enum | | `markdown` (default) \| `plain` \| `json` |
| `entities` | uuid[] | | anchors into Entity; entity-centric extraction at encode time [source: 02-mem0-universal-memory-layer] |
| `tags` | string[] | | A-MEM assigns tags at capture [source: 04-a-mem-agentic-memory-architecture] |
| `embedding` | object | | `{vector[], model, dims}`; stored as vector index or graph node property (FalkorDB co-location) [source: 06-hybrid-retrieval-strategies] |
| `provenance` | object | ✓ | `{origin, session_id?, agent_id?, actor?}` — mandatory provenance [source: 02-mem0-universal-memory-layer]. **`agent_id` and `actor` are server-stamped** from the authenticated principal (bearer token / API key on REST, connection context on MCP), never client-writable — client-supplied values are ignored as provenance evidence (F21); only `origin` is client-asserted |
| `source_ref` | object | | `{kind: file\|url\|tool_call\|message, path/uri, hash?}` — file hash enables change detection in the Shared Memory Sync Layer [source: 10-spec-driven-development-memory] |
| `created_at` / `updated_at` | timestamp | ✓/— | access tracking is **decoupled** from this table: reads do not update any column; recency signals come from the `memory_access_log` append-only log (F8) [source: 06-hybrid-retrieval-strategies] |
| `confidence` | number 0–1 | | encoder-assessed; repetition across sessions raises it (encoding policy, architecture §2.2) |
| `decay_score` | number 0–1 | | 1.0 = fully vital; driven by Lifecycle Manager |
| `validity` | object | | `{valid_from, valid_until?, superseded_by?}` — temporal validity ranges; supersession closes the interval rather than deleting [source: 03-zep-temporal-knowledge-graph] |
| `ttl_expires_at` | timestamp | | TTL expiration without full re-index [source: 06-hybrid-retrieval-strategies] |
| `version` | integer | ✓ | monotonic; supersession writes a new version [source: 06-hybrid-retrieval-strategies] |
| `access_scope` | enum | ✓ | `individual` \| `shared` — the ownership boundary of architecture §1.4 [source: 10-spec-driven-development-memory] |
| `shared_space_id` | uuid? | | required iff `access_scope = shared`; points at the SharedMemorySpace |
| `origin_session_id` | uuid? | | the AgentSession that produced it |

**JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.org/schemas/ai-memory/memory.schema.json",
  "title": "Memory",
  "type": "object",
  "required": ["id", "type", "content", "provenance", "created_at", "version", "access_scope"],
  "properties": {
    "id": { "type": "string", "format": "uuid" },
    "type": { "enum": ["episodic", "semantic", "procedural"] },
    "content": { "type": "string", "minLength": 1 },
    "content_format": { "enum": ["markdown", "plain", "json"], "default": "markdown" },
    "entities": { "type": "array", "items": { "type": "string", "format": "uuid" } },
    "tags": { "type": "array", "items": { "type": "string", "pattern": "^[a-z0-9][a-z0-9_-]*$" } },
    "embedding": {
      "type": "object",
      "required": ["vector", "model", "dims"],
      "properties": {
        "vector": { "type": "array", "items": { "type": "number" }, "minItems": 1 },
        "model": { "type": "string" },
        "dims": { "type": "integer", "minimum": 1 }
      }
    },
    "provenance": {
      "type": "object",
      "required": ["origin"],
      "description": "agent_id/actor/session_id are server-stamped from the authenticated principal (readOnly; client-supplied values ignored)",
      "properties": {
        "origin": { "enum": ["agent_observation", "user_instruction", "file_artifact", "consolidation"] },
        "session_id": { "type": "string", "format": "uuid", "readOnly": true },
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
    "created_at": { "type": "string", "format": "date-time" },
    "updated_at": { "type": "string", "format": "date-time" },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "validity": {
      "type": "object",
      "properties": {
        "valid_from": { "type": "string", "format": "date-time" },
        "valid_until": { "type": "string", "format": "date-time" },
        "superseded_by": { "type": "string", "format": "uuid" }
      }
    },
    "ttl_expires_at": { "type": "string", "format": "date-time" },
    "version": { "type": "integer", "minimum": 1 },
    "access_scope": { "enum": ["individual", "shared"] },
    "shared_space_id": { "type": "string", "format": "uuid" },
    "origin_session_id": { "type": "string", "format": "uuid" }
  }
}
```

**JSON instance:**

```json
{
  "id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
  "type": "semantic",
  "content": "The memory architecture decay policy is supersession-first: close the temporal validity interval and open a new version; deletion is reserved for TTL expiry.",
  "content_format": "markdown",
  "entities": ["018f5a2e-6d10-7c3f-9a1b-2e4c8d5f7a30"],
  "tags": ["architecture", "decay-policy"],
  "embedding": {
    "vector": [0.021, -0.113, 0.342, -0.078],
    "model": "text-embedding-3-small",
    "dims": 1536
  },
  "provenance": {
    "origin": "consolidation",
    "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80",
    "agent_id": "claude-code",
    "actor": "agent"
  },
  "source_ref": {
    "kind": "file",
    "path": ".zbot/specs/ai-agent-memory-architecture/plan.md",
    "hash": "sha256:4f2c…"
  },
  "created_at": "2026-08-23T23:45:12Z",
  "updated_at": "2026-08-23T23:45:12Z",
  "confidence": 0.86,
  "decay_score": 0.94,
  "validity": {
    "valid_from": "2026-08-23T23:45:12Z",
    "valid_until": null,
    "superseded_by": null
  },
  "ttl_expires_at": null,
  "version": 1,
  "access_scope": "individual",
  "shared_space_id": null,
  "origin_session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80"
}
```

**YAML instance:**

```yaml
id: 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21
type: semantic
content: >
  The memory architecture decay policy is supersession-first: close the
  temporal validity interval and open a new version; deletion is reserved
  for TTL expiry.
content_format: markdown
entities:
  - 018f5a2e-6d10-7c3f-9a1b-2e4c8d5f7a30
tags: [architecture, decay-policy]
embedding:
  vector: [0.021, -0.113, 0.342, -0.078]
  model: text-embedding-3-small
  dims: 1536
provenance:
  origin: consolidation
  session_id: 018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80
  agent_id: claude-code
  actor: agent
source_ref:
  kind: file
  path: .zbot/specs/ai-agent-memory-architecture/plan.md
  hash: sha256:4f2c…
created_at: "2026-08-23T23:45:12Z"
updated_at: "2026-08-23T23:45:12Z"
confidence: 0.86
decay_score: 0.94
validity:
  valid_from: "2026-08-23T23:45:12Z"
  valid_until: null
  superseded_by: null
ttl_expires_at: null
version: 1
access_scope: individual
shared_space_id: null
origin_session_id: 018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80
```

### 3.2 MemoryLink

A directed, weighted edge between two Memory records (or Memory → Entity). The Index Manager creates these at encode time; the Retrieval Engine's graph path expands along them [source: 04-a-mem-agentic-memory-architecture, 06-hybrid-retrieval-strategies].

| Field | Type | Req | Notes / Evidence |
|---|---|---|---|
| `id` | uuid | ✓ | edge identifier |
| `source_id` / `target_id` | uuid | ✓ | both endpoints must exist (referential integrity enforced by relational metadata even when edges live in the graph DB) |
| `relationship_type` | enum | ✓ | `derived_from` (consolidation), `supersedes` (temporal chain), `similar_to` (semantic kNN), `co_occurs_with` (entity co-occurrence), `causal_next` (event sequence), `anchors_entity` (Memory→Entity) — link families from A-MEM link generation and Zep graph edges [source: 04-a-mem-agentic-memory-architecture, 03-zep-temporal-knowledge-graph] |
| `weight` | number 0–1 | ✓ | default 1.0; tuned by retrieval feedback |
| `evidence` | string? | | why the link exists (kept for auditability) |
| `created_at` | timestamp | ✓ | |

**JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.org/schemas/ai-memory/memory-link.schema.json",
  "title": "MemoryLink",
  "type": "object",
  "required": ["id", "source_id", "target_id", "relationship_type", "weight", "created_at"],
  "properties": {
    "id": { "type": "string", "format": "uuid" },
    "source_id": { "type": "string", "format": "uuid" },
    "target_id": { "type": "string", "format": "uuid" },
    "relationship_type": {
      "enum": ["derived_from", "supersedes", "similar_to", "co_occurs_with", "causal_next", "anchors_entity"]
    },
    "weight": { "type": "number", "minimum": 0, "maximum": 1, "default": 1.0 },
    "evidence": { "type": "string" },
    "created_at": { "type": "string", "format": "date-time" }
  }
}
```

**JSON instance:**

```json
{
  "id": "018f5a2e-b7c4-7d19-a3e8-5c6f1d2b4e90",
  "source_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
  "target_id": "018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42",
  "relationship_type": "derived_from",
  "weight": 0.91,
  "evidence": "semantic summary consolidated from 4 episodic session records",
  "created_at": "2026-08-23T23:45:12Z"
}
```

**YAML instance:**

```yaml
id: 018f5a2e-b7c4-7d19-a3e8-5c6f1d2b4e90
source_id: 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21
target_id: 018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42
relationship_type: derived_from
weight: 0.91
evidence: semantic summary consolidated from 4 episodic session records
created_at: "2026-08-23T23:45:12Z"
```

### 3.3 Entity

An entity-centric anchor (person, project, tool, repository). Distinctive of production systems: Zep tracks people/entities with temporal validity; Mem0 indexes by user/session/agent identifiers [source: 03-zep-temporal-knowledge-graph, 02-mem0-universal-memory-layer].

| Field | Type | Req | Notes |
|---|---|---|---|
| `entity_id` | uuid | ✓ | |
| `name` | string | ✓ | canonical name |
| `entity_type` | enum | ✓ | `person` \| `project` \| `repository` \| `tool` \| `organization` \| `concept` |
| `aliases` | string[] | | name variants for lexical match (feeds BM25 path) [source: 06-hybrid-retrieval-strategies] |
| `facts` | object[] | | `{fact, valid_from, valid_until?, memory_id}` — temporal validity per fact [source: 03-zep-temporal-knowledge-graph] |
| `created_at` | timestamp | ✓ | |

**JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.org/schemas/ai-memory/entity.schema.json",
  "title": "Entity",
  "type": "object",
  "required": ["entity_id", "name", "entity_type", "created_at"],
  "properties": {
    "entity_id": { "type": "string", "format": "uuid" },
    "name": { "type": "string", "minLength": 1 },
    "entity_type": { "enum": ["person", "project", "repository", "tool", "organization", "concept"] },
    "aliases": { "type": "array", "items": { "type": "string" } },
    "facts": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["fact", "valid_from", "memory_id"],
        "properties": {
          "fact": { "type": "string" },
          "valid_from": { "type": "string", "format": "date-time" },
          "valid_until": { "type": "string", "format": "date-time" },
          "memory_id": { "type": "string", "format": "uuid" }
        }
      }
    },
    "created_at": { "type": "string", "format": "date-time" }
  }
}
```

**JSON instance:**

```json
{
  "entity_id": "018f5a2e-6d10-7c3f-9a1b-2e4c8d5f7a30",
  "name": "ai-memory-research",
  "entity_type": "project",
  "aliases": ["the memory ward"],
  "facts": [
    {
      "fact": "Ward uses supersession-first decay policy",
      "valid_from": "2026-08-23T23:45:12Z",
      "valid_until": null,
      "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21"
    }
  ],
  "created_at": "2026-08-20T10:00:00Z"
}
```

**YAML instance:**

```yaml
entity_id: 018f5a2e-6d10-7c3f-9a1b-2e4c8d5f7a30
name: ai-memory-research
entity_type: project
aliases: [the memory ward]
facts:
  - fact: Ward uses supersession-first decay policy
    valid_from: "2026-08-23T23:45:12Z"
    valid_until: null
    memory_id: 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21
created_at: "2026-08-20T10:00:00Z"
```

### 3.4 AgentSession

The runtime binding. The working tier is *not* persisted as records — it is the context window itself; this model tracks its shape and budget [source: 01-letta-memgpt-stateful-agents, 05-coala-memory-types-taxonomy].

| Field | Type | Req | Notes / Evidence |
|---|---|---|---|
| `session_id` | uuid | ✓ | |
| `agent_type` | enum | ✓ | `claude-code` \| `codex` \| `cursor` \| `letta` \| `custom` [source: 07-claude-code-memory, 08-codex-copilot-persistent-memory, 09-cursor-cursorrules-memory] |
| `user_id` | string | ✓ | memory indexed per user [source: 02-mem0-universal-memory-layer] |
| `shared_space_id` | uuid? | | if session runs inside a SharedMemorySpace |
| `context_window` | object | ✓ | `{model, max_tokens, used_tokens, instruction_slot_budget, slots_used}` — dual budget [source: 07-claude-code-memory] |
| `active_memories` | uuid[] | ✓ | working-set: memory ids currently injected in-context |
| `injection_order` | string[] | | recorded bootstrap order (e.g. global → project → rules → conditional) [source: 07-claude-code-memory] |
| `created_at` / `ended_at` | timestamp | ✓/— | session end fires consolidation [source: 10-spec-driven-development-memory] |
| `summary` | string? | | recursive summarization output at eviction/session end — nothing is truly lost [source: 01-letta-memgpt-stateful-agents] |

**JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.org/schemas/ai-memory/agent-session.schema.json",
  "title": "AgentSession",
  "type": "object",
  "required": ["session_id", "agent_type", "user_id", "context_window", "active_memories", "created_at"],
  "properties": {
    "session_id": { "type": "string", "format": "uuid" },
    "agent_type": { "enum": ["claude-code", "codex", "cursor", "letta", "custom"] },
    "user_id": { "type": "string", "minLength": 1 },
    "shared_space_id": { "type": "string", "format": "uuid" },
    "context_window": {
      "type": "object",
      "required": ["model", "max_tokens", "instruction_slot_budget"],
      "properties": {
        "model": { "type": "string" },
        "max_tokens": { "type": "integer", "minimum": 1 },
        "used_tokens": { "type": "integer", "minimum": 0 },
        "instruction_slot_budget": { "type": "integer", "minimum": 0 },
        "slots_used": { "type": "integer", "minimum": 0 }
      }
    },
    "active_memories": { "type": "array", "items": { "type": "string", "format": "uuid" } },
    "injection_order": { "type": "array", "items": { "type": "string" } },
    "created_at": { "type": "string", "format": "date-time" },
    "ended_at": { "type": "string", "format": "date-time" },
    "summary": { "type": "string" }
  }
}
```

**JSON instance:**

```json
{
  "session_id": "018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80",
  "agent_type": "claude-code",
  "user_id": "videogamer",
  "shared_space_id": null,
  "context_window": {
    "model": "claude-sonnet-4.6",
    "max_tokens": 200000,
    "used_tokens": 84512,
    "instruction_slot_budget": 150,
    "slots_used": 61
  },
  "active_memories": [
    "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
    "018f5a2e-aa57-7f22-d5e9-4c8b2a6d1f03"
  ],
  "injection_order": ["global_claude_md", "project_claude_md", "project_rules", "conditional_rules"],
  "created_at": "2026-08-24T09:01:58Z",
  "ended_at": null,
  "summary": null
}
```

**YAML instance:**

```yaml
session_id: 018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80
agent_type: claude-code
user_id: videogamer
shared_space_id: null
context_window:
  model: claude-sonnet-4.6
  max_tokens: 200000
  used_tokens: 84512
  instruction_slot_budget: 150
  slots_used: 61
active_memories:
  - 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21
  - 018f5a2e-aa57-7f22-d5e9-4c8b2a6d1f03
injection_order: [global_claude_md, project_claude_md, project_rules, conditional_rules]
created_at: "2026-08-24T09:01:58Z"
ended_at: null
summary: null
```

### 3.5 SharedMemorySpace

*(Renamed from `TeamMemorySpace` and genericized: a shared memory space is not just for teams. The same entity covers one user sharing memory across their own sessions, different users sharing across sessions, a single agent persisting across sessions, a team, or a whole organization.)*

The shared, access-controlled memory space. Its core attributes are domain-agnostic; how the space is physically realized (git files, vector DB, graph) is a `storage_backend` choice, not a schema constraint. Critically, `artifacts` is **not a live database membership list** — it is the manifest of externalized, reviewable representations of the space's contents (only populated when the backend includes files) [source: 10-spec-driven-development-memory].

| Field | Type | Req | Notes / Evidence |
|---|---|---|---|
| `id` | uuid | ✓ | stable identifier of the space |
| `name` | string | ✓ | human-readable name |
| `description` | string | | what this shared space is for |
| `owner_type` | enum | ✓ | `user` \| `agent` \| `team` \| `organization` — who owns the space; a `user`-owned space persists one person's memory across their sessions, an `agent`-owned space persists one agent's, `team`/`organization` scope it to multiple principals |
| `owner_id` | string | ✓ | reference to the owning principal (user id, agent id, team id, org id) |
| `scope` | string | ✓ | domain/context this space covers, e.g. `software-development`, `research`, `customer-support`, `personal` |
| `participants` | object[] | | who the space is shared with: `{principal_type: user\|agent\|session\|group, principal_id, access_level: read\|write\|promote\|admin, granted_at?}` |
| `access_policy` | object | ✓ | generic access control (below) — shared memory is review-gated by default [source: 10-spec-driven-development-memory, 09-cursor-cursorrules-memory] |
| `storage_backend` | object | ✓ | `{kind: files\|relational\|vector\|graph\|hybrid, config_ref?}` — which backend class realizes the space (§5) |
| `artifacts` | object[] | | only when the backend includes files: `{uri, kind, role, revision?, hash?}` — externalized reviewable items; see §6 for concrete shapes [source: 10-spec-driven-development-memory, 07-claude-code-memory, 09-cursor-cursorrules-memory] |
| `sync_state` | object | ✓ | `{status, pending_proposals, revision}` — backend-neutral replication state |
| `last_synced_at` | timestamp | ✓ | change propagation timestamp [source: 10-spec-driven-development-memory] |
| `retention_policy` | object | | `{supersede_not_delete (default true), ttl_days?, archive_after_days?}` — mirrors the lifecycle rules of §1 [source: 03-zep-temporal-knowledge-graph, 06-hybrid-retrieval-strategies] |
| `created_at` / `updated_at` | timestamp | ✓/— | |

**Generic `access_policy`** (no backend or domain baked in):

| Key | Values | Meaning |
|---|---|---|
| `default_access` | `read` \| `write` \| `none` | what an unlisted principal gets |
| `write` | `owner_approved` \| `participant_free` \| `proposal_only` | who may write; `proposal_only` forces every write through a PromotionProposal [source: 10-spec-driven-development-memory] |
| `promote` | `human_review` \| `auto` | whether promotion from individual memory is reviewed |

**JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.org/schemas/ai-memory/shared-memory-space.schema.json",
  "title": "SharedMemorySpace",
  "type": "object",
  "required": ["id", "name", "owner_type", "owner_id", "scope", "access_policy", "storage_backend", "sync_state", "last_synced_at"],
  "properties": {
    "id": { "type": "string", "format": "uuid" },
    "name": { "type": "string", "minLength": 1 },
    "description": { "type": "string" },
    "owner_type": { "enum": ["user", "agent", "team", "organization"] },
    "owner_id": { "type": "string", "minLength": 1 },
    "scope": { "type": "string", "minLength": 1 },
    "participants": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["principal_type", "principal_id", "access_level"],
        "properties": {
          "principal_type": { "enum": ["user", "agent", "session", "group"] },
          "principal_id": { "type": "string", "minLength": 1 },
          "access_level": { "enum": ["read", "write", "promote", "admin"] },
          "granted_at": { "type": "string", "format": "date-time" }
        }
      }
    },
    "access_policy": {
      "type": "object",
      "required": ["default_access", "write", "promote"],
      "properties": {
        "default_access": { "enum": ["read", "write", "none"] },
        "write": { "enum": ["owner_approved", "participant_free", "proposal_only"] },
        "promote": { "enum": ["human_review", "auto"] }
      }
    },
    "storage_backend": {
      "type": "object",
      "required": ["kind"],
      "properties": {
        "kind": { "enum": ["files", "relational", "vector", "graph", "hybrid"] },
        "config_ref": { "type": "string" }
      }
    },
    "artifacts": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["uri", "kind", "role"],
        "properties": {
          "uri": { "type": "string" },
          "kind": { "type": "string" },
          "role": { "enum": ["procedural", "semantic", "episodic"] },
          "revision": { "type": "string" },
          "hash": { "type": "string" }
        }
      }
    },
    "sync_state": {
      "type": "object",
      "required": ["status"],
      "properties": {
        "status": { "enum": ["in_sync", "pending_review", "diverged", "offline"] },
        "pending_proposals": { "type": "integer", "minimum": 0 },
        "revision": { "type": "string" }
      }
    },
    "last_synced_at": { "type": "string", "format": "date-time" },
    "retention_policy": {
      "type": "object",
      "properties": {
        "supersede_not_delete": { "type": "boolean", "default": true },
        "ttl_days": { "type": "integer", "minimum": 1 },
        "archive_after_days": { "type": "integer", "minimum": 1 }
      }
    },
    "created_at": { "type": "string", "format": "date-time" },
    "updated_at": { "type": "string", "format": "date-time" }
  }
}
```

**JSON instance** (SDD example — see "Example usages" below):

```json
{
  "id": "018f5a2e-c0de-71ab-93f5-6e2a8c4d0b17",
  "name": "acme/backend",
  "description": "Shared engineering memory for the backend repo: specs, agent rules, decisions",
  "owner_type": "team",
  "owner_id": "team:acme-backend",
  "scope": "software-development",
  "participants": [
    { "principal_type": "group", "principal_id": "acme/backend-committers", "access_level": "promote", "granted_at": "2026-08-01T10:00:00Z" },
    { "principal_type": "user", "principal_id": "videogamer", "access_level": "write", "granted_at": "2026-08-10T12:00:00Z" }
  ],
  "access_policy": { "default_access": "read", "write": "owner_approved", "promote": "human_review" },
  "storage_backend": { "kind": "files", "config_ref": "git:acme/backend" },
  "artifacts": [
    { "uri": "AGENTS.md", "kind": "agent_doc", "role": "procedural", "revision": "main" },
    { "uri": ".claude/rules/*.md", "kind": "rule", "role": "procedural", "revision": "main" },
    { "uri": ".cursor/rules/*.mdc", "kind": "rule", "role": "procedural", "revision": "main" },
    { "uri": "specs/001-checkout/spec.md", "kind": "spec", "role": "semantic", "revision": "main" },
    { "uri": "specs/001-checkout/plan.md", "kind": "spec", "role": "semantic", "revision": "main" }
  ],
  "sync_state": { "status": "in_sync", "pending_proposals": 0, "revision": "main@9f3c2ab" },
  "last_synced_at": "2026-08-24T08:55:00Z",
  "retention_policy": { "supersede_not_delete": true },
  "created_at": "2026-08-01T09:00:00Z",
  "updated_at": "2026-08-24T08:55:00Z"
}
```

**YAML instance** (same SDD example):

```yaml
id: 018f5a2e-c0de-71ab-93f5-6e2a8c4d0b17
name: acme/backend
description: Shared engineering memory for the backend repo (specs, agent rules, decisions)
owner_type: team
owner_id: team:acme-backend
scope: software-development
participants:
  - { principal_type: group, principal_id: acme/backend-committers, access_level: promote, granted_at: "2026-08-01T10:00:00Z" }
  - { principal_type: user, principal_id: videogamer, access_level: write, granted_at: "2026-08-10T12:00:00Z" }
access_policy:
  default_access: read
  write: owner_approved
  promote: human_review
storage_backend:
  kind: files
  config_ref: git:acme/backend
artifacts:
  - { uri: AGENTS.md, kind: agent_doc, role: procedural, revision: main }
  - { uri: .claude/rules/*.md, kind: rule, role: procedural, revision: main }
  - { uri: .cursor/rules/*.mdc, kind: rule, role: procedural, revision: main }
  - { uri: specs/001-checkout/spec.md, kind: spec, role: semantic, revision: main }
  - { uri: specs/001-checkout/plan.md, kind: spec, role: semantic, revision: main }
sync_state:
  status: in_sync
  pending_proposals: 0
  revision: main@9f3c2ab
last_synced_at: "2026-08-24T08:55:00Z"
retention_policy:
  supersede_not_delete: true
created_at: "2026-08-01T09:00:00Z"
updated_at: "2026-08-24T08:55:00Z"
```

#### Example usages of SharedMemorySpace

The generic model covers any sharing topology. SDD (below) is one instantiation, not the model.

**Example 1 — Software development (team-owned, file/git backend).** The instance above: `owner_type: team`, `storage_backend: files`, artifacts are specs and rule files, writes gated by `owner_approved` + `promote: human_review`. This is the pattern of §6.3 [source: 10-spec-driven-development-memory].

**Example 2 — Customer support (organization-owned, hybrid backend).** A support organization shares resolution knowledge across all agent sessions and human agents:

```yaml
id: 018f5a2e-77aa-7c31-9d20-1b2c3d4e5f60
name: acme-support-kb
description: Shared resolution knowledge for support agents across sessions
owner_type: organization
owner_id: org:acme-support
scope: customer-support
participants:
  - { principal_type: group, principal_id: acme/support-agents, access_level: write }
  - { principal_type: agent, principal_id: agent:support-copilot, access_level: write }
access_policy:
  default_access: none
  write: participant_free
  promote: human_review
storage_backend:
  kind: hybrid
  config_ref: graph+vector:acme-support
artifacts: []
sync_state:
  status: in_sync
  pending_proposals: 3
  revision: "graph-epoch-4211"
last_synced_at: "2026-08-24T11:02:00Z"
retention_policy:
  supersede_not_delete: true
  archive_after_days: 365
```

Agents write resolutions freely (`participant_free`), but promotion of individual episodic memories into the shared knowledge graph still goes through human review [source: 02-mem0-universal-memory-layer, 10-spec-driven-development-memory].

**Example 3 — Single user, cross-session (`owner_type: user`).** One user's persistent memory across their own sessions and devices — `participants` may be empty or list other devices/users the user chose to share with. This is the shape Mem0 assumes when indexing memory per user [source: 02-mem0-universal-memory-layer]: same entity, `owner_type: user`, `storage_backend: vector`, no artifacts.

The SDD-specific concepts (git file-manifest, spec directories, diff review) live entirely in the `files`-backend instantiation: `storage_backend.kind: files`, `artifacts[].revision` holding git refs, and `access_policy.write: proposal_only` realized as PromotionProposal diffs. Nothing in the core schema requires them.

## Operational Log Records (non-core, F14)

§3.6 RecallRequest and §3.7 RecallResult live here, below the core entity definitions. They are **log records, not domain entities** — ephemeral operational/observability data with log-level retention (e.g., retain 30 days, then archive/purge). They persist only because the async API polls them and stats read them.

### 3.6 RecallRequest *(operational log record)*

> **Non-core entity (F14).** This is an observability log record, not a domain entity — it has no lifecycle state of interest to the domain beyond its log role. It persists because the async API polls it and stats read it. Retention: log-level (e.g., retain 30 days, then archive/purge). Schemas unchanged; classification only.

Emitted by the Recall Router, consumed by the Retrieval Engine [source: 06-hybrid-retrieval-strategies, 02-mem0-universal-memory-layer]. `top_k`: **default 50, max 200** (F6) — 50 is the sensible default for most recall operations; 200 accommodates the upper bound of instruction-slot budgets for agents that need more context.

| Field | Type | Req | Notes / Evidence |
|---|---|---|---|
| `request_id` | uuid | ✓ | |
| `query` | string | ✓ | embedded by the Engine |
| `context` | object | ✓ | `{entities[], task_signature?, time_bounds?, mentioned_files[]}` — entity extraction at the router [source: 02-mem0-universal-memory-layer] |
| `trigger` | enum | ✓ | `task_context` \| `user_query` \| `temporal` \| `associative` \| `session_start` — the four trigger classes + bootstrap [source: 06-hybrid-retrieval-strategies, 07-claude-code-memory] |
| `agent_id` / `session_id` | string/uuid | ✓ | |
| `retrieval_params` | object | ✓ | `{strategies[], top_k, rerank, min_score, slot_budget, token_budget}` — strategies choose the parallel paths; `rerank` mandatory in production [source: 06-hybrid-retrieval-strategies]; `top_k` default **50**, max **200** (F6 alignment) |
| `requested_at` | timestamp | ✓ | feeds temporal path and latency tracking |

**JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.org/schemas/ai-memory/recall-request.schema.json",
  "title": "RecallRequest",
  "type": "object",
  "required": ["request_id", "query", "context", "trigger", "agent_id", "session_id", "retrieval_params", "requested_at"],
  "properties": {
    "request_id": { "type": "string", "format": "uuid" },
    "query": { "type": "string", "minLength": 1 },
    "context": {
      "type": "object",
      "properties": {
        "entities": { "type": "array", "items": { "type": "string", "format": "uuid" } },
        "task_signature": { "type": "string" },
        "time_bounds": {
          "type": "object",
          "properties": {
            "from": { "type": "string", "format": "date-time" },
            "to": { "type": "string", "format": "date-time" }
          }
        },
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
        "strategies": {
          "type": "array",
          "minItems": 1,
          "items": { "enum": ["vector", "bm25", "graph", "temporal"] }
        },
        "top_k": { "type": "integer", "minimum": 1, "maximum": 200, "default": 50 },
        "rerank": { "enum": ["cross_encoder", "none"], "default": "cross_encoder" },
        "min_score": { "type": "number", "minimum": 0, "maximum": 1 },
        "slot_budget": { "type": "integer", "minimum": 0 },
        "token_budget": { "type": "integer", "minimum": 0 }
      }
    },
    "requested_at": { "type": "string", "format": "date-time" }
  }
}
```

**JSON instance:**

```json
{
  "request_id": "018f5a2e-f1d5-74c8-b2a7-3d9e5c1f7a62",
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
  "requested_at": "2026-08-24T09:03:11Z"
}
```

**YAML instance:**

```yaml
request_id: 018f5a2e-f1d5-74c8-b2a7-3d9e5c1f7a62
query: How does this project handle memory decay across sessions?
context:
  entities:
    - 018f5a2e-6d10-7c3f-9a1b-2e4c8d5f7a30
  task_signature: architecture-design
  time_bounds:
    from: "2026-07-01T00:00:00Z"
    to: null
  mentioned_files: [pages/architecture.md]
trigger: user_query
agent_id: claude-code
session_id: 018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80
retrieval_params:
  strategies: [vector, bm25, graph, temporal]
  top_k: 25
  rerank: cross_encoder
  min_score: 0.35
  slot_budget: 40
  token_budget: 4000
requested_at: "2026-08-24T09:03:11Z"
```

### 3.7 RecallResult *(operational log record)*

> **Non-core entity (F14).** Observability log record paired 1:1 with its request; the separate table keeps two fat arrays off the recall hot path. Same log-level retention as RecallRequest.

The Engine's answer after merge + re-rank, plus the Router's injection plan. Candidate sets merge across strategies before the cross-encoder re-rank [source: 06-hybrid-retrieval-strategies]; injections are counted against the instruction budget before entering working memory [source: 07-claude-code-memory].

| Field | Type | Req | Notes |
|---|---|---|---|
| `result_id` | uuid | ✓ | |
| `request_id` | uuid | ✓ | back-reference |
| `candidates` | object[] | ✓ | `{memory_id, score, rerank_score?, source_strategy[]}` pre/post re-rank |
| `injection_plan` | object[] | ✓ | ordered `{memory_id, position, slot_cost}` — what enters working memory, in what order |
| `slots_used` / `tokens_used` | integer | ✓ | charged against session budget |
| `latency_ms` | integer | | Zep targets sub-200ms [source: 03-zep-temporal-knowledge-graph] |

**JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.org/schemas/ai-memory/recall-result.schema.json",
  "title": "RecallResult",
  "type": "object",
  "required": ["result_id", "request_id", "candidates", "injection_plan", "slots_used", "tokens_used"],
  "properties": {
    "result_id": { "type": "string", "format": "uuid" },
    "request_id": { "type": "string", "format": "uuid" },
    "candidates": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["memory_id", "score", "source_strategies"],
        "properties": {
          "memory_id": { "type": "string", "format": "uuid" },
          "score": { "type": "number", "minimum": 0, "maximum": 1 },
          "rerank_score": { "type": "number", "minimum": 0, "maximum": 1 },
          "source_strategies": {
            "type": "array",
            "minItems": 1,
            "items": { "enum": ["vector", "bm25", "graph", "temporal"] }
          }
        }
      }
    },
    "injection_plan": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["memory_id", "position", "slot_cost"],
        "properties": {
          "memory_id": { "type": "string", "format": "uuid" },
          "position": { "type": "integer", "minimum": 1 },
          "slot_cost": { "type": "integer", "minimum": 0 }
        }
      }
    },
    "slots_used": { "type": "integer", "minimum": 0 },
    "tokens_used": { "type": "integer", "minimum": 0 },
    "latency_ms": { "type": "integer", "minimum": 0 }
  }
}
```

**JSON instance:**

```json
{
  "result_id": "018f5a2e-02b9-76da-c8f4-1a3e7b5c9d84",
  "request_id": "018f5a2e-f1d5-74c8-b2a7-3d9e5c1f7a62",
  "candidates": [
    {
      "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
      "score": 0.81,
      "rerank_score": 0.94,
      "source_strategies": ["vector", "graph"]
    },
    {
      "memory_id": "018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42",
      "score": 0.62,
      "rerank_score": 0.71,
      "source_strategies": ["bm25", "temporal"]
    }
  ],
  "injection_plan": [
    { "memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21", "position": 1, "slot_cost": 2 },
    { "memory_id": "018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42", "position": 2, "slot_cost": 1 }
  ],
  "slots_used": 3,
  "tokens_used": 412,
  "latency_ms": 187
}
```

**YAML instance:**

```yaml
result_id: 018f5a2e-02b9-76da-c8f4-1a3e7b5c9d84
request_id: 018f5a2e-f1d5-74c8-b2a7-3d9e5c1f7a62
candidates:
  - memory_id: 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21
    score: 0.81
    rerank_score: 0.94
    source_strategies: [vector, graph]
  - memory_id: 018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42
    score: 0.62
    rerank_score: 0.71
    source_strategies: [bm25, temporal]
injection_plan:
  - { memory_id: 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21, position: 1, slot_cost: 2 }
  - { memory_id: 018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42, position: 2, slot_cost: 1 }
slots_used: 3
tokens_used: 412
latency_ms: 187
```

### 3.8 PromotionProposal

The Shared Memory Sync Layer record implementing the ownership rule: individual memory is agent-written by default; shared memory is human-approved by default. Promotion = episodic → consolidated semantic → proposed diff → human-merged artifact [source: 10-spec-driven-development-memory, 07-claude-code-memory, 09-cursor-cursorrules-memory].

| Field | Type | Req | Notes |
|---|---|---|---|
| `proposal_id` | uuid | ✓ | |
| `shared_space_id` | uuid | ✓ | target SharedMemorySpace |
| `candidate_memory_id` | uuid | ✓ | the consolidated semantic memory being promoted |
| `target_artifact` | object | ✓ | `{path, kind, role}` — where in the file tree it lands |
| `diff` | string | ✓ | unified diff for human review |
| `status` | enum | ✓ | `draft` \| `in_review` \| `merged` \| `rejected` |
| `proposed_at` / `resolved_at` | timestamp | ✓/— | |
| `reviewer` | string? | | human who merged/rejected |

**JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.org/schemas/ai-memory/promotion-proposal.schema.json",
  "title": "PromotionProposal",
  "type": "object",
  "required": ["proposal_id", "shared_space_id", "candidate_memory_id", "target_artifact", "diff", "status", "proposed_at"],
  "properties": {
    "proposal_id": { "type": "string", "format": "uuid" },
    "shared_space_id": { "type": "string", "format": "uuid" },
    "candidate_memory_id": { "type": "string", "format": "uuid" },
    "target_artifact": {
      "type": "object",
      "required": ["path", "kind", "role"],
      "properties": {
        "path": { "type": "string" },
        "kind": { "enum": ["spec", "rule", "agent_doc", "memory_doc"] },
        "role": { "enum": ["procedural", "semantic", "episodic"] }
      }
    },
    "diff": { "type": "string", "minLength": 1 },
    "status": { "enum": ["draft", "in_review", "merged", "rejected"] },
    "proposed_at": { "type": "string", "format": "date-time" },
    "resolved_at": { "type": "string", "format": "date-time" },
    "reviewer": { "type": "string" }
  }
}
```

**JSON instance:**

```json
{
  "proposal_id": "018f5a2e-5e40-78f1-a9c3-7b2d6e4f8a15",
  "shared_space_id": "018f5a2e-c0de-71ab-93f5-6e2a8c4d0b17",
  "candidate_memory_id": "018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21",
  "target_artifact": {
    "path": "specs/002-agent-memory/spec.md",
    "kind": "spec",
    "role": "semantic"
  },
  "diff": "--- a/specs/002-agent-memory/spec.md\n+++ b/specs/002-agent-memory/spec.md\n@@ -12,6 +12,9 @@\n+Decay policy: supersession-first. Close the validity interval, open a new\n+version; deletion reserved for TTL expiry.\n",
  "status": "in_review",
  "proposed_at": "2026-08-24T09:12:00Z",
  "resolved_at": null,
  "reviewer": null
}
```

**YAML instance:**

```yaml
proposal_id: 018f5a2e-5e40-78f1-a9c3-7b2d6e4f8a15
shared_space_id: 018f5a2e-c0de-71ab-93f5-6e2a8c4d0b17
candidate_memory_id: 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21
target_artifact:
  path: specs/002-agent-memory/spec.md
  kind: spec
  role: semantic
diff: |-
  --- a/specs/002-agent-memory/spec.md
  +++ b/specs/002-agent-memory/spec.md
  @@ -12,6 +12,9 @@
  +Decay policy: supersession-first. Close the validity interval, open a new
  +version; deletion reserved for TTL expiry.
status: in_review
proposed_at: "2026-08-24T09:12:00Z"
resolved_at: null
reviewer: null
```

---

## 4. Relationships and Cardinalities

| Relationship | Cardinality | Enforcement / notes |
|---|---|---|
| AgentSession → Memory (`origin_session_id`) | 1 : N (0..* optional) | a session encodes many memories; file-artifact memories have no origin session |
| Memory → MemoryLink (`source_id` / `target_id`) | 1 : N each direction; Memory↔Memory is M : N through links | links are the join structure; the graph DB stores them natively |
| Memory ↔ Entity (`entities[]`, `anchors_entity` links) | M : N | a memory anchors many entities; an entity accumulates many memories |
| AgentSession → RecallRequest | 1 : N | one request per fired trigger; multiple triggers may fire per event (architecture §1.3) |
| RecallRequest → RecallResult | 1 : 1 | single merged+re-ranked result per request |
| RecallRequest/Result ↔ Memory (candidates) | M : N | retrieval selects from all memories; `source_strategies` records which path found each |
| SharedMemorySpace → Memory (`shared_space_id`) | 1 : N (0..* optional) | only memories with `access_scope = shared` |
| SharedMemorySpace → PromotionProposal | 1 : N | each proposal targets one artifact in one space |
| PromotionProposal → Memory (`candidate_memory_id`) | N : 1 | one candidate per proposal |
| Memory → Memory (`supersedes` / `validity.superseded_by`) | N : 1 chain | supersession chain preserves full history [source: 03-zep-temporal-knowledge-graph] |
| AgentSession → SharedMemorySpace (`shared_space_id`) | N : 1 (optional) | a session may run inside a shared space (deployment patterns S/H) |

### Entity Relationship Diagram

```
                ┌───────────────────┐            ┌───────────────────┐
                │   AgentSession    │            │ SharedMemorySpace │
                │───────────────────│            │───────────────────│
                │ session_id (PK)   │───────┐    │ id (PK)           │
                │ agent_type        │       │    │ owner_type/_id    │
                │ user_id           │       │    │ participants[]    │
                │ context_window    │       │    │ access_policy     │
                │ active_memories[]─┼─┐     │    │ storage_backend   │
                │ injection_order[] │ │     │    │ sync_state        │
                │ created/ended_at  │ │     │    │ last_synced_at    │
                │ summary           │ │     │    └─────────┬─────────┘
                └───────┬───────────┘ │     │              │
                        │ 1           │     │              │
                        │             │     └─── N : 1 ────┤ (session runs in space)
                        │ N           │                    │ 1
                 ┌──────▼───────────┐ │                    │
                 │  RecallRequest   │ │            ┌───────▼──────────┐
                 │──────────────────│ │            │ PromotionProposal│
                 │ request_id (PK)  │ │            │──────────────────│
                 │ query            │ │            │ proposal_id (PK) │
                 │ context          │ │            │ candidate_       │
                 │ trigger          │ │            │   memory_id (FK)─┼──┐
                 │ retrieval_params │ │            │ target_artifact  │  │
                 │ requested_at     │ │            │ shared_space_id  │  │
                 └──────┬───────────┘ │            └──────────────────┘  │                        │ 1           │                                  │
                 ┌──────▼───────────┐ │                                  │
                 │   RecallResult   │ │                                  │
                 │──────────────────│ │                                  │
                 │ result_id (PK)   │                                  │
                 │ request_id (FK)  │                                  │
                 │ candidates[] ────┼──────────── M : N ─────────┐      │
                 │ injection_plan[]─┼────────────────────────┐   │      │
                 └──────────────────┘                        │   │      │
                                                             ▼   ▼      ▼
   ┌─────────────────────────────────────────────────────────────────────────┐
   │             M E M O R Y     (id: PK, access_scope: individual|shared)   │
   │──────────────────────────────────────────────────────────────────────────│
   │ id · type(episodic|semantic|procedural) · content · embedding            │
   │ provenance · source_ref · timestamps · confidence · decay_score         │
   │ validity{valid_from, valid_until, superseded_by} · ttl · version        │
   │ origin_session_id (FK) · shared_space_id (FK, iff shared scope)         │
   └───────┬───────────────────────────────┬─────────────────────────────────┘
           │ M : N (entities[])            │ 1 : N (source_id/target_id)
   ┌───────▼──────────┐           ┌────────▼─────────┐         ┌──────────────────┐
   │      Entity      │           │   MemoryLink     │────────▶│  Memory (peer)   │
   │──────────────────│           │──────────────────│  M : N  │  (self-reference │
   │ entity_id (PK)   │           │ id (PK)          │  via    │   supersedes /   │
   │ name, type       │           │ source_id (FK)   │  links) │   derived_from   │
   │ aliases[]        │           │ target_id (FK)   │         └──────────────────┘
   │ facts[]          │           │ relationship_    │
   │  {fact, valid_   │           │   type, weight   │
   │   from/until}    │           │ evidence         │
   └──────────────────┘           └──────────────────┘
```

Key structural reads:

- **MemoryLink makes Memory a graph**, not a flat list — graph traversal retrieval operates directly on this relation [source: 06-hybrid-retrieval-strategies, 04-a-mem-agentic-memory-architecture].
- **The supersession chain is a linked list inside the graph**: `Memory --supersedes--> Memory` encodes version history with temporal validity [source: 03-zep-temporal-knowledge-graph].
- **Working memory is not a table**: `AgentSession.active_memories` + `injection_plan` describe what is in-context; the context window itself is the working tier [source: 01-letta-memgpt-stateful-agents, 05-coala-memory-types-taxonomy].
- **Shared memory is not a store membership**: when file-backed, `SharedMemorySpace.artifacts` points at files; the PromotionProposal is the only write path into it [source: 10-spec-driven-development-memory].

---

## 5. Storage Backend Mapping

Binding each entity to the four backend classes of architecture §3.1 [source: 01-letta-memgpt-stateful-agents, 06-hybrid-retrieval-strategies, 07-claude-code-memory, 10-spec-driven-development-memory]:

| Entity | Relational / metadata | Vector DB | Graph DB | Files (git) |
|---|---|---|---|---|
| **Memory** | **system of record**: all scalar fields, validity, provenance, version | embedding vectors keyed by `id` | node per memory when it participates in links; embeddings as node properties in co-located deployments (FalkorDB pattern) [source: 06-hybrid-retrieval-strategies] | procedural and team-scope memories are *projected* to files (§6); the file is the interop format [source: 07-claude-code-memory, 10-sdd] |
| **MemoryLink** | mirror for referential integrity | — | **primary home**: edges with `relationship_type` and `weight` [source: 03-zep-temporal-knowledge-graph, 04-a-mem-agentic-memory-architecture] | — |
| **Entity** | mirror of registry | optional embedding of `name`+`aliases` | **primary home**: entity nodes + temporal fact edges (Graphiti pattern) [source: 03-zep-temporal-knowledge-graph] | — |
| **AgentSession** | session metadata, budgets, timestamps | — | — | working tier lives in the runtime context window, not in any store [source: 01-letta-memgpt-stateful-agents] |
| **SharedMemorySpace** | manifest (when file-backed) + sync state + access policy | — | optional: shared semantic nodes when graph-backed | **primary home when `storage_backend: files`**: the artifacts themselves (`specs/`, `.claude/rules/`, `.cursor/rules/`, `AGENTS.md`) under version control; vector/graph backends realize it in their own store [source: 10-sdd, 07, 09] |
| **RecallRequest / RecallResult** | append-only observability log | — | — | — (ephemeral) |
| **PromotionProposal** | status tracking | — | — | the `diff` is authored as a file change / PR [source: 10-sdd] |

**Backend decision echoes:** prefer vector-in-graph co-location where available to eliminate the vector↔graph network hop [source: 06-hybrid-retrieval-strategies]; Letta's precedent keeps conversation metadata in a conventional DB and vectors in a dedicated index [source: 01-letta-memgpt-stateful-agents]; files remain the procedural/team tier in *all* deployment patterns because review-ability is what makes team memory trustworthy [source: 10-sdd, 09-cursor-cursorrules-memory].

---

## 6. Real-Tool Integration Mapping

How the models project onto the concrete file shapes the major tools read natively — markdown is the only memory format all three tools consume [source: 07-claude-code-memory, 08-codex-copilot-persistent-memory, 09-cursor-cursorrules-memory].

### 6.1 Claude Code — `CLAUDE.md` / `.claude/rules/` as procedural Memory

Each instruction file in the load hierarchy is a procedural `Memory` with `access_scope: shared` (or `individual` for `~/.claude/CLAUDE.md`), `type: procedural`, and `source_ref.kind: file`. Load order is fixed and concatenated, never overridden [source: 07-claude-code-memory]:

```
enterprise/user memory → global CLAUDE.md → project CLAUDE.md
  → .claude/rules/*.md → conditional rules → first user message
```

That order is recorded verbatim in `AgentSession.injection_order`. A rule file with YAML front matter carrying the data-model fields:

```markdown
# .claude/rules/memory-decay.md
---
memory_id: 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21
type: procedural
access_scope: shared
shared_space_id: 018f5a2e-c0de-71ab-93f5-6e2a8c4d0b17
tags: [decay-policy, architecture]
confidence: 0.9
validity:
  valid_from: "2026-08-23T23:45:12Z"
version: 1
source_ref:
  path: .claude/rules/memory-decay.md
---
# Memory decay policy

Supersession-first: close the validity interval, open a new version.
Deletion is reserved for TTL expiry.
```

The markdown body is `content`; the front matter is the scalar projection of the Memory schema. The `source_ref.hash` lets the Shared Memory Sync Layer detect out-of-band edits. The instruction-slot fields on `AgentSession.context_window` exist because Claude Code reliably follows ~150–200 instructions, ~50 consumed by its own system prompt [source: 07-claude-code-memory]. Agent-written `MEMORY.md` notes are episodic/semantic memories with `provenance.actor: agent` and the same front-matter projection [source: 07-claude-code-memory].

### 6.2 Cursor — `.cursor/rules/*.mdc` as procedural Memory

Same projection, Cursor's container. Rule files carry front matter the tool itself understands (`description`, `globs`, `alwaysApply`); the memory fields ride alongside [source: 09-cursor-cursorrules-memory]:

```markdown
# .cursor/rules/memory-decay.mdc
---
description: Memory decay policy for agent sessions
globs: ["pages/**", "specs/**"]
alwaysApply: false
memory_id: 018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21
type: procedural
access_scope: shared
tags: [decay-policy]
version: 1
---
Supersession-first decay: close the validity interval, open a new version.
```

The deprecated root `.cursorrules` maps to the same model with `source_ref.path: .cursorrules`; `AGENTS.md` at project root is the tool-portable canonical source from which tool-specific rule files are generated [source: 09-cursor-cursorrules-memory]. The Memory Bank community pattern is episodic structure realized as markdown files; the Recall tool's session-start injection is `trigger: session_start` in RecallRequest terms [source: 09-cursor-cursorrules-memory].

### 6.3 Spec Driven Development — `specs/[feature]/` as team semantic Memory

The Spec Kit directory contract is one `SharedMemorySpace` artifact list (`storage_backend: files`) [source: 10-spec-driven-development-memory]:

```
specs/
  001-checkout/
    spec.md          → Memory{type: semantic, access_scope: shared} (decisions)
    plan.md          → Memory{type: semantic, role: plan}          (steps)
    research.md      → Memory{type: episodic, aggregated}          (session research log)
    data-model.md    → Memory{type: semantic, role: schema}        (this page's counterpart)
    quickstart.md    → Memory{type: procedural}                    (how to run it)
```

- Git commits are consolidation events; history is the episodic record; reverts are decay — the SDD insight that the specs are the memory that compounds, so version the specs, not just the code they produced [source: 10-spec-driven-development-memory].
- The session-end spec update is the mandated write discipline: session end fires the Lifecycle Manager, which emits a `PromotionProposal` (diff) rather than writing the spec directly [source: 10-spec-driven-development-memory].
- Codex has no native persistence (client-side rollout files only), so in a Codex session the entire model is realized through an MCP memory server speaking these schemas — the models are the integration contract [source: 08-codex-copilot-persistent-memory].

### 6.4 Cross-tool portability rule

Because every tool reads markdown, the **file projection is the interchange format**: any Memory with `access_scope: shared` must be renderable to a front-mattered markdown file, and any such file must be re-ingestible into the Memory schema losslessly. Databases optimize retrieval; files remain the system of record for everything participants review [source: 07-claude-code-memory, 09-cursor-cursorrules-memory, 10-spec-driven-development-memory].

---

## 7. Lifecycle Operations on the Models

How the Lifecycle Manager's `MutationOps` (architecture §2.2) mutate these records:

| Operation | Effect on records | Evidence |
|---|---|---|
| **expire** (TTL) | set `validity.valid_until = now`, `decay_score = 0`; physical delete only here | [source: 06-hybrid-retrieval-strategies] |
| **supersede** | new Memory row `version = n+1`; old row gets `validity.valid_until = now, superseded_by = new.id`; `MemoryLink{type: supersedes}` created | [source: 03-zep-temporal-knowledge-graph] |
| **consolidate** | episodic set → new semantic Memory with `provenance.origin: consolidation`; `MemoryLink{type: derived_from}` from each source | [source: 01-letta-memgpt-stateful-agents, 02-mem0-universal-memory-layer] |
| **compact** | conversation prefix → `AgentSession.summary` (recursive summarization); original episodic records retained in store | [source: 01-letta-memgpt-stateful-agents] |
| **promote** | semantic Memory → `PromotionProposal{status: in_review}` → human merge flips `Memory.access_scope` to `shared` and attaches `shared_space_id` | [source: 10-spec-driven-development-memory] |
| **re-index delta** | Index Manager consumes the mutation and updates only affected vector/BM25/graph/temporal entries | [source: 06-hybrid-retrieval-strategies] |

---

## Uncertainty Notes

- **Temporal validity on Entity.facts and Memory.validity** is Zep-derived and single-source in this evidence set [source: 03-zep-temporal-knowledge-graph]; the field is designed to be nullable so systems without temporal semantics can omit it.
- **Instruction-slot budgeting** (~150–200, ~50 system-consumed) is Claude-specific guidance [source: 07-claude-code-memory]; the *fields* generalize, the *numbers* may not.
- **`confidence` semantics** are not standardized across sources; this model treats it as an encoder-local score refined by repetition (architecture encoding policy), not a calibrated probability.
- **UUIDv7 identifiers** are an implementation convention of this model, not something any source mandates.
- **Vector-in-graph co-location** (embeddings as node properties) reflects the FalkorDB pattern from one source [source: 06-hybrid-retrieval-strategies]; separate vector + graph stores remain fully supported by the mapping in §5.

## Related Pages

- [[pages/architecture|Architecture]] — the conceptual/logical/physical layers these models instantiate
- [[pages/memory-taxonomy|Memory Taxonomy]] — the source of the `Memory.type` enum
- [[pages/retrieval-and-recall|Retrieval and Recall]] — the evidence behind `RecallRequest.retrieval_params`
- [[ai-memory-research|Canonical overview]]

## Tags

- #ai-agents
- #memory-systems
- #data-models
- #schemas
- #architecture
- #spec-driven-development
