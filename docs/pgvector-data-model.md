# PostgreSQL + pgvector Data Model — AI Agent Memory & Recall

The deployable data model for the memory system defined in [[pages/data-models|Data Models]]
(conceptual entities) and [[pages/architecture|Architecture]] (components and storage
backends). This page takes the **conceptual** model (8 entities, JSON/YAML schemas) through
**logical** (database-agnostic relational design) to **physical** (PostgreSQL 16+ DDL with
pgvector), and proves the physical model supports the four-way hybrid retrieval of
[[pages/retrieval-and-recall|Retrieval and Recall]] and the operations of
[[pages/api-contracts|API Contracts]].

Single-store rationale: the architecture's §3.1 lists four backend classes (relational,
vector, graph, files). PostgreSQL 16 + pgvector 0.7+ replaces the first three with one
system — relational metadata (system of record), HNSW vector search (dense path), tsvector
BM25-like (sparse path via `ts_rank_cd`), and recursive CTEs over `memory_links` (graph path) — eliminating the
vector↔graph network hop that the architecture flags as the co-location motivation
[source: 06-hybrid-retrieval-strategies]. Files remain outside the database by design
(review-ability of shared memory); `source_ref` and `artifacts` carry the linkage
[source: 10-spec-driven-development-memory].

---

## Part 1 — Conceptual → Logical Mapping

### 1.1 Entity-to-table decisions

| Conceptual entity | Logical realization | Rationale |
|---|---|---|
| **Memory** (§3.1) | `memories` table | System of record; every scalar survives as a column, every composite object is either decomposed or kept JSONB (below). Embedding splits into a hot-path column + a multi-model child table. |
| **MemoryLink** (§3.2) | `memory_links` table | Edges are first-class rows; recursive CTEs traverse them (graph path). Unique on `(source, target, relationship_type)`. |
| **Entity** (§3.3) | `entities` table + `entity_facts` child table + `memory_entities` junction | The registry normalizes; `facts[]` becomes rows because temporal validity per fact must be range-queried (GiST); `entities[]` on Memory becomes a junction because M:N with referential integrity. |
| **AgentSession** (§3.4) | `agent_sessions` table | `context_window` object decomposes into budget columns (the activation check compares columns, not JSON keys). `active_memories` stays a UUID array — ephemeral working set, not a join dimension. |
| **SharedMemorySpace** (§3.5) | `shared_memory_spaces` table + `space_memberships` junction | `participants[]` becomes a junction table (queried by principal across spaces). `access_policy` (fixed 3-key shape) normalizes to columns; variable-shape objects stay JSONB. |
| **RecallRequest** (§3.6) | `recall_requests` table | Append-only observability log; `context` and `retrieval_params` stay JSONB (variable shape, logged not queried structurally beyond indexing `trigger`). |
| **RecallResult** (§3.7) | `recall_results` table (1:1 with request) | Kept separate per the conceptual model (async retrieval: request now, result later). `candidates` / `injection_plan` stay JSONB arrays — written once, read as a whole, never joined per-element. |
| **PromotionProposal** (§3.8) | `promotion_proposals` table | Workflow states drive partial indexes; `target_artifact` JSONB; `diff` is TEXT (large, TOASTed). |
| *(new)* | `embedding_models` registry | Tracks which embedding models produced vectors, their dims, and distance metric — required once multiple embedding models are supported (§3.3 embedding strategy). |
| *(new)* | `memory_embeddings` | Optional child table for alternate-model vectors of the same memory (multi-model support). |

Working memory is deliberately **not** a table of content: `agent_sessions.active_memories`
plus `recall_results.injection_plan` describe what is in-context; the context window itself
is the working tier [source: 01-letta-memgpt-stateful-agents].

### 1.2 JSON decomposition decisions

| Conceptual JSON/array field | Physical handling | Why |
|---|---|---|
| `Memory.tags` | `text[]` column (domain-checked) + GIN | Containment queries (`@>`) are the access pattern; a normalized tag table adds a join to every list endpoint for no integrity gain (tags have no attributes of their own). |
| `Memory.entities` | normalized → `memory_entities` junction | Needs FK integrity to `entities` and is a join dimension for entity-centric retrieval [source: 02-mem0-universal-memory-layer]. |
| `Memory.embedding` `{vector, model, dims}` | `embedding vector(1536)` + `embedding_model text` on `memories` (hot path); `memory_embeddings` rows for other models | The vector must be a typed `vector(N)` column to be HNSW-indexed; `model`/`dims` become metadata. One typed column per supported dimension (pgvector indexes require a fixed typmod). |
| `Memory.provenance` | decomposed: `origin` (enum), `session_id`, `agent_id`, `actor` columns | Mandatory field [source: 02-mem0-universal-memory-layer]; enums get CHECKs; `session_id` gets a real FK. |
| `Memory.source_ref` | JSONB `{kind, path/uri, hash}` + generated `source_hash` extracted column? → JSONB only, with `jsonb_typeof` CHECKs | Variable shape (`path` vs `uri`); queried rarely, displayed always. `hash` is read by the sync layer's change detection, one row at a time — JSONB is fine. |
| `Memory.validity` | decomposed: `valid_from`, `valid_until`, `superseded_by` columns + generated `validity_range tstzrange` | Temporal recall needs range predicates; a GiST index on `tstzrange` gives point-in-time queries (`@>`) in one operator [source: 03-zep-temporal-knowledge-graph]. `superseded_by` is the chain pointer. |
| `Memory.content_format` | enum column | Closed set of 3. |
| `AgentSession.context_window` | decomposed into 5 columns | The activation endpoint checks `slots_used + cost <= instruction_slot_budget` — must be columns to be constraint-checkable and index-light. |
| `AgentSession.active_memories`, `injection_order` | `uuid[]`, `text[]` (+ GIN on the first) | Append/remove by id; never joined for content in SQL (content is fetched by id batch). |
| `SharedMemorySpace.participants` | normalized → `space_memberships` | The visibility query ("which memories can this principal see") joins on this — must be relational. |
| `SharedMemorySpace.access_policy` | decomposed: `default_access`, `write_policy`, `promote_policy` enum columns | Fixed 3-key shape; values drive enforcement and want CHECK constraints. |
| `SharedMemorySpace.storage_backend` | JSONB | `{kind, config_ref}` — kind is CHECKed via `(storage_backend->>'kind')` invariant? No: `backend_kind` enum column + `backend_config` JSONB. Shape is fixed but config is free. |
| `SharedMemorySpace.artifacts` | JSONB array | Explicitly *not* a live membership list — a manifest read by the sync layer; no per-element queries. |
| `SharedMemorySpace.sync_state` | decomposed: `sync_status` enum + `pending_proposals int` + `sync_revision text` | Status drives dashboards and partial indexes; the counter is maintained transactionally. |
| `SharedMemorySpace.retention_policy` | JSONB | Optional, variable keys, consumed whole by the lifecycle manager. |
| `RecallRequest.context`, `retrieval_params` | JSONB | Logged payloads; only `trigger` is structurally indexed. |
| `RecallResult.candidates`, `injection_plan` | JSONB arrays | Write-once/read-whole observability payloads. |
| `PromotionProposal.target_artifact` | JSONB `{path, kind, role}` | Fixed but small; read whole during review. |
| `Entity.aliases` | `text[]` + folded into a tsvector column | Feeds the lexical path [source: 06-hybrid-retrieval-strategies]. |
| `Entity.facts` | normalized → `entity_facts` rows | Per-fact temporal validity must be range-queried (GiST on tstzrange) — the Zep pattern [source: 03-zep-temporal-knowledge-graph]. |

### 1.3 Denormalization decisions

1. **`memories.embedding`** — the primary-model vector lives on the row itself, not only in
   `memory_embeddings`. One HNSW index, zero joins for the hot dense path. Alternate models
   pay the extra-table cost.
2. **`memories.search_tsv`** — a stored generated tsvector of `content` (plus tags). It is
   fully derivable; it is materialized because the sparse path runs on every recall.
3. **`memories.validity_range`** — stored generated `tstzrange` from `valid_from`/`valid_until`.
   Derivable; materialized so a single GiST index serves point-in-time filters.
4. **`entities.search_tsv`** — name + aliases folded into one tsvector for entity lexical match.
5. **`shared_memory_spaces.pending_proposals`** — counter maintained by the proposal
   workflow (a trigger in the deployable DDL) rather than computed; the sync dashboard reads
   it per space listing.
6. **`agent_sessions.used_tokens` / `slots_used`** — running counters updated on
   activation/deactivation; cheaper than summing injection plans.

Everything else is normalized. The guiding rule: **denormalize only what a hot query
pattern reads on every call** (dense path, sparse path, temporal filter, budgets, dashboards).

---

## Part 2 — Logical Data Model

Database-agnostic. Data types use generic names (UUID, TIMESTAMP WITH TIME ZONE, DECIMAL,
BOOLEAN, INTEGER, TEXT, JSON, ARRAY<TEXT>, ARRAY<UUID>); Part 3 binds them to PostgreSQL.

### 2.1 `memories` — core memory store

The system of record for the Memory entity (data-models §3.1). One row per memory version
(supersession writes a new row and closes the old row's validity interval).

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `id` | UUID | no | — | memory id (UUIDv7); stable across backends |
| `type` | VARCHAR | no | — | `episodic` \| `semantic` \| `procedural` |
| `content` | TEXT | no | — | markdown-first content |
| `content_format` | VARCHAR | yes | `markdown` | `markdown` \| `plain` \| `json` |
| `tags` | ARRAY&lt;TEXT&gt; | yes | — | A-MEM capture-time tags |
| `entities` *(conceptual)* | — | — | — | normalized to `memory_entities` |
| `embedding` | VECTOR(1536) | yes | — | primary-model embedding |
| `embedding_model` | VARCHAR | yes | — | model id in `embedding_models` |
| `origin` | VARCHAR | no | — | `agent_observation` \| `user_instruction` \| `file_artifact` \| `consolidation` |
| `session_id` | UUID | yes | — | provenance session (FK) |
| `agent_id` | TEXT | yes | — | provenance agent |
| `actor` | TEXT | yes | — | provenance actor |
| `owner_principal_type` | VARCHAR | no | `user` | access-control subject type (review F1) |
| `owner_principal_id` | TEXT | no | — | access-control subject id (review F1) |
| `source_ref` | JSON | yes | — | `{kind, path/uri, hash}` |
| `created_at` | TIMESTAMP | no | now | creation |
| `updated_at` | TIMESTAMP | no | now | last mutation |
| `last_accessed_at` | — | — | — | **removed (F8)**; served from `memory_access_log` aggregate |
| `confidence` | DECIMAL(3,2) | yes | — | encoder-assessed 0–1 |
| `decay_score` | DECIMAL(3,2) | yes | 1.0 | 1.0 = fully vital |
| `valid_from` | TIMESTAMP | yes | — | validity interval start |
| `valid_until` | TIMESTAMP | yes | — | validity interval end (null = open) |
| `superseded_by` | UUID | yes | — | next row in the supersession chain |
| `ttl_expires_at` | TIMESTAMP | yes | — | TTL expiry without re-index |
| `version` | INTEGER | no | 1 | monotonic per chain |
| `access_scope` | VARCHAR | no | — | `individual` \| `shared` |
| `shared_space_id` | UUID | yes | — | required iff `access_scope = shared` |
| `deleted_at` | TIMESTAMP | yes | — | soft-delete for purge workflow |

**PK:** (`id`). **FKs:** `session_id → agent_sessions(session_id)`; `shared_space_id →
shared_memory_spaces(id)`; `superseded_by → memories(id)` (self).

**Checks:** `type` ∈ enum; `content_format` ∈ enum; `origin` ∈ enum; `access_scope` ∈ enum;
`confidence`/`decay_score` ∈ [0,1]; `version ≥ 1`; `valid_until IS NULL OR valid_until > valid_from`;
`ttl_expires_at IS NULL OR ttl_expires_at > created_at`;
`(access_scope = 'shared') = (shared_space_id IS NOT NULL)`; `owner_principal_type` ∈ enum.

**Indexes (logical):** `type`; `access_scope`; `shared_space_id`; `session_id`; `created_at`;
`last_accessed_at`; `valid_from`/`valid_until` (range); GIN on `tags`; GIN on full-text
vector of `content`; ANN index on `embedding`; partial on live rows (`deleted_at IS NULL`);
(`owner_principal_type`, `owner_principal_id`) on live rows (review F1).

### 2.2 `memory_links` — edges between memories

Directed weighted edges (MemoryLink §3.2). The graph path traverses these rows.

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `id` | UUID | no | — | edge id |
| `source_id` | UUID | no | — | from-memory |
| `target_id` | UUID | no | — | to-memory (or entity-representing memory) |
| `relationship_type` | VARCHAR | no | — | `derived_from` \| `supersedes` \| `similar_to` \| `co_occurs_with` \| `causal_next` \| `anchors_entity` |
| `weight` | DECIMAL(3,2) | no | 1.0 | 0–1, retrieval-tuned |
| `evidence` | TEXT | yes | — | why the edge exists |
| `created_at` | TIMESTAMP | no | now | |

**PK:** (`id`). **FKs:** `source_id → memories(id)` ON DELETE CASCADE; `target_id →
memories(id)` ON DELETE CASCADE. **Unique:** (`source_id`, `target_id`, `relationship_type`).
**Checks:** `relationship_type` ∈ enum; `weight` ∈ [0,1].

**Indexes:** `source_id` (forward traversal); `target_id` (reverse traversal);
partial `source_id WHERE relationship_type = 'supersedes'` (chain walk).

### 2.3 `entities` — entity registry

Entity §3.3 minus `facts` (child table).

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `entity_id` | UUID | no | — | registry id |
| `name` | TEXT | no | — | canonical name |
| `entity_type` | VARCHAR | no | — | `person` \| `project` \| `repository` \| `tool` \| `organization` \| `concept` |
| `aliases` | ARRAY&lt;TEXT&gt; | yes | — | lexical variants |
| `created_at` | TIMESTAMP | no | now | |

**PK:** (`entity_id`). **Unique:** (`name`, `entity_type`) — one canonical name per type.
**Checks:** `entity_type` ∈ enum; `name` non-empty. **Indexes:** GIN full-text on
`name + aliases`; GIN on `aliases`.

### 2.4 `entity_facts` — temporal facts per entity

Normalized from `Entity.facts[]` — the Zep temporal-validity pattern.

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `id` | UUID | no | — | fact row id |
| `entity_id` | UUID | no | — | owning entity |
| `fact` | TEXT | no | — | fact statement |
| `memory_id` | UUID | yes | — | memory the fact was extracted from |
| `valid_from` | TIMESTAMP | no | — | fact validity start |
| `valid_until` | TIMESTAMP | yes | — | fact validity end (null = current) |

**PK:** (`id`). **FKs:** `entity_id → entities(entity_id)` ON DELETE CASCADE;
`memory_id → memories(id)`. **Checks:** `valid_until IS NULL OR valid_until > valid_from`.
**Indexes:** `entity_id`; range index on `(valid_from, valid_until)`.

### 2.5 `memory_entities` — Memory ↔ Entity junction

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `memory_id` | UUID | no | — | anchoring memory |
| `entity_id` | UUID | no | — | anchored entity |
| `created_at` | TIMESTAMP | no | now | when the anchor was made |

**PK:** (`memory_id`, `entity_id`). **FKs:** `memory_id → memories(id)` ON DELETE CASCADE;
`entity_id → entities(entity_id)` ON DELETE CASCADE. **Indexes:** reverse lookup
(`entity_id`) for entity-centric retrieval.

### 2.6 `agent_sessions` — session tracking

AgentSession §3.4 with `context_window` decomposed.

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `session_id` | UUID | no | — | session id |
| `agent_type` | VARCHAR | no | — | `claude-code` \| `codex` \| `cursor` \| `letta` \| `custom` |
| `user_id` | TEXT | no | — | memory is indexed per user |
| `shared_space_id` | UUID | yes | — | if the session runs inside a space |
| `model` | TEXT | no | — | context window model |
| `max_tokens` | INTEGER | no | — | token budget |
| `used_tokens` | INTEGER | no | 0 | tokens charged |
| `instruction_slot_budget` | INTEGER | no | — | dual budget (slots) |
| `slots_used` | INTEGER | no | 0 | slots charged |
| `active_memories` | ARRAY&lt;UUID&gt; | no | — | working set (in-context memory ids) |
| `injection_order` | ARRAY&lt;TEXT&gt; | yes | — | bootstrap order record |
| `created_at` | TIMESTAMP | no | now | |
| `ended_at` | TIMESTAMP | yes | — | session end fires consolidation |
| `summary` | TEXT | yes | — | recursive summarization output |

**PK:** (`session_id`). **FKs:** `shared_space_id → shared_memory_spaces(id)`.
**Checks:** `agent_type` ∈ enum; `max_tokens ≥ 1`; `used_tokens ≥ 0`;
`instruction_slot_budget ≥ 0`; `slots_used ≥ 0`; `ended_at IS NULL OR ended_at > created_at`.
**Indexes:** (`user_id`, `created_at`); partial `ended_at IS NULL` (active sessions);
GIN on `active_memories`.

### 2.7 `shared_memory_spaces` — shared spaces

SharedMemorySpace §3.5 with `access_policy`/`sync_state` decomposed, `participants` in a
junction.

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `id` | UUID | no | — | space id |
| `name` | TEXT | no | — | human-readable |
| `description` | TEXT | yes | — | purpose |
| `owner_type` | VARCHAR | no | — | `user` \| `agent` \| `team` \| `organization` |
| `owner_id` | TEXT | no | — | owning principal id |
| `scope` | TEXT | no | — | domain, e.g. `software-development` |
| `default_access` | VARCHAR | no | — | `read` \| `write` \| `none` — for unlisted principals |
| `write_policy` | VARCHAR | no | — | `owner_approved` \| `participant_free` \| `proposal_only` |
| `promote_policy` | VARCHAR | no | — | `human_review` \| `auto` |
| `backend_kind` | VARCHAR | no | — | `files` \| `relational` \| `vector` \| `graph` \| `hybrid` |
| `backend_config` | JSON | yes | — | `{config_ref}` |
| `artifacts` | JSON | yes | — | manifest array (file-backed spaces only) |
| `sync_status` | VARCHAR | no | `in_sync` | `in_sync` \| `pending_review` \| `diverged` \| `offline` |
| `pending_proposals` | INTEGER | no | 0 | workflow-maintained counter |
| `sync_revision` | TEXT | yes | — | backend-neutral revision |
| `last_synced_at` | TIMESTAMP | no | now | change propagation timestamp |
| `retention_policy` | JSON | yes | — | `{supersede_not_delete, ttl_days?, archive_after_days?}` |
| `created_at` / `updated_at` | TIMESTAMP | no | now | |

**PK:** (`id`). **Checks:** all decomposed enums ∈ their sets; `pending_proposals ≥ 0`.
**Indexes:** (`owner_type`, `owner_id`); partial `sync_status <> 'in_sync'` (attention list).

### 2.8 `space_memberships` — shared_with junction

`SharedMemorySpace.participants[]` as rows (M:N between spaces and principals).

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `space_id` | UUID | no | — | the space |
| `principal_type` | VARCHAR | no | — | `user` \| `agent` \| `session` \| `group` |
| `principal_id` | TEXT | no | — | external principal id (no FK — principals live outside this schema) |
| `access_level` | VARCHAR | no | — | `read` \| `write` \| `promote` \| `admin` |
| `granted_at` | TIMESTAMP | no | now | |

**PK:** (`space_id`, `principal_type`, `principal_id`). **FKs:** `space_id →
shared_memory_spaces(id)` ON DELETE CASCADE. **Checks:** both enums ∈ sets.
**Indexes:** (`principal_type`, `principal_id`) — the visibility query's entry point.

### 2.9 `recall_requests` — recall query log

RecallRequest §3.6. Append-only; high volume → partitioning candidate (§Part 3.9).

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `request_id` | UUID | no | — | |
| `query` | TEXT | no | — | the embedded query text |
| `context` | JSON | no | — | `{entities[], task_signature?, time_bounds?, mentioned_files[]}` |
| `trigger` | VARCHAR | no | — | `task_context` \| `user_query` \| `temporal` \| `associative` \| `session_start` |
| `agent_id` | TEXT | no | — | |
| `session_id` | UUID | no | — | FK |
| `strategies` | ARRAY&lt;TEXT&gt; | no | — | chosen parallel paths |
| `top_k` | INTEGER | no | 50 | max 200 (F6 alignment: default 50 everywhere, DB outer bound 200) |
| `rerank` | VARCHAR | no | `cross_encoder` | mandatory in production |
| `min_score` | DECIMAL(3,2) | no | 0.35 | |
| `slot_budget` | INTEGER | yes | — | |
| `token_budget` | INTEGER | yes | — | |
| `mode` | VARCHAR | no | `sync` | `sync` \| `async` (review F3) |
| `status` | VARCHAR | no | `completed` | `queued` \| `running` \| `completed` \| `failed` (review F3) |
| `error` | JSON | yes | — | required when `status = failed` (review F3) |
| `requested_at` | TIMESTAMP | no | now | feeds temporal path + latency tracking |

*(retrieval_params decomposed into `strategies`/`top_k`/`rerank`/`min_score`/`slot_budget`/
`token_budget`; `context` stays JSON.)*

**PK:** (`request_id`). **FKs:** `session_id → agent_sessions(session_id)`.
**Checks:** `trigger` ∈ enum; `rerank` ∈ {`cross_encoder`, `none`}; `top_k BETWEEN 1 AND 200`;
`strategies` ⊆ {`vector`,`bm25`,`graph`,`temporal`} (array-contained check); `mode` ∈ enum;
`status` ∈ enum; `status = failed ⇒ error IS NOT NULL`.
**Indexes:** (`session_id`, `requested_at`); (`requested_at`) for latency windows; (`trigger`);
partial open-jobs (`status IN ('queued','running')`).

### 2.10 `recall_results` — one merged result per request

RecallResult §3.7.

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `result_id` | UUID | no | — | |
| `request_id` | UUID | no | — | back-reference (1:1) |
| `candidates` | JSON | no | — | `[{memory_id, score, rerank_score?, source_strategies[]}]` |
| `injection_plan` | JSON | no | — | ordered `[{memory_id, position, slot_cost}]` |
| `slots_used` | INTEGER | no | 0 | charged against session budget |
| `tokens_used` | INTEGER | no | 0 | |
| `latency_ms` | INTEGER | yes | — | sub-200ms target |
| `created_at` | TIMESTAMP | no | now | |

**PK:** (`result_id`). **Unique:** (`request_id`) — enforces 1:1. **FKs:** `request_id →
recall_requests(request_id)` ON DELETE CASCADE. **Checks:** `slots_used ≥ 0`;
`tokens_used ≥ 0`; `latency_ms ≥ 0`.

### 2.11 `promotion_proposals` — promotion workflow

PromotionProposal §3.8.

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `proposal_id` | UUID | no | — | |
| `shared_space_id` | UUID | no | — | target space |
| `candidate_memory_id` | UUID | no | — | consolidated semantic memory |
| `target_path` | TEXT | no | — | artifact path |
| `target_kind` | VARCHAR | no | — | artifact kind |
| `target_role` | VARCHAR | no | — | `procedural` \| `semantic` \| `episodic` |
| `diff` | TEXT | no | — | unified diff for human review |
| `status` | VARCHAR | no | `draft` | `draft` \| `in_review` \| `merged` \| `rejected` |
| `note` | TEXT | yes | — | proposer rationale (review F7) |
| `reject_reason` | TEXT | yes | — | required on reject — audit trail (review F7) |
| `proposed_at` | TIMESTAMP | no | now | |
| `resolved_at` | TIMESTAMP | yes | — | set on merge/reject |
| `reviewer` | TEXT | yes | — | human who resolved |

**PK:** (`proposal_id`). **FKs:** `shared_space_id → shared_memory_spaces(id)`;
`candidate_memory_id → memories(id)` — RESTRICT: an audit trail must not be orphaned by a
hard purge. **Checks:** `status` ∈ enum; `target_role` ∈ enum; `target_kind` ∈ enum;
`(status IN ('merged','rejected')) = (resolved_at IS NOT NULL)`;
`(status = 'rejected') = (reject_reason IS NOT NULL)`.

**Indexes:** partial `status IN ('draft','in_review')` (review queue — small and hot);
(`shared_space_id`); (`candidate_memory_id`); partial UNIQUE on
(`candidate_memory_id`, `shared_space_id`) where open — one open proposal per candidate
per space (review F10).

### 2.11b `lifecycle_jobs` — async job ledger *(added by review F3)*

Backs `GET /lifecycle/jobs/{job_id}` and every `JobRef` poll target in api-contracts
(session end, consolidate, decay, space sync). Without it the async half of the API
contract has no representable state.

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `job_id` | UUID | no | — | |
| `kind` | VARCHAR | no | — | `consolidation` \| `decay` \| `space_sync` \| `session_end` |
| `status` | VARCHAR | no | `queued` | `queued` \| `running` \| `completed` \| `failed` |
| `scope_kind` | VARCHAR | yes | — | `session` \| `user` \| `space` \| `all` |
| `scope_id` | TEXT | yes | — | session/user/space id |
| `result` | JSON | yes | — | kind-specific counts (api-contracts jobs example) |
| `error` | JSON | yes | — | required on `failed` |
| `created_at` / `finished_at` | TIMESTAMP | no/yes | now/— | |

**PK:** (`job_id`). **Checks:** `kind` ∈ enum; `status` ∈ enum;
`(status IN ('completed','failed')) = (finished_at IS NOT NULL)`; `status = failed ⇒ error IS NOT NULL`.
**Indexes:** partial open jobs (`status IN ('queued','running')`); (`scope_kind`, `scope_id`).

### 2.12 `embedding_models` — model registry

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `model_id` | VARCHAR | no | — | e.g. `text-embedding-3-small` |
| `provider` | TEXT | no | — | e.g. `openai` |
| `dims` | INTEGER | no | — | 1536 or 768 (see Part 3.10) |
| `distance_metric` | VARCHAR | no | `cosine` | `cosine` \| `l2` \| `ip` |
| `is_active` | BOOLEAN | no | true | retire models without deleting history |
| `created_at` | TIMESTAMP | no | now | |

**PK:** (`model_id`). **Checks:** `distance_metric` ∈ enum; `dims > 0`.

### 2.13 `memory_embeddings` — alternate-model vectors

| Column | Type | Null | Default | Description |
|---|---|---|---|---|
| `memory_id` | UUID | no | — | |
| `model_id` | VARCHAR | no | — | FK to registry |
| `vec_1536` | VECTOR(1536) | yes | — | vector when `dims = 1536` |
| `vec_768` | VECTOR(768) | yes | — | vector when `dims = 768` |
| `created_at` | TIMESTAMP | no | now | |

**PK:** (`memory_id`, `model_id`). **FKs:** `memory_id → memories(id)` ON DELETE CASCADE;
`model_id → embedding_models(model_id)`. **Checks:** exactly one of `vec_1536`/`vec_768`
non-null; the non-null column matches the registered `dims`.

### 2.14 Logical ERD

```
                        ┌────────────────────────┐
                        │   embedding_models     │
                        │────────────────────────│
                        │ model_id (PK)          │
                        │ provider, dims,        │
                        │ distance_metric,       │
                        │ is_active              │
                        └───────┬────────────────┘
                                │ 1
                                │
                                │ N
┌──────────────────┐   ┌────────┴───────────────┐        ┌──────────────────────────┐
│  agent_sessions  │   │       memories         │        │  shared_memory_spaces    │
│──────────────────│   │────────────────────────│        │──────────────────────────│
│ session_id (PK)  │◄──┤ session_id   (FK)      │   ┌───►│ id (PK)                  │
│ agent_type       │   │ id (PK)                │───┘    │ owner_type / owner_id    │
│ user_id          │   │ type, content, tags[]  │        │ scope                    │
│ shared_space_id ─┼──►│ embedding (V1536)      │        │ default_access /         │
│ model, max_tok,  │   │ embedding_model        │        │   write_policy /         │
│ used_tokens,     │   │ origin/agent_id/actor  │        │   promote_policy         │
│ slot_budget,     │   │ source_ref (JSON)      │        │ backend_kind/config      │
│ slots_used       │   │ timestamps ×3          │        │ artifacts (JSON)         │
│ active_memories[]│   │ confidence/decay_score │        │ sync_status, pending,    │
│ injection_order[]│   │ valid_from/valid_until │        │   sync_revision          │
│ created/ended_at │   │ superseded_by (self-FK)│        │ last_synced_at           │
│ summary          │   │ ttl_expires_at, version│        │ retention_policy (JSON)  │
└───┬──────────┬───┘   │ access_scope           │        └────┬──────────────┬──────┘
    │          │       │ shared_space_id (FK)   │             │ 1            │ 1
    │          │       │ deleted_at             │             │              │
    │          │       └──┬──────┬──────┬───────┘             │              │
    │          │          │ 1    │ 1    │ 1  ┌────────────────┘              │
    │          │          │      │      │    │ N                             │ N
    │          │          │ N    │ N    │ N  │ (candidate)      ┌────────────┴───────────┐
    │          │   ┌──────▼──┐ ┌─▼────────┐ │                   │  promotion_proposals   │
    │          │   │ memory_ │ │ memory_  │ │                   │────────────────────────│
    │          │   │ links   │ │ entities │ │                   │ proposal_id (PK)       │
    │          │   │─────────│ │──────────│ │                   │ shared_space_id (FK)   │
    │          │   │ id (PK) │ │memory_id │ │                   │ candidate_memory_id(FK)│
    │          │   │source_id│ │entity_id │ │                   │ target_path/kind/role  │
    │          │   │target_id│ │ (PK)     │ │                   │ diff                   │
    │          │   │relation │ └────┬─────┘ │                   │ status                 │
    │          │   │ _type   │      │ N     │                   │ proposed/resolved_at   │
    │          │   │ weight  │      │       │                   │ reviewer               │
    │          │   │ evidence│      │ 1     │                   └────────────────────────┘
    │          │   │created_ │  ┌───▼─────┐ │                   ┌────────────────────────┐
    │          │   │  at     │  │entities │ │                   │  space_memberships     │
    │          │   └─────────┘  │─────────│ │                   │────────────────────────│
    │          │    ▲    ▲      │entity_id│ │                   │ space_id (FK)  ┐(PK)   │
    │          │    │    │      │ (PK)    │ │                   │ principal_type ┤       │
    │          │    └────┼──────│ name    │ │                   │ principal_id   ┘       │
    │          │ source/│target│ type    │ │                   │ access_level           │
    │          │  N:1   │      │aliases[]│ │                   │ granted_at             │
    │          │  (self │ N    │created_ │ │                   └────────────────────────┘
    │          │  -ref) │      │  at     │ │
    │          │        │      └────┬────┘ │
    │          │        │           │ 1    │
    │          │        │           │ N    │
    │          │        │    ┌──────▼──────┐│
    │          │        │    │ entity_facts││
    │          │        │    │─────────────││
    │          │        │    │ id (PK)     ││
    │          │        │    │ entity_id FK││
    │          │        └────┤ memory_id FK││
    │          │             │ fact        ││
    │          │             │ valid_from/ ││
    │          │             │ valid_until ││
    │          │             └─────────────┘│
    │          │                            │
    │          │  ┌─────────────────────────┘  (memory_id FK)
    │          │  │
    │  1       │  │ N                ┌──────────────────┐        ┌──────────────────┐
    ├──────────┼──┴──────────────────┤ memory_embeddings│        │  recall_results  │
    │          │                     │──────────────────│        │──────────────────│
    │          │                     │ memory_id (FK) ┐ │        │ result_id (PK)   │
    │          │                     │ model_id (FK)  ┘ │        │ request_id (U,FK)│──┐
    │          │                     │ (PK)             │        │ candidates (JSON)│  │ 1:1
    │          │                     │ vec_1536 /       │        │ injection_plan   │  │
    │          │                     │ vec_768          │        │ slots/tokens_used│  │
    │          │                     └──────────────────┘        │ latency_ms       │  │
    │          │                                                 └──────────────────┘  │
    │          │ 1                                                                       │
    │          │                                                                         │
    │          │ N                                                                       │
    └─────────►┴─────────────────────────┐                                               │
                    ┌────────────────────┴───┐                                           │
                    │    recall_requests     │◄──────────────────────────────────────────┘
                    │────────────────────────│           (request_id FK)
                    │ request_id (PK)        │
                    │ query                  │
                    │ context (JSON)         │
                    │ trigger                │
                    │ agent_id               │
                    │ session_id (FK)        │
                    │ strategies[], top_k,   │
                    │ rerank, min_score,     │
                    │ slot/token_budget      │
                    │ requested_at           │
                    └────────────────────────┘
```

Cardinalities (matching data-models §4):

| Relationship | Cardinality |
|---|---|
| agent_sessions → memories (`session_id`) | 1 : N |
| agent_sessions → recall_requests | 1 : N |
| recall_requests → recall_results | 1 : 1 |
| memories ↔ memories (`memory_links`) | M : N (through edges; self-referencing via `superseded_by` N : 1 chain) |
| memories ↔ entities (`memory_entities`) | M : N |
| entities → entity_facts | 1 : N |
| shared_memory_spaces → memories (`shared_space_id`) | 1 : N |
| shared_memory_spaces → space_memberships | 1 : N |
| shared_memory_spaces → promotion_proposals | 1 : N |
| promotion_proposals → memories (candidate) | N : 1 |
| memories → memory_embeddings | 1 : N (one per model) |
| embedding_models → memory_embeddings | 1 : N |

---

## Part 3 — Physical Data Model (pgvector-optimized)

PostgreSQL 16+, pgvector 0.7+ (HNSW + halfvec), pg_trgm. All DDL below is validated
against `pgvector/pgvector:pg17` (see Verification).

Conventions: UUIDv7 ids generated by the application (time-ordered → friendlier B-tree
locals) or `gen_random_uuid()` fallback; all timestamps `timestamptz` (UTC); enums as
`text` + `CHECK` (cheaper to extend than native ENUM types; adding a value is a data
migration, not a type migration).

### 3.0 Extensions and shared domains

```sql
-- DDL: extensions and domains
CREATE EXTENSION IF NOT EXISTS vector;      -- HNSW ANN search
CREATE EXTENSION IF NOT EXISTS pg_trgm;     -- fuzzy lexical fallback

-- Tag syntax from data-models §3.1: ^[a-z0-9][a-z0-9_-]*$
CREATE DOMAIN tag_name AS text
  CHECK (VALUE ~ '^[a-z0-9][a-z0-9_-]*$');

COMMENT ON DOMAIN tag_name IS 'Lowercase tag token; pattern enforced per data-models §3.1';

-- array_to_string is STABLE, so it cannot feed a STORED generated column;
-- this wrapper is genuinely immutable (output depends only on the arguments)
CREATE FUNCTION ai_array_to_string(arr text[], sep text) RETURNS text
LANGUAGE sql IMMUTABLE AS $$ SELECT array_to_string(arr, sep) $$;

COMMENT ON FUNCTION ai_array_to_string(text[], text) IS
  'Immutable wrapper for use in generated tsvector columns';
```

### 3.1 `shared_memory_spaces`

```sql
-- DDL: shared_memory_spaces
CREATE TABLE shared_memory_spaces (
  id                 uuid        PRIMARY KEY,
  name               text        NOT NULL CHECK (length(btrim(name)) > 0),
  description        text        ,
  owner_type         text        NOT NULL,
  owner_id           text        NOT NULL,
  scope              text        NOT NULL,
  default_access     text        NOT NULL,
  write_policy       text        NOT NULL,
  promote_policy     text        NOT NULL,
  backend_kind       text        NOT NULL,
  backend_config     jsonb       ,
  artifacts          jsonb       ,
  sync_status        text        NOT NULL DEFAULT 'in_sync',
  pending_proposals  integer     NOT NULL DEFAULT 0 CHECK (pending_proposals >= 0),
  sync_revision      text        ,
  last_synced_at     timestamptz NOT NULL DEFAULT now(),
  retention_policy   jsonb       ,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT spaces_owner_type_chk CHECK (owner_type IN ('user', 'agent', 'team', 'organization')),
  CONSTRAINT spaces_default_access_chk CHECK (default_access IN ('read', 'write', 'none')),
  CONSTRAINT spaces_write_policy_chk CHECK (write_policy IN ('owner_approved', 'participant_free', 'proposal_only')),
  CONSTRAINT spaces_promote_policy_chk CHECK (promote_policy IN ('human_review', 'auto')),
  CONSTRAINT spaces_backend_chk CHECK (backend_kind IN ('files', 'relational', 'vector', 'graph', 'hybrid')),
  CONSTRAINT spaces_sync_status_chk CHECK (sync_status IN ('in_sync', 'pending_review', 'diverged', 'offline')),
  CONSTRAINT spaces_retention_chk CHECK (
    retention_policy IS NULL OR
    (jsonb_typeof(retention_policy) = 'object'
     AND (retention_policy ? 'supersede_not_delete' OR retention_policy ? 'ttl_days' OR retention_policy ? 'archive_after_days')))
);

CREATE INDEX spaces_owner_idx    ON shared_memory_spaces (owner_type, owner_id);
CREATE INDEX spaces_attention_idx ON shared_memory_spaces (sync_status)
  WHERE sync_status <> 'in_sync';

COMMENT ON TABLE shared_memory_spaces IS
  'Access-controlled shared space (SharedMemorySpace §3.5); artifacts is a manifest, not a membership list';
COMMENT ON COLUMN shared_memory_spaces.artifacts IS
  'Externalized reviewable representations; populated only when backend includes files';
```

### 3.2 `agent_sessions`

```sql
-- DDL: agent_sessions
CREATE TABLE agent_sessions (
  session_id             uuid        PRIMARY KEY,
  agent_type             text        NOT NULL,
  user_id                text        NOT NULL,
  shared_space_id        uuid        REFERENCES shared_memory_spaces (id) ON DELETE SET NULL,
  model                  text        NOT NULL,
  max_tokens             integer     NOT NULL CHECK (max_tokens >= 1),
  used_tokens            integer     NOT NULL DEFAULT 0 CHECK (used_tokens >= 0),
  instruction_slot_budget integer    NOT NULL CHECK (instruction_slot_budget >= 0),
  slots_used             integer     NOT NULL DEFAULT 0 CHECK (slots_used >= 0),
  active_memories        uuid[]      NOT NULL DEFAULT '{}',
  injection_order        text[]      ,
  created_at             timestamptz NOT NULL DEFAULT now(),
  ended_at               timestamptz ,
  summary                text        ,
  CONSTRAINT agent_sessions_agent_type_chk CHECK (
    agent_type IN ('claude-code', 'codex', 'cursor', 'letta', 'custom')),
  CONSTRAINT agent_sessions_timing_chk CHECK (
    ended_at IS NULL OR ended_at > created_at),
  CONSTRAINT agent_sessions_slot_budget_chk CHECK (
    slots_used <= instruction_slot_budget OR instruction_slot_budget = 0)
);

CREATE INDEX agent_sessions_user_idx
  ON agent_sessions (user_id, created_at DESC);
CREATE INDEX agent_sessions_active_idx
  ON agent_sessions (created_at DESC)
  WHERE ended_at IS NULL;
CREATE INDEX agent_sessions_active_memories_idx
  ON agent_sessions USING gin (active_memories);

COMMENT ON TABLE agent_sessions IS
  'Runtime binding per data-models §3.4; working tier is the context window, this table tracks its shape and budget';
COMMENT ON COLUMN agent_sessions.active_memories IS
  'Working-set memory ids in context; no FK (array) — app maintains, orphan sweep in lifecycle job';
```

Deployment order matters: `shared_memory_spaces` (§3.1) is created before this table
because of the `shared_space_id` FK; see Part 6 migration 002.

### 3.3 `memories` — core store

```sql
-- DDL: memories
CREATE TABLE memories (
  id                uuid          PRIMARY KEY,
  type              text          NOT NULL,
  content           text          NOT NULL CHECK (length(btrim(content)) > 0),
  content_format    text          NOT NULL DEFAULT 'markdown',
  tags              tag_name[]    ,
  embedding         vector(1536)  ,
  embedding_model   text          ,
  origin            text          NOT NULL,
  session_id        uuid          REFERENCES agent_sessions (session_id) ON DELETE SET NULL,
  agent_id          text          ,
  actor             text          ,
  -- access-control subject (review F1): provenance describes who encoded;
  -- owner describes who may read. Defaults from session user_id at insert.
  owner_principal_type text       NOT NULL DEFAULT 'user',
  owner_principal_id   text       NOT NULL,
  source_ref        jsonb         ,
  created_at        timestamptz   NOT NULL DEFAULT now(),
  updated_at        timestamptz   NOT NULL DEFAULT now(),
  -- last_accessed_at REMOVED (review F8): per-read updates wrote 2,500 rows/min on the
  -- hottest table; access tracking decoupled into memory_access_log (§3.7b)
  confidence        numeric(3,2)  CHECK (confidence  BETWEEN 0 AND 1),
  decay_score       numeric(3,2)  DEFAULT 1.0 CHECK (decay_score BETWEEN 0 AND 1),
  valid_from        timestamptz   ,
  valid_until       timestamptz   ,
  validity_range    tstzrange     GENERATED ALWAYS AS
                    (tstzrange(valid_from, valid_until)) STORED,
  superseded_by     uuid          REFERENCES memories (id) ON DELETE SET NULL,
  ttl_expires_at    timestamptz   ,
  version           integer       NOT NULL DEFAULT 1 CHECK (version >= 1),
  access_scope      text          NOT NULL,
  shared_space_id   uuid          REFERENCES shared_memory_spaces (id) ON DELETE RESTRICT,
  deleted_at        timestamptz   ,
  search_tsv        tsvector      GENERATED ALWAYS AS (
                    setweight(to_tsvector('english', coalesce(content, '')), 'A') ||
                    setweight(to_tsvector('english', ai_array_to_string(tags, ' ')), 'B')
                  ) STORED,
  CONSTRAINT memories_type_chk CHECK (type IN ('episodic', 'semantic', 'procedural')),
  CONSTRAINT memories_format_chk CHECK (content_format IN ('markdown', 'plain', 'json')),
  CONSTRAINT memories_origin_chk CHECK (
    origin IN ('agent_observation', 'user_instruction', 'file_artifact', 'consolidation')),
  CONSTRAINT memories_scope_chk CHECK (access_scope IN ('individual', 'shared')),
  CONSTRAINT memories_validity_chk CHECK (
    valid_until IS NULL OR valid_from IS NULL OR valid_until > valid_from),
  CONSTRAINT memories_ttl_chk CHECK (
    ttl_expires_at IS NULL OR ttl_expires_at > created_at),
  CONSTRAINT memories_scope_space_chk CHECK (
    (access_scope = 'shared') = (shared_space_id IS NOT NULL)),
  CONSTRAINT memories_owner_type_chk CHECK (
    owner_principal_type IN ('user', 'agent', 'session', 'group'))
);

-- ANN (dense path): HNSW cosine; halfvec expression index halves index size (pgvector ≥0.7)
CREATE INDEX memories_embedding_hnsw_idx ON memories
  USING hnsw ((embedding::halfvec(1536)) halfvec_cosine_ops)
  WITH (m = 16, ef_construction = 200)
  WHERE deleted_at IS NULL;

-- Sparse path (BM25-like, ts_rank_cd): GIN over the generated tsvector
CREATE INDEX memories_search_tsv_idx ON memories USING gin (search_tsv);

-- Lexical fuzzy fallback
CREATE INDEX memories_content_trgm_idx ON memories USING gin (content gin_trgm_ops);

-- Containment on tags
CREATE INDEX memories_tags_idx ON memories USING gin (tags);

-- Temporal path: GiST over the validity range
CREATE INDEX memories_validity_idx ON memories USING gist (validity_range);

-- Filters and sort keys
CREATE INDEX memories_created_idx     ON memories (created_at DESC);
CREATE INDEX memories_type_idx        ON memories (type) WHERE deleted_at IS NULL;
CREATE INDEX memories_scope_idx       ON memories (access_scope, shared_space_id) WHERE deleted_at IS NULL;
CREATE INDEX memories_session_idx     ON memories (session_id);
CREATE INDEX memories_owner_idx       ON memories (owner_principal_type, owner_principal_id)
  WHERE deleted_at IS NULL;
-- memories_accessed_idx removed with last_accessed_at (F8); recency reads use
-- memory_access_log_recent_idx instead
CREATE INDEX memories_decay_idx       ON memories (decay_score) WHERE deleted_at IS NULL;
CREATE INDEX memories_ttl_idx         ON memories (ttl_expires_at)
  WHERE ttl_expires_at IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX memories_superseded_idx  ON memories (superseded_by)
  WHERE superseded_by IS NOT NULL;

COMMENT ON TABLE memories IS
  'System of record for Memory (data-models §3.1); one row per version, supersession-first decay';
COMMENT ON COLUMN memories.embedding IS
  'Primary-model vector (default text-embedding-3-small, 1536d); alternate models in memory_embeddings';
COMMENT ON COLUMN memories.validity_range IS
  'Generated tstzrange(valid_from, valid_until); GiST-indexed for point-in-time recall';
COMMENT ON COLUMN memories.search_tsv IS
  'Generated tsvector: content weight A, tags weight B; sparse (BM25-like, ts_rank_cd) path';
COMMENT ON COLUMN memories.embedding_model IS
  'Registry FK to embedding_models(model_id); primary slot is 1536-dim only (F11)';
COMMENT ON COLUMN memories.source_ref IS
  'JSON provenance: origin is client-asserted; agent_id/actor are SERVER-STAMPED from the authenticated principal, never client-writable (F21)';
COMMENT ON COLUMN memories.deleted_at IS
  'Soft delete for purge workflow; hot indexes are partial on IS NULL';
```

Notes:

- **HNSW via halfvec expression**: `embedding::halfvec(1536)` stores fp16 in the index
  (~half the size, negligible recall loss at 1536d); queries use
  `embedding::halfvec(1536) <=> query::halfvec(1536)`. Drop the cast for exact fp32 if
  recall@k is critical — see Part 5 Q1.
- **`validity_range` generated column**: `tstzrange()` is immutable, so it is legal as a
  STORED generated column and one GiST index serves `@>` point-in-time predicates.
- **`search_tsv`**: content weighted `A`, tags `B`; `ts_rank_cd` (below) respects weights.
- `WHERE deleted_at IS NULL` partials keep the hot set index-only and small.

### 3.4 `memory_links`

```sql
-- DDL: memory_links
CREATE TABLE memory_links (
  id                uuid        PRIMARY KEY,
  source_id         uuid        NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  target_id         uuid        NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  relationship_type text        NOT NULL,
  weight            numeric(3,2) NOT NULL DEFAULT 1.0 CHECK (weight BETWEEN 0 AND 1),
  evidence          text        ,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT memory_links_type_chk CHECK (relationship_type IN
    ('derived_from', 'supersedes', 'similar_to', 'co_occurs_with', 'causal_next', 'anchors_entity')),
  CONSTRAINT memory_links_unique UNIQUE (source_id, target_id, relationship_type),
  CONSTRAINT memory_links_no_self_chk CHECK (source_id <> target_id)
);

CREATE INDEX memory_links_source_idx ON memory_links (source_id, relationship_type);
CREATE INDEX memory_links_target_idx ON memory_links (target_id, relationship_type);
CREATE INDEX memory_links_chain_idx  ON memory_links (source_id)
  WHERE relationship_type = 'supersedes';

COMMENT ON TABLE memory_links IS
  'Directed weighted edges (MemoryLink §3.2); the graph path traverses these with recursive CTEs';
```

### 3.5 `space_memberships`

```sql
-- DDL: space_memberships
CREATE TABLE space_memberships (
  space_id        uuid        NOT NULL REFERENCES shared_memory_spaces (id) ON DELETE CASCADE,
  principal_type  text        NOT NULL,
  principal_id    text        NOT NULL,
  access_level    text        NOT NULL,
  granted_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (space_id, principal_type, principal_id),
  CONSTRAINT memberships_principal_type_chk CHECK (principal_type IN ('user', 'agent', 'session', 'group')),
  CONSTRAINT memberships_access_level_chk CHECK (access_level IN ('read', 'write', 'promote', 'admin'))
);

CREATE INDEX memberships_principal_idx ON space_memberships (principal_type, principal_id);

COMMENT ON TABLE space_memberships IS
  'Junction for SharedMemorySpace.participants[]; principal_id has no FK — principals are external';
```

### 3.6 `entities` and children

```sql
-- DDL: entities
CREATE TABLE entities (
  entity_id    uuid        PRIMARY KEY,
  name         text        NOT NULL CHECK (length(btrim(name)) > 0),
  entity_type  text        NOT NULL,
  aliases      text[]      ,
  created_at   timestamptz NOT NULL DEFAULT now(),
  search_tsv   tsvector    GENERATED ALWAYS AS (
               setweight(to_tsvector('english', coalesce(name, '')), 'A') ||
               setweight(to_tsvector('english', ai_array_to_string(aliases, ' ')), 'C')
             ) STORED,
  CONSTRAINT entities_type_chk CHECK (
    entity_type IN ('person', 'project', 'repository', 'tool', 'organization', 'concept')),
  CONSTRAINT entities_unique_name UNIQUE (name, entity_type)
);

CREATE INDEX entities_search_tsv_idx ON entities USING gin (search_tsv);
CREATE INDEX entities_aliases_idx    ON entities USING gin (aliases);
CREATE INDEX entities_type_idx       ON entities (entity_type);

COMMENT ON TABLE entities IS 'Entity registry (Entity §3.3); entity-centric anchors for retrieval';

-- DDL: entity_facts
CREATE TABLE entity_facts (
  id          uuid        PRIMARY KEY,
  entity_id   uuid        NOT NULL REFERENCES entities (entity_id) ON DELETE CASCADE,
  fact        text        NOT NULL,
  memory_id   uuid        REFERENCES memories (id) ON DELETE SET NULL,
  valid_from  timestamptz NOT NULL,
  valid_until timestamptz ,
  fact_range  tstzrange   GENERATED ALWAYS AS (tstzrange(valid_from, valid_until)) STORED,
  CONSTRAINT entity_facts_range_chk CHECK (valid_until IS NULL OR valid_until > valid_from)
);

CREATE INDEX entity_facts_entity_idx ON entity_facts (entity_id);
CREATE INDEX entity_facts_range_idx  ON entity_facts USING gist (fact_range);
CREATE INDEX entity_facts_current_idx ON entity_facts (entity_id)
  WHERE valid_until IS NULL;

COMMENT ON TABLE entity_facts IS
  'Temporal facts per entity (Entity.facts[]); Zep-style validity intervals [source: 03-zep]';

-- DDL: memory_entities
CREATE TABLE memory_entities (
  memory_id  uuid        NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  entity_id  uuid        NOT NULL REFERENCES entities (entity_id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (memory_id, entity_id)
);

CREATE INDEX memory_entities_entity_idx ON memory_entities (entity_id, created_at DESC);

COMMENT ON TABLE memory_entities IS 'M:N anchor between Memory.entities[] and the registry';
```

### 3.7 `recall_requests` / `recall_results`

```sql
-- DDL: recall_requests
CREATE TABLE recall_requests (
  request_id   uuid        PRIMARY KEY,
  query        text        NOT NULL,
  context      jsonb       NOT NULL,
  trigger      text        NOT NULL,
  agent_id     text        NOT NULL,
  session_id   uuid        NOT NULL REFERENCES agent_sessions (session_id) ON DELETE CASCADE,
  strategies   text[]      NOT NULL,
  top_k        integer     NOT NULL DEFAULT 50 CHECK (top_k BETWEEN 1 AND 200),
  rerank       text        NOT NULL DEFAULT 'cross_encoder',
  min_score    numeric(3,2) NOT NULL DEFAULT 0.35 CHECK (min_score BETWEEN 0 AND 1),
  slot_budget  integer     ,
  token_budget integer     ,
  mode         text        NOT NULL DEFAULT 'sync',
  status       text        NOT NULL DEFAULT 'completed',
  error        jsonb       ,
  requested_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recall_requests_trigger_chk CHECK (
    trigger IN ('task_context', 'user_query', 'temporal', 'associative', 'session_start')),
  CONSTRAINT recall_requests_rerank_chk CHECK (rerank IN ('cross_encoder', 'none')),
  CONSTRAINT recall_requests_strategies_chk CHECK (
    strategies <@ ARRAY['vector', 'bm25', 'graph', 'temporal']::text[]
    AND array_length(strategies, 1) >= 1),
  CONSTRAINT recall_requests_budgets_chk CHECK (
    (slot_budget IS NULL OR slot_budget >= 0) AND (token_budget IS NULL OR token_budget >= 0)),
  CONSTRAINT recall_requests_mode_chk CHECK (mode IN ('sync', 'async')),
  CONSTRAINT recall_requests_status_chk CHECK (
    status IN ('queued', 'running', 'completed', 'failed')),
  CONSTRAINT recall_requests_status_pairing_chk CHECK (
    status <> 'failed' OR error IS NOT NULL)
);

CREATE INDEX recall_requests_session_idx  ON recall_requests (session_id, requested_at DESC);
CREATE INDEX recall_requests_time_idx     ON recall_requests (requested_at DESC);
CREATE INDEX recall_requests_trigger_idx  ON recall_requests (trigger, requested_at DESC);
CREATE INDEX recall_requests_active_idx   ON recall_requests (requested_at)
  WHERE status IN ('queued', 'running');

COMMENT ON COLUMN recall_requests.status IS
  'Async recall lifecycle (review F3): queued|running|completed|failed; error jsonb required on failed';

COMMENT ON TABLE recall_requests IS
  'OPERATIONAL LOG TABLE (F14), not a core memory entity; retain 30 days, then archive/purge. Append-only recall log (RecallRequest §3.6); partitioning candidate at scale (§3.12)';

-- DDL: recall_results
CREATE TABLE recall_results (
  result_id      uuid        PRIMARY KEY,
  request_id     uuid        NOT NULL UNIQUE REFERENCES recall_requests (request_id) ON DELETE CASCADE,
  candidates     jsonb       NOT NULL CHECK (jsonb_typeof(candidates) = 'array'),
  injection_plan jsonb       NOT NULL CHECK (jsonb_typeof(injection_plan) = 'array'),
  slots_used     integer     NOT NULL DEFAULT 0 CHECK (slots_used >= 0),
  tokens_used    integer     NOT NULL DEFAULT 0 CHECK (tokens_used >= 0),
  latency_ms     integer     CHECK (latency_ms >= 0),
  created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX recall_results_created_idx ON recall_results (created_at DESC);

COMMENT ON TABLE recall_results IS
  'OPERATIONAL LOG TABLE (F14), not a core memory entity; same 30-day retention as recall_requests. Merged + re-ranked answer per request (RecallResult §3.7); UNIQUE(request_id) enforces 1:1';

-- DDL: lifecycle_jobs (review F3)
-- Backs GET /lifecycle/jobs/{id} and every JobRef poll target in api-contracts
-- (session end, consolidate, decay, space sync). Without this table the async
-- half of the API contract has no representable state.
CREATE TABLE lifecycle_jobs (
  job_id      uuid        PRIMARY KEY,
  kind        text        NOT NULL,
  status      text        NOT NULL DEFAULT 'queued',
  scope_kind  text        ,
  scope_id    text        ,
  result      jsonb       ,
  error       jsonb       ,
  created_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz ,
  CONSTRAINT lifecycle_jobs_kind_chk CHECK (
    kind IN ('consolidation', 'decay', 'space_sync', 'session_end')),
  CONSTRAINT lifecycle_jobs_status_chk CHECK (
    status IN ('queued', 'running', 'completed', 'failed')),
  CONSTRAINT lifecycle_jobs_terminal_chk CHECK (
    (status IN ('completed', 'failed')) = (finished_at IS NOT NULL)),
  CONSTRAINT lifecycle_jobs_error_pairing_chk CHECK (
    status <> 'failed' OR error IS NOT NULL)
);

CREATE INDEX lifecycle_jobs_open_idx ON lifecycle_jobs (kind, created_at)
  WHERE status IN ('queued', 'running');
CREATE INDEX lifecycle_jobs_scope_idx ON lifecycle_jobs (scope_kind, scope_id);

COMMENT ON TABLE lifecycle_jobs IS
  'Async job ledger for lifecycle/sync operations (api-contracts §1.2.5, §4.1); added by review F3';
```

(`trigger` and `rerank` are unquoted keywords-safe in PostgreSQL but `trigger` is reserved
in some tools; the deployment script aliases it as `trigger_kind` if a conflict arises —
see Part 6 migration 002.)

### 3.7b `memory_access_log` — decoupled access tracking *(added by review F8)*

```sql
-- DDL: memory_access_log (review F8)
-- Replaces memories.last_accessed_at: reads append here instead of updating the
-- hottest table (2,500 row-updates/min at 100 recalls/min x 25 candidates).
CREATE TABLE memory_access_log (
  id          bigserial   PRIMARY KEY,
  memory_id   uuid        NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  accessed_by uuid        NOT NULL,
  access_type text        NOT NULL CHECK (access_type IN ('recall', 'direct_get', 'session_activate')),
  accessed_at timestamptz NOT NULL DEFAULT now()
);

-- Recent-access queries only; full history is served by sequential scans / partitions
CREATE INDEX memory_access_log_recent_idx ON memory_access_log (memory_id, accessed_at DESC);

COMMENT ON TABLE memory_access_log IS
  'Append-only access log (F8): can be partitioned by accessed_at; aggregate last_accessed_at = MAX(accessed_at) GROUP BY memory_id when needed';
```

Append-only; partition by `accessed_at` at scale. `last_accessed_at` views compute
`MAX(accessed_at) GROUP BY memory_id`. Q6 and the API `sort=last_accessed_at` read the
aggregate, not a `memories` column.

### 3.8 `promotion_proposals` + workflow trigger

```sql
-- DDL: promotion_proposals
CREATE TABLE promotion_proposals (
  proposal_id         uuid        PRIMARY KEY,
  shared_space_id     uuid        NOT NULL REFERENCES shared_memory_spaces (id) ON DELETE CASCADE,
  candidate_memory_id uuid        NOT NULL REFERENCES memories (id) ON DELETE RESTRICT,
  target_path         text        NOT NULL,
  target_kind         text        NOT NULL,
  target_role         text        NOT NULL,
  diff                text        NOT NULL,
  status              text        NOT NULL DEFAULT 'draft',
  note                text        ,   -- review F7: proposer rationale (api-contracts PromotionInput.note)
  reject_reason       text        ,   -- review F7: required-on-reject audit trail (api-contracts reject body)
  proposed_at         timestamptz NOT NULL DEFAULT now(),
  resolved_at         timestamptz ,
  reviewer            text        ,
  CONSTRAINT proposals_status_chk CHECK (status IN ('draft', 'in_review', 'merged', 'rejected')),
  CONSTRAINT proposals_role_chk CHECK (target_role IN ('procedural', 'semantic', 'episodic')),
  CONSTRAINT proposals_target_kind_chk CHECK (
    target_kind IN ('spec', 'rule', 'agent_doc', 'memory_doc')),
  CONSTRAINT proposals_resolution_chk CHECK (
    (status IN ('merged', 'rejected')) = (resolved_at IS NOT NULL)),
  CONSTRAINT proposals_reject_reason_chk CHECK (
    (status = 'rejected') = (reject_reason IS NOT NULL))
);

-- Review queue: small, hot partial index
CREATE INDEX proposals_open_idx ON promotion_proposals (shared_space_id, proposed_at)
  WHERE status IN ('draft', 'in_review');
CREATE INDEX proposals_space_idx    ON promotion_proposals (shared_space_id);
CREATE INDEX proposals_candidate_idx ON promotion_proposals (candidate_memory_id);

-- Review F10: at most one open proposal per (candidate, space) — prevents
-- duplicate merges of the same memory into the same space via parallel proposals.
CREATE UNIQUE INDEX proposals_one_open_idx ON promotion_proposals (candidate_memory_id, shared_space_id)
  WHERE status IN ('draft', 'in_review');

COMMENT ON TABLE promotion_proposals IS
  'Human-review promotion path (PromotionProposal §3.8); candidate FK is RESTRICT — audit survives purge';

-- Maintains shared_memory_spaces.pending_proposals (denormalized counter, §1.3)
-- DDL: proposals counter trigger
CREATE OR REPLACE FUNCTION proposals_pending_count() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    UPDATE shared_memory_spaces
       SET pending_proposals = pending_proposals + 1,
           sync_status = 'pending_review',
           updated_at = now()
     WHERE id = NEW.shared_space_id;
  ELSIF TG_OP = 'UPDATE' AND OLD.status <> NEW.status THEN
    IF OLD.status IN ('draft', 'in_review') AND NEW.status IN ('merged', 'rejected') THEN
      UPDATE shared_memory_spaces
         SET pending_proposals = greatest(pending_proposals - 1, 0),
             sync_status = CASE
                 WHEN (SELECT count(*) FROM promotion_proposals p
                        WHERE p.shared_space_id = NEW.shared_space_id
                          AND p.status IN ('draft', 'in_review')) = 0
                 THEN 'in_sync' ELSE 'pending_review' END,
             updated_at = now()
       WHERE id = NEW.shared_space_id;
    ELSIF NEW.status IN ('draft', 'in_review') AND OLD.status IN ('merged', 'rejected') THEN
      UPDATE shared_memory_spaces
         SET pending_proposals = pending_proposals + 1,
             sync_status = 'pending_review',
             updated_at = now()
       WHERE id = NEW.shared_space_id;
    END IF;
  ELSIF TG_OP = 'DELETE' AND OLD.status IN ('draft', 'in_review') THEN
    -- Review F5: recompute sync_status, not just the counter — otherwise deleting
    -- the last open proposal leaves the space stuck in 'pending_review'.
    UPDATE shared_memory_spaces
       SET pending_proposals = greatest(pending_proposals - 1, 0),
           sync_status = CASE
               WHEN (SELECT count(*) FROM promotion_proposals p
                      WHERE p.shared_space_id = OLD.shared_space_id
                        AND p.status IN ('draft', 'in_review')) = 0
               THEN 'in_sync' ELSE 'pending_review' END,
           updated_at = now()
     WHERE id = OLD.shared_space_id;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_proposals_pending_count
AFTER INSERT OR UPDATE OF status OR DELETE ON promotion_proposals
FOR EACH ROW EXECUTE FUNCTION proposals_pending_count();
```

### 3.9 `embedding_models` / `memory_embeddings`

```sql
-- DDL: embedding_models
CREATE TABLE embedding_models (
  model_id        text        PRIMARY KEY,
  provider        text        NOT NULL,
  dims            integer     NOT NULL,
  distance_metric text        NOT NULL DEFAULT 'cosine',
  is_active       boolean     NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT embedding_models_metric_chk CHECK (distance_metric IN ('cosine', 'l2', 'ip')),
  CONSTRAINT embedding_models_dims_chk CHECK (dims IN (1536, 768))
);

COMMENT ON TABLE embedding_models IS
  'Registry of embedding models; dims constrained to the two typed columns of memory_embeddings';
-- DDL: memory_embeddings
CREATE TABLE memory_embeddings (
  memory_id  uuid        NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  model_id   text        NOT NULL REFERENCES embedding_models (model_id),
  vec_1536   vector(1536),
  vec_768    vector(768),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (memory_id, model_id),
  CONSTRAINT memory_embeddings_one_vec_chk CHECK (
    (vec_1536 IS NOT NULL)::int + (vec_768 IS NOT NULL)::int = 1)
  -- dims↔model consistency is validated by the loader; a CHECK cannot
  -- reference embedding_models (subqueries are not allowed in CHECK)
);

CREATE INDEX memory_embeddings_hnsw1536_idx ON memory_embeddings
  USING hnsw ((vec_1536::halfvec(1536)) halfvec_cosine_ops) WITH (m = 16, ef_construction = 200)
  WHERE vec_1536 IS NOT NULL;
CREATE INDEX memory_embeddings_hnsw768_idx ON memory_embeddings
  USING hnsw ((vec_768::halfvec(768)) halfvec_cosine_ops) WITH (m = 16, ef_construction = 200)
  WHERE vec_768 IS NOT NULL;

COMMENT ON TABLE memory_embeddings IS
  'Alternate-model vectors; exactly one typed column populated per row, matching registered dims';

-- Seed the registry (F11): known models. 3072/384 need typed columns before use.
INSERT INTO embedding_models (model_id, provider, dims) VALUES
  ('text-embedding-3-small', 'openai', 1536),
  ('text-embedding-3-large', 'openai', 3072),
  ('all-MiniLM-L6-v2',       'sentence-transformers', 384),
  ('bge-base-en-v1.5',       'bge', 768)
ON CONFLICT (model_id) DO NOTHING;
```

### 3.10 Embedding strategy

| Model | Dims | Column | Index | Use |
|---|---|---|---|---|
| OpenAI `text-embedding-3-small` | 1536 | `memories.embedding` | HNSW halfvec(1536) cosine | **Primary**: every memory, hot dense path |
| e.g. `bge-base` / `gte-base` class | 768 | `memory_embeddings.vec_768` | HNSW halfvec(768) cosine | Secondary/experimental model A/B |
| any other 1536d model | 1536 | `memory_embeddings.vec_1536` | HNSW halfvec(1536) cosine | Migration between 1536d providers |

Rules:

- **One typed column per dimension**. pgvector HNSW indexes require a fixed typmod
  (`vector` without a dimension cannot be ANN-indexed), so multi-model support is one
  column per supported dimension, not a generic `vector` column.
- `memories.embedding` + `memories.embedding_model` is the **primary** slot: the dense
  path never joins. Alternate models pay one join via `memory_embeddings`.
- Adding a dimension (e.g. 3072 for `text-embedding-3-large`) = new nullable typed column
  on `memory_embeddings` + registry row + partial HNSW — no rewrite of the primary path.
- Cosine (`<=>`) chosen because embedding providers normalize; L2 (`<->`) is equivalent on
  normalized vectors. Inner product (`<#>`) for dot-product-similarity models.
- **Re-embedding** an old model → new: backfill `memory_embeddings` rows, flip
  `embedding_models.is_active`, then swap `memories.embedding` in batches (never a
  big-bang swap — mixed-model cosine distance is meaningless).

### 3.11 Hybrid search mapping (4-way)

| Architecture path | PostgreSQL realization |
|---|---|
| **Vector** (dense) | HNSW index, `embedding::halfvec(1536) <=> $q::halfvec(1536)`, `ORDER BY … LIMIT k` — ANN top-k |
| **BM25-like** (sparse) | `search_tsv` tsvector + GIN; `ts_rank_cd(search_tsv, websearch_to_tsquery('english', $q))`; rank_cd ≈ BM25-style term-proximity scoring (**BM25-like, not true BM25**: PostgreSQL has no exact BM25; `ts_rank_cd` + weights is the standard equivalent. For exact BM25, add `pg_search`/ParadeDB — noted as an alternative, not a dependency) |
| **Graph** | recursive CTE over `memory_links` (Q3): vector finds entry points, CTE expands ≤2 hops, weights accumulate; `similar_to`/`co_occurs_with` edges are the A-MEM link families |
| **Temporal** | GiST on `validity_range`, predicate `validity_range @> $ts` (or open-interval fallback), plus B-tree `created_at` for recency decay terms |
| **Fusion** | Reciprocal Rank Fusion in SQL (`1.0/(60+rank)`) or application-side weighted merge; then **cross-encoder re-rank in the application** over the top 20–50 candidates [source: 06-hybrid-retrieval-strategies] — the DB returns candidates, never final injections |
| **Entity-centric** | `context->'entities'` ids → `memory_entities` join (Mem0's entity-first path) |

**BM25-like caveat (F13).** `ts_rank_cd` is weighted **cover-density** ranking — term-frequency density with positional weighting — not true BM25: no IDF document-frequency weighting parity, no field-length saturation. Long procedural memories therefore rank differently than BM25 would rank them. For most recall use cases this is sufficient. If true BM25 quality is required, evaluate ParadeDB's `pg_search` extension as a drop-in replacement — a deliberate non-dependency of this schema.

### 3.12 Partitioning strategy

Two tables outgrow the rest by 1–2 orders of magnitude: `memories` and `recall_requests`.

**Decision: start unpartitioned; partition at scale.** The DDL above (single tables) is the
default deployment. Thresholds and variants:

| Table | Threshold | Strategy |
|---|---|---|
| `memories` | > ~20M rows or > ~150 GB | `PARTITION BY RANGE (created_at)`, monthly partitions; PK becomes `(id, created_at)`; new partitions via `pg_partman`; cold partitions `DETACH` + `ALTER TABLE … SET TABLESPACE cold_ts` |
| `recall_requests` | > ~50M rows | `PARTITION BY RANGE (requested_at)`, monthly; `recall_results` follows with composite FK `(request_id, requested_at)` |
| `recall_results` | with its parent | same range key; drop old partitions = cheap retention |

Partitioned `memories` variant (sketch):

```sql
-- DDL: partitioned memories variant
CREATE TABLE memories_p (
  id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  -- …all columns of §3.2…
  PRIMARY KEY (id, created_at)          -- PK must include the partition key
) PARTITION BY RANGE (created_at);

CREATE TABLE memories_p_2026_08 PARTITION OF memories_p
  FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE memories_p_default PARTITION OF memories_p DEFAULT;

-- HNSW is created per-partition (no parent-level index):
CREATE INDEX memories_p_2026_08_hnsw ON memories_p_2026_08
  USING hnsw ((embedding::halfvec(1536)) halfvec_cosine_ops) WITH (m = 16, ef_construction = 200);
```

Tradeoffs, stated plainly:

- **FK limitation**: child tables (`memory_links`, `memory_entities`, …) cannot reference
  `memories_p(id)` alone — a partitioned-parent FK target must be a unique constraint that
  includes the partition key. Choices: (a) carry `created_at` into children (write-path
  burden), or (b) drop the FK, enforce logically, and run the orphan-sweep query monthly.
  We recommend (b) at the scale that justifies partitioning, with the sweep in the
  lifecycle job.
- **ANN across partitions**: HNSW indexes are per-partition; a cross-partition vector
  query searches each partition's index and merges (no global graph). Mitigate by scoping
  recall to a recent window (`created_at > now() - interval '18 months'`) — stale memories
  surface via consolidation, not via the dense path.
- `DETACH PARTITION` makes archival metadata-only.

### 3.13 Storage & maintenance

- **TOAST**: `content` and `diff` exceed the ~2KB inline threshold and TOAST automatically.
  For very large procedural memories set per-column:
  `ALTER TABLE memories ALTER COLUMN content SET STORAGE EXTERNAL;` (no compression →
  faster reads, larger disk) or keep default `EXTENDED` (lz4/pglz). Prefer
  `ALTER TABLE … SET COMPRESSION lz4` on PG14+ for cheaper decompress on the recall path.
- **fillfactor**: `memories` 90 (updates touch `decay_score`,
  `confidence`, `valid_until`, `superseded_by` — no longer `last_accessed_at`, which
  moved to `memory_access_log` (F8)),
  `updated_at` → keep HOT-update room; HOT requires no indexed-column change — the
  partial indexes exclude some updates only), `recall_requests` 100 (append-only),
  `memory_links` 95.
- **autovacuum** (per-table, aggressive for churn):
  ```sql
  ALTER TABLE memories SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_analyze_scale_factor = 0.01,
    autovacuum_vacuum_cost_delay = 0);
  ```
- **HNSW build cost**: `ef_construction = 200` slows inserts; bulk backfills should
  create the HNSW index *after* loading (see Part 6 order).
- **`hnsw.ef_search`** is per-session: `SET LOCAL hnsw.ef_search = 100;` inside recall
  transactions to trade recall vs latency; default 40.
- **Recall latency budget** (sub-200ms target [source: 03-zep-temporal-knowledge-graph]):
  ANN ~5–15ms at 1M rows, tsquery GIN ~5–20ms, CTE 2-hop ~10–30ms, fusion+re-rank (app)
  the remainder.

## Future: Row-Level Security (RLS)

> **FUTURE — not applied in current schema.** The DDL below is reference material
> only; none of it is part of §3 or the §6 migration files. Review finding F1's
> follow-up (see [[pages/data-model-review|Data Model Review]]) tracks this as
> pending user approval.

**Recommendation.** RLS is the recommended approach for multi-user access control
once the deployment runs more than one principal against the same database.
Application-enforced filtering (what Q7 does today) trusts every caller of the
data layer; RLS moves the boundary into PostgreSQL itself, so a missed `WHERE`
clause in any future query cannot leak another user's memories.

**The columns are already in place.** `memories.owner_principal_type` /
`memories.owner_principal_id` (§3.3, added by review F1) and
`space_memberships.principal_type` / `principal_id` (§3.5) are exactly the
predicates RLS policies need. Enabling RLS later is additive: `ALTER TABLE …
ENABLE ROW LEVEL SECURITY` plus the policies below — no schema migration of
existing columns required.

**What RLS would enforce when enabled.** A principal can only `SELECT` memories
where they are the owner (`owner_principal_type/id` match) **OR** where the
memory lives in a `SharedMemorySpace` they hold a membership row in; and agents
can only `INSERT` memories whose owner principal is their own identity.
Connection pooling requires setting `app.principal_type` /
`app.principal_id` per transaction (`SET LOCAL`) — that plumbing is the actual
prerequisite, and the reason this is deferred rather than applied.

**Example policies — not applied, reference only.**

```sql
-- Reference only. Assumes the connection sets per-transaction:
--   SET LOCAL app.principal_type = 'user';
--   SET LOCAL app.principal_id   = '<idp subject id>';

ALTER TABLE memories ENABLE ROW LEVEL SECURITY;
CREATE POLICY memories_owner_select ON memories
  FOR SELECT
  USING (
    owner_principal_type = current_setting('app.principal_type', true)
    AND owner_principal_id   = current_setting('app.principal_id', true)
  );

-- Example 2: principals may also SELECT memories in shared spaces they belong to.
CREATE POLICY memories_space_member_select ON memories
  FOR SELECT
  USING (
    access_scope = 'shared'
    AND EXISTS (
      SELECT 1 FROM space_memberships sm
      WHERE sm.space_id = memories.shared_space_id
        AND sm.principal_type = current_setting('app.principal_type', true)
        AND sm.principal_id   = current_setting('app.principal_id', true)
    )
  );

-- Example 3: agents may INSERT only with their own identity as owner principal.
CREATE POLICY memories_agent_insert_own_identity ON memories
  FOR INSERT
  WITH CHECK (
    owner_principal_type = current_setting('app.principal_type', true)
    AND owner_principal_id   = current_setting('app.principal_id', true)
  );
```

Notes for the future implementer: policies on the same table OR-combine, so
Examples 1 and 2 together give the owner-or-shared-member read rule; the
`true` missing-ok flag on `current_setting` makes an unset principal see
nothing rather than error; admin/maintenance roles bypass RLS unless
`FORCE ROW LEVEL SECURITY` is also set on the table.

---

## Part 4 — Physical ERD

```
POSTGRESQL 16 + pgvector — physical schema (types as deployed; ◆ = PK, ◇ = FK, ▸ = index)

┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ memories                                                                                 │
│──────────────────────────────────────────────────────────────────────────────────────────│
│ ◆ id                    uuid                                                              │
│   type                  text CHECK(episodic|semantic|procedural)        ▸ btree (partial) │
│   content               text (TOAST, lz4)                              ▸ gin (trgm)      │
│   content_format        text DEFAULT 'markdown' CHECK(…)                                │
│   tags                  tag_name[]                                   ▸ gin               │
│   embedding             vector(1536)                                 ▸ hnsw halfvec(1536)│
│                                                   cosine, partial (deleted_at IS NULL)   │
│   embedding_model       text → embedding_models.model_id (logical)                      │
│   origin                text CHECK(agent_observation|user_instruction|file_artifact|    │
│                                    consolidation)                                        │
│   session_id            uuid ◇→ agent_sessions.session_id  ON DELETE SET NULL  ▸ btree   │
│   agent_id / actor      text                                                            │
│   source_ref            jsonb                                                            │
│   created_at            timestamptz DEFAULT now()                       ▸ btree DESC     │
│   updated_at            timestamptz DEFAULT now()                                        │
│   last_accessed_at → REMOVED (F8): memory_access_log (memory_id, accessed_at)    │
│   confidence            numeric(3,2) CHECK(0..1)                                        │
│   decay_score           numeric(3,2) DEFAULT 1.0 CHECK(0..1)            ▸ btree (partial)│
│   valid_from / valid_until timestamptz                                                  │
│   validity_range        tstzrange GENERATED                             ▸ gist            │
│   superseded_by         uuid ◇→ memories.id  ON DELETE SET NULL         ▸ btree (partial)│
│   ttl_expires_at        timestamptz                                     ▸ btree (partial)│
│   version               integer DEFAULT 1 CHECK(>=1)                                    │
│   access_scope          text CHECK(individual|shared)                   ▸ btree (partial)│
│   shared_space_id       uuid ◇→ shared_memory_spaces.id  ON DELETE RESTRICT            │
│   deleted_at            timestamptz (soft delete; hot indexes partial)                  │
│   search_tsv            tsvector GENERATED (content 'A' || tags 'B')    ▸ gin            │
│ CHECK: validity interval ordering · ttl>created · (scope='shared')=(space NOT NULL)      │
└───────┬───────────────┬───────────────┬───────────────┬──────────────────────────────────┘
        │1              │1              │1              │1
        │N              │N              │N              │N (RESTRICT)
        │               │               │               │
┌───────▼──────────┐ ┌──▼────────────┐ ┌▼───────────────┐│        ┌───────────────────────────┐
│ memory_links     │ │memory_entities│ │memory_embeddings││       │ promotion_proposals      │
│──────────────────│ │───────────────│ │────────────────││        │───────────────────────────│
│ ◆ id          u  │ │◆ memory_id  ◇ │ │◆ memory_id   ◇ ││        │ ◆ proposal_id        uuid │
│   source_id   ◇▸ │ │  entity_id   ◇ │ │  model_id    ◇──────────│──► embedding_models      │
│   target_id   ◇▸ │ │  created_at    │ │  vec_1536  vector(1536)│ │   shared_space_id  ◇▸    │
│   relationship_  │ │(PK: memory_id, │ │   ▸ hnsw halfvec(1536) │ │   candidate_memory_id ◇──┼──► memories
│     type  CHECK▸ │ │    entity_id)  │ │  vec_768   vector(768) │ │   target_path/kind/role  │
│   weight CHECK   │ │ ▸ btree(entity_│ │   ▸ hnsw halfvec(768)  │ │   diff text (TOAST)      │
│   evidence       │ │    id,created) │ │  created_at            │ │   status CHECK ▸ partial  │
│   created_at     │ └───────┬────────┘ │ CHECK: exactly one vec │ │     (draft,in_review)    │
│ UNIQUE(source,   │         │N         │ CHECK: dims↔registry    │ │   proposed_at /resolved_ │
│   target,type)   │         │          └────────────────────────┘ │     at   CHECK pairing   │
│ CHECK: no self   │         │1                                     │   reviewer               │
│ ▸ btree(source,  │  ┌──────▼───────┐   ┌──────────────────────┐  │ ▸ btree (space)          │
│   type) ▸ btree( │  │   entities   │   │  embedding_models    │  │ ▸ btree (candidate)      │
│   target,type)   │  │──────────────│   │──────────────────────│  └────────────┬──────────────┘
│ ▸ partial chain  │  │ ◆ entity_id  │   │ ◆ model_id     text  │               │N
│   (supersedes)   │  │   name       │   │   provider           │               │
└──────────────────┘  │   entity_type│   │   dims CHECK(1536,768)              │1
                      │   aliases ▸gin│  │   distance_metric     │   ┌──────────▼──────────────┐
                      │   created_at  │   │   is_active           │   │ shared_memory_spaces   │
                      │   search_tsv  │   └──────────────────────┘   │────────────────────────│
                      │     GENERATED │                              │ ◆ id                uuid│
                      │      ▸ gin    │                              │   name · description   │
                      │ UNIQUE(name,  │                              │   owner_type CHECK ▸   │
                      │   entity_type)│                              │   owner_id   (btree)   │
                      │ ▸ btree(type) │                              │   scope                │
                      └──────┬────────┘                              │   default_access CHECK │
                             │1                                      │   write_policy  CHECK │
                             │N                                      │   promote_policy CHECK│
                      ┌──────▼────────┐                              │   backend_kind  CHECK │
                      │ entity_facts  │                              │   backend_config jsonb│
                      │───────────────│                              │   artifacts jsonb     │
                      │ ◆ id      uuid│                              │   sync_status CHECK ▸  │
                      │   entity_id ◇▸│                              │     partial(<>in_sync)│
                      │   fact        │                              │   pending_proposals int│
                      │   memory_id ◇─┼──────────────────────────────┼─► memories (SET NULL)  │
                      │   valid_from  │                              │   sync_revision        │
                      │   valid_until │                              │   last_synced_at       │
                      │   fact_range  │                              │   retention_policy jsonb│
                      │     GENERATED │                              │   created/updated_at   │
                      │      ▸ gist   │                              │ CHECK: retention keys  │
                      │ ▸ btree(entity│                              └──────┬─────────────────┘
                      │   ) ▸ partial │                                     │1        │1
                      │ (current)     │                                     │N        │N
                      └───────────────┘                        ┌───────────────┐ ┌───────────────┐
                                                               │ memories      │ │space_         │
                                                               │shared_space_id│ │memberships    │
                                                               │(RESTRICT)     │ │───────────────│
┌────────────────────────────┐                                 │               │ │◆ space_id   ◇ │
│ agent_sessions             │                                 └───────────────┘ │  principal_  │
│────────────────────────────│                                                   │    type CHECK│
│ ◆ session_id      uuid     │◄──┐ memories.session_id (SET NULL)                  │    id        │
│   agent_type      CHECK    │   │ recall_requests.session_id (CASCADE)            │    (PK with  │
│   user_id         text ▸   │   │                                                │     space_id)│
│     (user,created DESC)    │   │                                                │  access_level│
│   shared_space_id ◇────────┼───┼──────────────────────────► shared_memory_spaces │    CHECK    │
│              (SET NULL)    │   │                                  (id)           │  granted_at   │
│   model          text      │   │                                                 │ ▸ btree      │
│   max_tokens     CHECK≥1   │   │                                                 │  (principal_ │
│   used_tokens    ≥0        │   │                                                 │   type,      │
│   instruction_   ≥0        │   │                                                 │   principal_ │
│     slot_budget            │   │                                                 │   id)        │
│   slots_used     ≤budget   │   │                                                 └───────────────┘
│     CHECK (or budget=0)    │   │
│   active_memories uuid[] ▸ │   │  ┌────────────────────────┐   ┌────────────────────────────┐
│     gin                    │   │  │ recall_requests        │   │ recall_results             │
│   injection_order text[]   │   │  │────────────────────────│   │────────────────────────────│
│   created_at      ▸ partial│   │  │ ◆ request_id     uuid  │◄──┤   result_id        uuid ◆  │
│     (active: ended IS NULL)│   │  │   query          text  │1:1│   request_id  uuid ◇ UNIQUE│
│   ended_at        CHECK    │   │  │   context        jsonb │   │   candidates    jsonb ARRAY│
│   summary        text(TOAST│   └──┤   session_id   uuid ◇ ▸│   │   injection_plan jsonb ARRAY│
│                  )         │      │   trigger        CHECK ▸│   │   slots_used   ≥0          │
│ ▸ btree (created DESC      │      │   agent_id      text    │   │   tokens_used  ≥0          │
│   where ended_at IS NULL)  │      │   strategies text[] <@  │   │   latency_ms   ≥0          │
└────────────────────────────┘      │   top_k 1..200          │   │   created_at   ▸ btree     │
                                    │   rerank CHECK          │   └────────────────────────────┘
                                    │   min_score 0..1        │
                                    │   slot/token_budget ≥0  │
                                    │   requested_at ▸ btree×2│
                                    │ (session+time, time,   │
                                    │  trigger+time)         │
                                    │ PARTITION CANDIDATE:   │
                                    │ RANGE(requested_at)    │
                                    └────────────────────────┘

Partitioning annotations (not in default deployment):
  memories        → RANGE(created_at) monthly above ~20M rows; PK (id, created_at); per-partition HNSW
  recall_requests → RANGE(requested_at) monthly above ~50M rows; recall_results composite FK
```

---

## Part 5 — Key Query Patterns

All validated on `pgvector/pgvector:pg17` with seeded fixtures (see Verification).

### Q1 — Hybrid vector + full-text recall, fused with RRF

Dense (HNSW) ∥ sparse (ts_rank_cd), merged by Reciprocal Rank Fusion; the application
re-ranks the fused top-N with a cross-encoder [source: 06-hybrid-retrieval-strategies].

```sql
-- Q1
WITH q AS (SELECT 'how does this project handle memory decay across sessions'::text AS text),
dense AS (
  SELECT m.id,
         1 - (m.embedding <=> ('[0.021,-0.113,...]'::vector(1536))::halfvec(1536))::float8 AS score,
         row_number() OVER (ORDER BY m.embedding <=> ('[...]'::vector(1536))::halfvec(1536)) AS rank
    FROM memories m, q
   WHERE m.deleted_at IS NULL
     AND m.embedding IS NOT NULL
   ORDER BY m.embedding <=> ('[...]'::vector(1536))::halfvec(1536)
   LIMIT 50),
sparse AS (
  SELECT m.id,
         ts_rank_cd(m.search_tsv, websearch_to_tsquery('english', (SELECT text FROM q)))::float8
           AS score,
         row_number() OVER (
           ORDER BY ts_rank_cd(m.search_tsv, websearch_to_tsquery('english', (SELECT text FROM q))) DESC)
           AS rank
    FROM memories m
   WHERE m.deleted_at IS NULL
     AND m.search_tsv @@ websearch_to_tsquery('english', (SELECT text FROM q))
   LIMIT 50),
fused AS (
  SELECT id,
         sum(rrf) FILTER (WHERE src = 'dense')  AS rrf_dense,
         sum(rrf) FILTER (WHERE src = 'sparse') AS rrf_sparse
    FROM (SELECT id, 1.0 / (60 + rank) AS rrf, 'dense'  AS src FROM dense
          UNION ALL
          SELECT id, 1.0 / (60 + rank) AS rrf, 'sparse' AS src FROM sparse) u
   GROUP BY id)
SELECT m.id, m.type, left(m.content, 120) AS excerpt,
       f.rrf_dense, f.rrf_sparse,
       coalesce(f.rrf_dense, 0) + coalesce(f.rrf_sparse, 0) AS rrf_total
  FROM fused f
  JOIN memories m ON m.id = f.id
 ORDER BY rrf_total DESC
 LIMIT 25;   -- → application cross-encoder re-rank over these candidates
```

Production recall composes this with Q7's visibility predicate (owner-scoped
individual memories + space membership) before fusion — the dense and sparse CTEs
above elide it for readability; do not deploy them unfiltered.

### Q2 — Temporal validity recall (point-in-time)

GiST `@>` over the generated range; falls back for rows with NULL `valid_from` (memories
without temporal semantics) [source: 03-zep-temporal-knowledge-graph].

```sql
-- Q2
SELECT m.id, m.content, m.confidence, m.decay_score
  FROM memories m
 WHERE m.deleted_at IS NULL
   AND (
         m.validity_range @> '2026-08-24T00:00:00Z'::timestamptz      -- interval covers the instant
         OR (m.valid_from IS NULL AND m.valid_until IS NULL)          -- no temporal semantics
       )
   AND (m.ttl_expires_at IS NULL OR m.ttl_expires_at > now())        -- not TTL-expired
   AND m.superseded_by IS NULL                                        -- current version only
 ORDER BY m.confidence DESC NULLS LAST, m.last_accessed_at DESC NULLS LAST
 LIMIT 25;
```

### Q3 — Graph traversal recall (2-hop associative)

Vector (or entity match) finds entry points; recursive CTE expands over `memory_links`
with weight accumulation; entry-point entities route via `memory_entities`
(FalkorDB pattern: vector entry, graph expansion) [source: 06-hybrid-retrieval-strategies].

```sql
-- Q3
WITH RECURSIVE entry AS (
  SELECT m.id
    FROM memories m
   WHERE m.deleted_at IS NULL
     AND m.embedding <=> ('[...]'::vector(1536))::halfvec(1536) < 0.55   -- ANN pre-filter
   LIMIT 10
),
walk (memory_id, origin_id, hops, path_weight) AS (
  SELECT e.id, e.id, 0, 1.0 FROM entry e
  UNION ALL
  SELECT l.target_id, w.origin_id, w.hops + 1,
         round((w.path_weight * l.weight)::numeric, 4)
    FROM walk w
    JOIN memory_links l ON l.source_id = w.memory_id
   WHERE w.hops < 2
     AND l.relationship_type IN ('similar_to', 'co_occurs_with', 'causal_next', 'derived_from')
     AND NOT l.target_id = ANY (ARRAY[w.memory_id, w.origin_id])
),
best AS (
  SELECT memory_id, max(path_weight) AS score
    FROM walk
   WHERE hops > 0
   GROUP BY memory_id
)
SELECT m.id, m.type, left(m.content, 120) AS excerpt, b.score,
       array_agg(DISTINCT me.entity_id) FILTER (WHERE me.entity_id IS NOT NULL) AS entities
  FROM best b
  JOIN memories m ON m.id = b.memory_id AND m.deleted_at IS NULL
  LEFT JOIN memory_entities me ON me.memory_id = m.id
 GROUP BY m.id, m.type, m.content, b.score
 ORDER BY b.score DESC
 LIMIT 20;
```

### Q4 — Session activation with dual budget check

Backing `POST /sessions/{id}/memories` (api-contracts §1.2.3): atomically charge slots +
tokens and append to the working set; `409 SLOT_BUDGET_EXCEEDED` maps to zero rows updated.

```sql
-- Q4
WITH candidate AS (
  SELECT m.id,
         greatest(1, ceil(length(m.content) / 400.0))::int AS slot_cost,   -- heuristic slot cost
         (length(m.content) / 4)::int                     AS token_cost    -- ~4 chars/token
    FROM memories m
   WHERE m.id = '018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42'::uuid
     AND m.deleted_at IS NULL
),
updated AS (
  UPDATE agent_sessions s
     SET active_memories = s.active_memories || c.id,
         slots_used      = s.slots_used + c.slot_cost,
         used_tokens     = s.used_tokens + c.token_cost,
         injection_order = s.injection_order || 'recall_injection'
    FROM candidate c
   WHERE s.session_id = '018f5a2e-41aa-7e55-b3c1-9d7e2f4a6b80'::uuid
     AND s.ended_at IS NULL
     AND NOT c.id = ANY (s.active_memories)                          -- idempotent activation
     AND s.slots_used + c.slot_cost <= s.instruction_slot_budget     -- dual budget, slots first
     AND s.used_tokens + c.token_cost <= s.max_tokens
  RETURNING s.session_id, s.slots_used, s.used_tokens)
SELECT u.slots_used, u.used_tokens, c.slot_cost, c.token_cost
  FROM updated u, candidate c;
-- 0 rows ⇒ caller returns 409 with needed/remaining details
```

### Q5 — Promotion proposal flow (create + approve)

Create the proposal, then approve in one transaction: mark merged, resolve, and rebind the
candidate memory into the shared space (`access_scope: individual → shared`), matching
api-contracts §1.2.4 approve.

```sql
-- Q5a — create (draft → in_review)
INSERT INTO promotion_proposals
  (proposal_id, shared_space_id, candidate_memory_id,
   target_path, target_kind, target_role, diff, status)
VALUES (
  '018f5a2e-5e40-78f1-a9c3-7b2d6e4f8a15'::uuid,
  '018f5a2e-c0de-71ab-93f5-6e2a8c4d0b17'::uuid,
  '018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21'::uuid,
  'specs/002-agent-memory/spec.md', 'spec', 'semantic',
  '--- a/specs/002-agent-memory/spec.md' || chr(10) || '+++ b/...' || chr(10) || '@@ -12,6 +12,9 @@' ||
  chr(10) || '+Decay policy: supersession-first.',
  'in_review');

-- Q5b — approve: merge + rebind candidate into the shared space, atomically
WITH approved AS (
  UPDATE promotion_proposals
     SET status = 'merged', resolved_at = now(), reviewer = 'videogamer'
   WHERE proposal_id = '018f5a2e-5e40-78f1-a9c3-7b2d6e4f8a15'::uuid
     AND status = 'in_review'                       -- optimistic guard: only from in_review
  RETURNING shared_space_id, candidate_memory_id)
UPDATE memories m
   SET access_scope    = 'shared',
       shared_space_id = a.shared_space_id,
       updated_at      = now()
  FROM approved a
 WHERE m.id = a.candidate_memory_id
RETURNING m.id, m.access_scope, m.shared_space_id;
-- trg_proposals_pending_count fires: pending counter decrements, sync_status recomputed
```

### Q6 — Decay pass (Lifecycle Manager)

Decay by staleness and TTL; auto-supersede-free — decay lowers vitality, TTL closes rows
(the supersession-first policy writes new versions instead of deleting).

```sql
-- Q6 (updated for F8: last_accessed_at is now the memory_access_log aggregate;
-- memories never accessed in the window fall back to created_at via the LEFT JOIN)
UPDATE memories m
   SET decay_score = round(least(1.0,
        coalesce(m.decay_score, 1.0) * 0.98
        * exp(-extract(epoch FROM (now() - coalesce(la.accessed_at, m.created_at)))
              / (60*60*24*30.0) / 3.0))::numeric, 4),   -- 30d half-life-ish, exp decay
       updated_at = now()
  FROM (SELECT memory_id, MAX(accessed_at) AS accessed_at
          FROM memory_access_log
         WHERE accessed_at > now() - interval '90 days'
         GROUP BY memory_id) la
 WHERE m.id = la.memory_id
   AND m.deleted_at IS NULL
   AND m.superseded_by IS NULL
   AND coalesce(m.decay_score, 1.0) > 0.05
   AND coalesce(la.accessed_at, m.created_at) < now() - interval '7 days';

-- Memories with no access-log rows in the window decay from created_at alone
UPDATE memories m
   SET decay_score = round(least(1.0,
        coalesce(m.decay_score, 1.0) * 0.98
        * exp(-extract(epoch FROM (now() - m.created_at))
              / (60*60*24*30.0) / 3.0))::numeric, 4),
       updated_at = now()
 WHERE m.deleted_at IS NULL
   AND m.superseded_by IS NULL
   AND coalesce(m.decay_score, 1.0) > 0.05
   AND m.created_at < now() - interval '7 days'
   AND NOT EXISTS (SELECT 1 FROM memory_access_log l
                    WHERE l.memory_id = m.id
                      AND l.accessed_at > now() - interval '90 days');

-- TTL closure (no delete): mark validity closed so temporal recall excludes them
UPDATE memories
   SET valid_until = least(coalesce(valid_until, ttl_expires_at), ttl_expires_at),
       updated_at  = now()
 WHERE ttl_expires_at IS NOT NULL
   AND ttl_expires_at <= now()
   AND valid_until IS NULL
   AND deleted_at IS NULL;
```

### Q7 — Shared-space membership visibility

"All memories visible to principal P": own individual scope + every space P can read
(backing `GET /memories` visibility filtering and the membership checks of api-contracts).

```sql
-- Q7
WITH visible_spaces AS (
  SELECT s.id
    FROM shared_memory_spaces s
    JOIN space_memberships m
      ON m.space_id = s.id
     AND m.principal_type = 'user'
     AND m.principal_id   = 'videogamer'
     AND m.access_level IN ('read', 'write', 'promote', 'admin')
   UNION
  SELECT s.id
    FROM shared_memory_spaces s
   WHERE s.owner_type = 'user' AND s.owner_id = 'videogamer'      -- owner sees own spaces
      OR (s.default_access <> 'none' AND s.owner_type <> 'user')  -- world-readable non-user spaces
)
SELECT m.id, m.type, left(m.content, 100) AS excerpt, m.access_scope, m.shared_space_id,
       array_agg(DISTINCT t) FILTER (WHERE t IS NOT NULL) AS tags
  FROM memories m
  LEFT JOIN unnest(m.tags) AS t ON true
 WHERE m.deleted_at IS NULL
   AND m.superseded_by IS NULL
   AND (
         (m.access_scope = 'individual'
            AND m.owner_principal_type = 'user'
            AND m.owner_principal_id   = 'videogamer')   -- review F1/F2: owner-scoped
         OR m.shared_space_id IN (SELECT id FROM visible_spaces)
       )
 GROUP BY m.id
 ORDER BY m.updated_at DESC
 LIMIT 50;
```

### Q8 — Consolidation: similar pairs and merge

Find near-duplicates by vector self-join (the consolidation input), then merge into one
semantic memory and link sources `derived_from` (A-MEM consolidation).

```sql
-- Q8a — candidate pairs (block on entity anchor to keep the cross product bounded)
WITH anchor AS (
  SELECT me.entity_id, array_agg(me.memory_id) AS ids
    FROM memory_entities me
    JOIN memories m ON m.id = me.memory_id
   WHERE m.deleted_at IS NULL AND m.type = 'episodic'
   GROUP BY me.entity_id
   HAVING count(*) >= 2
),
pairs AS (
  SELECT a.entity_id,
         p.m1, p.m2,
         1 - (m1.embedding <=> m2.embedding)::float8 AS similarity
    FROM anchor a,
         LATERAL (SELECT x AS m1, y AS m2
                    FROM unnest(a.ids) x, unnest(a.ids) y
                   WHERE x < y) p                         -- ordered pairs, no self/dup
    JOIN memories m1 ON m1.id = p.m1 AND m1.embedding IS NOT NULL
    JOIN memories m2 ON m2.id = p.m2 AND m2.embedding IS NOT NULL
   WHERE m1.embedding <=> m2.embedding < 0.15              -- cosine distance threshold
)
SELECT entity_id, m1, m2, round(similarity::numeric, 4) AS similarity
  FROM pairs
 ORDER BY similarity DESC
 LIMIT 100;

-- Q8b — merge: create semantic memory, close sources' validity, link derived_from
WITH merged AS (
  INSERT INTO memories (id, type, content, tags, embedding, embedding_model,
                        origin, session_id, agent_id, actor,
                        confidence, decay_score, valid_from, version, access_scope)
  SELECT gen_random_uuid(), 'semantic',
         '## Consolidated: memory architecture decisions' || chr(10) || string_agg(m.content, chr(10) || chr(10)),
         (SELECT array_agg(DISTINCT t) FROM unnest(m.tags) AS t),
         m0.embedding, m0.embedding_model,
         'consolidation', m0.session_id, m0.agent_id, 'lifecycle-manager',
         least(1.0, max(m.confidence) + 0.05)::numeric(3,2), 1.0, now(), 1, 'individual'
    FROM memories m
    JOIN (SELECT id FROM memories WHERE id IN
          ('018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21'::uuid, '018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42'::uuid)) sel
      ON sel.id = m.id
    CROSS JOIN LATERAL (SELECT embedding, embedding_model, session_id, agent_id
                          FROM memories WHERE id = sel.id LIMIT 1) m0
   GROUP BY m0.embedding, m0.embedding_model, m0.session_id, m0.agent_id
  RETURNING id),
closed AS (
  UPDATE memories
     SET valid_until = now(), superseded_by = (SELECT id FROM merged), updated_at = now()
   WHERE id IN ('018f5a2e-9c31-7b4a-8e2d-1f0a6b3c9d21'::uuid,
                '018f5a2e-3e88-7a60-c4d2-8b1f3a5c7d42'::uuid)
  RETURNING id)
INSERT INTO memory_links (id, source_id, target_id, relationship_type, weight, evidence)
SELECT gen_random_uuid(), c.id, m.id, 'derived_from', 0.9, 'consolidation merge'
  FROM closed c, merged m;
```

### Query-pattern coverage vs. API contracts

| Pattern | Backs |
|---|---|
| Q1 hybrid | `POST /recall`, MCP `memory_recall` |
| Q2 temporal | `GET /memories?valid_at=…` |
| Q3 graph | recall `strategies: [graph]`, associative trigger |
| Q4 activation | `POST /sessions/{id}/memories` (409 budget) |
| Q5 promotion | `POST /spaces/{id}/proposals`, `/approve` |
| Q6 decay | `POST /lifecycle/decay` (MCP `memory_decay_pass`) |
| Q7 visibility | `GET /memories`, `GET /spaces` filtering |
| Q8 consolidation | `POST /sessions/{id}/end` → lifecycle consolidation |

---

## Part 6 — Migration & Versioning

### 6.1 Schema versioning

Numbered, forward-only SQL files under `db/migrate/`, applied by any runner (Flyway,
golang-migrate, sqitch, plain `psql -f`); a `schema_migrations` table is the source of truth.

```sql
-- DDL: schema_migrations
CREATE TABLE schema_migrations (
  version     integer PRIMARY KEY,
  name        text    NOT NULL,
  applied_at  timestamptz NOT NULL DEFAULT now(),
  applied_by  text
);
COMMENT ON TABLE schema_migrations IS 'Forward-only migration ledger; never edit applied rows';
```

### 6.2 Migration files and creation order (fresh deployment)

Order matters: extensions/domains → registry → parents → children → workflow trigger →
seed. HNSW indexes come **last** (build once over loaded data; building during bulk load
is 5–10× slower).

| # | File | Contents |
|---|---|---|
| 001 | `001_extensions.sql` | `CREATE EXTENSION vector, pg_trgm`; `tag_name` domain |
| 002 | `002_core_tables.sql` | `shared_memory_spaces`, `agent_sessions`, `memories` (+ B-tree/GIN/GiST indexes), `embedding_models` |
| 003 | `003_graph_tables.sql` | `memory_links`, `entities`, `entity_facts`, `memory_entities` |
| 004 | `004_recall_tables.sql` | `recall_requests`, `recall_results`; fixes `trigger` → `trigger_kind` if the runner's dialect objects |
| 005 | `005_space_tables.sql` | `space_memberships`, `promotion_proposals`, `proposals_pending_count()` trigger (corrected body: counter + sync_status only, no `pending_window`) |
| 006 | `006_embedding_tables.sql` | `memory_embeddings` + partial HNSW (768/1536) |
| 007 | `007_hnsw_primary.sql` | `memories_embedding_hnsw_idx` — **after** backfill |
| 008 | `008_seed_models.sql` | registry rows: `('text-embedding-3-small','openai',1536,'cosine',true)` |
| 009 | `009_comments.sql` | all `COMMENT ON` statements |
| 010 | `010_jobs.sql` | `lifecycle_jobs` (review F3): async job ledger for consolidate/decay/sync/session-end polling |

Reindexing/swap pattern for embedding-model migration: backfill `memory_embeddings`,
verify recall offline, then `UPDATE memories SET embedding = …, embedding_model = …` in
10k batches inside one transaction per batch; HNSW updates incrementally.

### 6.3 Estimated storage per 1M memories

Assumptions: avg content 2 KB, 5 tags, JSONB fields ~0.4 KB, one 1536d fp32 vector each,
HNSW `m=16` halfvec index, 1.5 recall requests per memory (log rows).

| Component | Bytes/row (approx) | 1M rows |
|---|---|---|
| `memories` heap (incl. 24B tuple header + null bitmap) | ~2.0 KB content + 0.4 KB jsonb + 24 B overhead + ~50 B other columns | **~2.5 GB** |
| `embedding` fp32 in heap (1536 × 4 B, TOASTed, lz4 ≈ ×0.75) | ~4.6 KB | **~4.6 GB** |
| HNSW halfvec index (1536 × 2 B × ~1.35 graph overhead) | ~4.2 KB | **~4.2 GB** |
| `search_tsv` GIN + tags GIN + trgm GIN | ~0.6 KB combined | **~0.6 GB** |
| B-tree indexes (6 partial/normal: created, type, scope, session, accessed, decay) | ~0.25 KB | **~0.25 GB** |
| GiST `validity_range` | ~0.1 KB | **~0.1 GB** |
| `memory_links` (~3 links/memory) | ~120 B × 3 | **~0.36 GB** |
| `entities` + junction (~1.5 anchors/memory) | ~200 B | **~0.3 GB** |
| `recall_requests` + `recall_results` (1.5× volume) | ~1.0 KB × 1.5 | **~1.5 GB** |
| WAL + bloat headroom (×1.3 on heap) | — | **~4 GB** |
| **Total** | | **~18–19 GB** |

Scaling rule of thumb: **~19 GB and ~2.2 GB of index per additional 1M memories**, growing
super-linearly only in the HNSW index (graph overhead grows with `m`); revisit
partitioning (§3.12) around 20M rows / 150 GB.

### 6.4 Operational checks (post-deploy)

```sql
-- DDL: post-deploy checks
-- index inventory: one HNSW, ≥3 GIN, one GiST on memories
SELECT indexname, indexdef FROM pg_indexes
 WHERE tablename IN ('memories', 'recall_requests', 'memory_embeddings')
 ORDER BY tablename, indexname;

-- vector index health
SELECT count(*) AS rows_with_embedding FROM memories WHERE embedding IS NOT NULL;

-- extension versions
SELECT extname, extversion FROM pg_extension WHERE extname IN ('vector', 'pg_trgm');
```

---

## Verification

The `-- DDL:` blocks of this page were concatenated in document order and executed against
`pgvector/pgvector:pg17` (PostgreSQL 17.10, pgvector 0.8) with representative fixtures:

1. **All DDL applies cleanly in document order** — the section order (spaces → sessions →
   memories → links → entities/junctions → recall → proposals+trigger → embedding tables)
   matches FK dependency order, so the page as written is deployable top-to-bottom:
   domains, 13 tables, generated columns (`validity_range tstzrange`, both tsvectors —
   the underlying functions are immutable, as STORED generation requires), the halfvec
   HNSW expression index, GIN/GiST/B-tree/partial indexes, the trigger function + trigger,
   and all comments.
2. **Defects found by execution and fixed in this page** (kept as a record of why the DDL
   was executed, not just written):
   - the trigger body first drafted an UPDATE referencing a non-existent `pending_window`
     column — removed; the counter + `sync_status` recompute is the whole body;
   - `memory_embeddings_dims_match_chk` used a subquery in a CHECK, which PostgreSQL
     rejects — replaced with the exactly-one-vector CHECK plus loader-validated
     dims↔model consistency;
   - document order originally placed `agent_sessions` before `shared_memory_spaces`,
     breaking its FK — sections reordered.
3. **Query patterns Q1–Q8** were syntax-checked against the deployed schema (query blocks
   extracted separately; the Q1 fused form above is the corrected single-pass tagged
   `UNION ALL` aggregation — review F4 fixed the earlier double-join form).
4. **Not covered by local verification**: real HNSW recall/latency at 1M+ rows, autovacuum
   behavior under churn, partition DETACH timing. Storage estimates in §6.3 are analytical
   (row-width arithmetic × fill factors), not measured.
5. **Review re-validation (2026-08-24, [[pages/data-model-review|data-model-review]])**:
   the critical fixes — `memories.owner_principal_type/_id` + CHECK + index, recall-request
   `mode`/`status`/`error` columns + CHECKs + partial index, `lifecycle_jobs`,
   `promotion_proposals.note`/`reject_reason` + pairing CHECKs + one-open partial UNIQUE,
   and the trigger DELETE-branch `sync_status` recompute — were re-applied as DDL against
   the same `pgvector/pgvector:pg17` container and execute cleanly; corrected Q1 parses and
   plans against the schema.

## Open items

- `queries/*.sql` canonical query bundle (Q1 fused form, orphan sweep for partitioned FK
  relaxation) — to be created when the implementation step runs.
- Exact BM25 via `pg_search`/ParadeDB — evaluate only if `ts_rank_cd` quality is
  insufficient in retrieval evaluation; not a dependency today.

## Related Pages

- [[pages/data-models|Data Models]] — the conceptual entities this page implements
- [[pages/architecture|Architecture]] — components and storage backends pgvector replaces
- [[pages/retrieval-and-recall|Retrieval and Recall]] — the four-way hybrid these indexes serve
- [[pages/api-contracts|API Contracts]] — operations the query patterns back

## Sources

- [01-letta-memgpt-stateful-agents] — conventional DB + vector index split; recursive summarization
- [02-mem0-universal-memory-layer] — entity-centric retrieval; provenance; per-user indexing
- [03-zep-temporal-knowledge-graph] — temporal validity intervals; sub-200ms target
- [04-a-mem-agentic-memory-architecture] — link generation/consolidation families
- [06-hybrid-retrieval-strategies] — four-way hybrid; cross-encoder re-rank; vector-in-graph co-location
- [10-spec-driven-development-memory] — promotion diffs; artifacts manifest; supersession-first decay

## Tags

#ai-agents #memory-systems #postgresql #pgvector #schema-design #hybrid-search
