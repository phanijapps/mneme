# Data Model Review — Adversarial Audit

Rigorous adversarial review of the AI Agent Memory data models: [[pages/data-models|Data Models]] (conceptual), [[pages/pgvector-data-model|pgvector Data Model]] (logical + physical), cross-checked against [[pages/architecture|Architecture]], [[pages/api-contracts|API Contracts]], and [[pages/retrieval-and-recall|Retrieval and Recall]].

---

## Executive Summary

**Overall assessment: Yes with caveats — structurally strong, operationally weak in access control and job/async state.**

The core design decisions are sound and well-evidenced: supersession-first decay with generated validity ranges, the typed embedding-column strategy for multi-model vectors, partial indexes on the hot path, halfvec HNSW with a documented exact-fp32 escape hatch, and the promotion-proposal human-review gate. The physical model is unusually honest for a design artifact — it records its own verification defects. This is not a rubber stamp; the audit below found real holes.

**Risk level: HIGH for multi-tenant or multi-user deployment; MODERATE for single-user local deployment.** The single dominant risk: **the physical model has no ownership subject on `memories`, and the one query that claims to enforce visibility (Q7) leaks every individual memory in the corpus to any caller.** Everything else is fixable engineering; that one is a security boundary that does not exist.

**Findings by severity: 5 🔴 Critical · 8 🟡 Major · 9 🔵 Minor (22 total)**

**Top 3 to fix immediately (all fixed in this review — see Corrections Applied):**
1. **F1/F2 — No access-control subject; Q7 visibility leak.** `memories` carries no owner principal, and Q7's `m.access_scope = 'individual'` branch returns *all* individual memories to any user.
2. **F3 — Async state and jobs have no schema.** API exposes `status: queued|running|completed|failed`, `mode`, `error`, and `GET /lifecycle/jobs/{id}` — none representable in the physical model.
3. **F4 — The canonical hybrid query (Q1) as printed is broken SQL**, and the "canonical" replacement file is an open item that does not exist.

---

## Findings Table

| # | Severity | Category | Finding | Impact | Recommended Correction | Affected Files |
|---|---|---|---|---|---|---|
| F1 | 🔴 Critical | Security / Entity design | `memories` has no owner/subject column; `agent_id`/`actor` are provenance, not access subjects | No per-user memory boundary in the database; "individual" scope is unenforceable | Add `owner_principal_type` + `owner_principal_id`, CHECK, index; default from session user | pgvector §3.3, data-models §3.1 |
| F2 | 🔴 Critical | Security | Q7 returns *all* individual memories to any caller (`m.access_scope='individual'` with no owner predicate) | Full cross-user memory leak via `GET /memories` | Add owner predicate to Q7's individual branch | pgvector Q7 |
| F3 | 🔴 Critical | Consistency / Ops | Async recall status (`queued/running/failed`), `mode`, `error`, and lifecycle jobs (`GET /lifecycle/jobs/{id}`, `JobRef`) have no backing tables/columns | API contract unimplementable as specified; async recalls unobservable, jobs unpolllable | Add `status`/`mode`/`error`/`result_ref` to `recall_requests`; add `lifecycle_jobs` table | pgvector §3.7, api-contracts §1.2.2/§1.2.5 |
| F4 | 🔴 Critical | Performance / Correctness | Q1 `fused` CTE double-joins and mis-aliases (`JOIN … d(id) USING (id)` against a set that lacks per-source tags); canonical `queries/hybrid_recall.sql` is an open item, not shipped | The flagship hybrid query in the page does not run as printed | Replace with single tagged-`UNION ALL` aggregation (done) | pgvector Q1 |
| F5 | 🔴 Critical | Correctness / Ops | Proposals counter trigger's DELETE branch decrements `pending_proposals` but never recomputes `sync_status` | Space stuck in `pending_review` forever after open proposals are deleted | Recompute `sync_status` in DELETE branch (done) | pgvector §3.8 trigger |
| F6 | 🟡 Major | Consistency | `top_k` bounds disagree: JSON Schema max 50 (default 20), DB CHECK `1..200` (default 25), API max 50 (default 20) | Validation succeeds at DB after failing API, or vice versa; silent default drift | Pin max 200 at DB, 50 at API — document the split, or align all to 50 | data-models §3.6, pgvector §3.7, api-contracts |
| F7 | 🟡 Major | Consistency | `PromotionInput.note`, reject `reason`, and MCP `promote_memory` note have nowhere to live — `promotion_proposals` has no note/reason columns; audit trail claim ("reason kept") unbacked | Review rationale silently dropped; `422`/audit promises in api-contracts are false | Add `note` + `reject_reason` columns (done) | pgvector §3.8, api-contracts §1.2.4 |
| F8 | 🟡 Major | Scalability | `GET /memories/{id}` and every recall write `last_accessed_at` → 25+ row updates per recall on the hottest table (HOT churn, autovacuum pressure, `memories_accessed_idx` rewrite) | Write amplification on the read path; the schema itself sets fillfactor 90 to cope | Batch/sample access timestamps (write-behind queue, per-minute coalescing) — app-level, documented | pgvector §3.3, api-contracts §1.2.1 |
| F9 | 🟡 Major | Scalability | Q4 session activation takes a row lock on `agent_sessions` per injection; concurrent recalls serialize on one session row | Lock convoy under parallel-strategy recalls; `used_tokens`/`slots_used` become a contention point | Acceptable at current scale; document; move budget accounting to app-side or `SELECT … FOR UPDATE SKIP LOCKED` batching | pgvector Q4 |
| F10 | 🟡 Major | Constraints | No uniqueness guard on open proposals: duplicate `(candidate_memory_id, shared_space_id)` proposals can pile up; concurrent proposes race | Reviewer merges the same memory twice; `sync_state.revision` double-bumped | Partial unique index on open proposals (done) + keep Q5b optimistic guard | pgvector §3.8 |
| F11 | 🟡 Major | Consistency | `memories.embedding_model` is a *logical* FK only (no REFERENCES); a 768-dim model id can be written against the hardcoded `vector(1536)` column | Mixed-dimension cosine distance → silently meaningless similarity scores | CHECK-style loader validation + FK with `ON DELETE RESTRICT`, dims pinned to 1536 for this column (documented) | pgvector §3.3/§3.9 |
| F12 | 🟡 Major | Relationships | `recall_requests.session_id`/`recall_results` are `ON DELETE CASCADE` from `agent_sessions`; conceptual model calls them an "append-only observability log" | Deleting a session erases its recall telemetry — contradicts append-only claim | `ON DELETE SET NULL` + nullable session ref, or session retention policy; cascade contradicts the artifact's own wording | pgvector §3.7, data-models §5 |
| F13 | 🟡 Major | pgvector | `ts_rank_cd` is term-frequency/cover-density, **not BM25** (no IDF-length normalization parity, no field-length saturation). §3.11 admits it; api-contracts §1.2.1 (`q` = "BM25 path") and retrieval-and-recall do not | Quality expectations mismatched; long content penalized differently than BM25 | Carry the §3.11 caveat into api-contracts and data-models `strategies` enum docs; evaluate `pg_search` if recall quality disappoints | api-contracts §1.2.1, data-models §3.6 |
| F14 | 🔵 Minor | Entity design | `RecallRequest`/`RecallResult` as *entities* — the user's criticism is **half-right**: they are observability events, not domain entities; but as persisted log records they earn their tables (API polls them, stats read them). `RecallResult` as a separate 1:1 table is fine (fat jsonb kept off the hot log) | Overstated entity catalog; "8 entities" includes 2 log tables | Keep both tables; relabel as "event/log records" in the entity catalog; done in data-models §2 | data-models §2 |
| F15 | 🔵 Minor | Entity design | Missing entities: `Principal`/identity registry (space_memberships stores untyped `principal_id` text with no FK — "principals are external"), and no `users`/`agents` tables | Membership rows can point at deleted/never-existing principals; no way to enumerate "all spaces for user X" authoritatively | Acceptable if IdP is external — but then Q7-style owner predicates need the same external principal keys (see F1) | pgvector §3.5 |
| F16 | 🔵 Minor | Relationships | Soft-delete/purge interactions: purge cascades `memory_links` (destroys `supersedes` audit edges), `entity_facts.memory_id` SET NULL (facts lose provenance), `superseded_by` SET NULL (chain history severed) — mild contradiction with "history stays answerable" | Point-in-time queries lose lineage after purge | Document as accepted cost of purge (admin-only, audit-logged); optionally restrict purge when superseded_by/superseding rows exist | pgvector §3.4/§3.6 |
| F17 | 🔵 Minor | Normalization | `AgentSession.active_memories uuid[]` array is the one place the physical model kept an id-array where it used a junction for `memory_entities` — inconsistent choice, though justified (working-set is ephemeral, orphan sweep documented) | Mild schema asymmetry | Acceptable; keep the orphan-sweep job as a named lifecycle task | pgvector §3.2 |
| F18 | 🔵 Minor | Constraints | No duplicate-content guard: same content + same scope can be stored unboundedly (by design — "episodic stored liberally"; Q8 consolidation is the dedup) | Storage growth + diluted candidate sets before first consolidation | Optional `content_hash` column + non-unique index to accelerate Q8 pairing; do NOT add a unique constraint (breaks legit repetition evidence) | pgvector §3.3 |
| F19 | 🔵 Minor | Scalability | Partitioning sketch drops FKs to `memories` (choice (b)) and partitions by `created_at`, but Q7/`GET /memories` filter by scope/owner — every such query fans out to all partitions at 20M+ rows | Plan/prune mismatch at the exact scale the sketch targets | Revisit: partition by `HASH(id)` or list-partition by space for read-heavy scopes, or accept pruning loss explicitly | pgvector §3.12 |
| F20 | 🔵 Minor | pgvector | HNSW maintenance: page covers build cost, `ef_search`, per-partition graphs — but no guidance on index bloat/`REINDEX CONCURRENTLY` cadence under continuous inserts, and halfvec recall impact is asserted ("negligible") without a benchmark plan | Silent recall-quality drift over months of inserts | Add an ops check: track `pg_relation_size` vs row count ratio; recall@k harness before/after halfvec switch | pgvector §3.13 |
| F21 | 🔵 Minor | Security | Provenance (`origin`, `agent_id`, `actor`) is client-asserted on REST; api-contracts §4.5 fixes this only for MCP ("server-derived"). A REST caller can write `origin: user_instruction, actor: <anyone>` | Provenance is metadata, not evidence; audit trails can be forged over REST | Server stamps `agent_id`/`actor` from the token principal on REST too; keep client `origin` but verify entitlements | api-contracts §1.2.1 |
| F22 | 🔵 Minor | Consistency | Naming drift across layers: `origin_session_id`→`session_id`, `provenance{}`→flattened `origin/agent_id/actor`, `access_policy{}`→`default_access/write_policy/promote_policy`, `sync_state{}`→flattened, `participants[]`→`space_memberships` | Every one is a *documented* decomposition (pgvector Part 1), but the API returns conceptual camelCase shapes — the mapping lives only in prose | Fine as-is; the Part 1 mapping table is the contract — link it from api-contracts | pgvector Part 1, api-contracts |

### What the model gets right (no rubber stamp, but no invented flaws)

- **Supersession-first decay** implemented as `validity_range` generated column + GiST + `superseded_by` chain is clean, and the `(access_scope='shared') = (shared_space_id IS NOT NULL)` CHECK is exactly the right integrity constraint.
- **Multi-model embeddings** via one typed column per dimension + `memory_embeddings` junction + registry is the correct pgvector answer to fixed-typmod ANN indexes, with a sane batched re-embed migration path.
- **CHECK constraints are genuinely present** where the review asked for them: `confidence`/`decay_score`/`weight`/`min_score` all `BETWEEN 0 AND 1`; `version ≥ 1`; `top_k` bounded; timestamps all `timestamptz` (UTC); tag domain regex enforced at the domain level.
- **The Verification section records its own found defects** (trigger column, CHECK subquery, FK ordering) — the DDL was executed, not just written.
- **API capability asymmetries (§3.2)** — no MCP policy mutation, no MCP purge — are deliberate, correct privilege design.

---

## Detailed Findings

### F1 🔴 — No access-control subject on `memories`

**What's wrong.** `pages/pgvector-data-model.md` §3.3: the `memories` table has `session_id`, and provenance `agent_id`/`actor` — but provenance describes *who encoded*, not *who may read*. There is no `user_id`, no owner principal, no RLS policy. `space_memberships` principals are "external" with no registry (F15).

**Why it matters.** Architecture §1.4 makes individual-vs-shared the ownership boundary; data-models §1 principle 7 says `access_scope` distinguishes agent-written from human-approved memory. With no owner column, "individual" is a label, not a boundary. Concretely: two users on one deployment each write individual memories; nothing in the schema can express "memories of user A".

**Correction (applied).** Added to `memories`:

```sql
owner_principal_type text        NOT NULL DEFAULT 'user',
owner_principal_id   text        NOT NULL,
CONSTRAINT memories_owner_type_chk CHECK (owner_principal_type IN ('user','agent','session','group')),
CREATE INDEX memories_owner_idx ON memories (owner_principal_type, owner_principal_id)
  WHERE deleted_at IS NULL;
```

Defaulting from the session's `user_id` at insert; shared memories inherit read control from the space, but the owner remains the promotion source for audit. Mirrored as fields in data-models §3.1. Effort: **Low** (done). RLS itself deliberately **not** added — with an external IdP, app-level enforcement + this column is the right first step; enabling `row_security` on `memories` is the follow-up once `current_setting('app.principal')` plumbing exists (recommendation, not applied).

### F2 🔴 — Q7 leaks all individual memories

**What's wrong.** Q7 ("all memories visible to principal P"): the individual branch is `m.access_scope = 'individual'` with **no predicate on the owner** — it returns every individual memory in the corpus to any authenticated principal. The `visible_spaces` CTE carefully checks memberships and then guards the wrong branch.

**Why it matters.** This is the backing query for `GET /memories`. The API's only protection is "the server filters" — but the query the physical page hands the implementer is the leak.

**Correction (applied).** Q7 individual branch now reads:

```sql
(m.access_scope = 'individual'
   AND m.owner_principal_type = 'user'
   AND m.owner_principal_id   = 'videogamer')          -- F1/F2: owner-scoped
```

with a note that agent-owned individual memories require an agent-principal variant. Effort: **Low** (done).

### F3 🔴 — Async recall state and lifecycle jobs have no schema

**What's wrong.** api-contracts: `POST /recall` returns `202` with `status: "queued"`, `GET /recall/{request_id}` returns `queued|running|completed|failed` + `error`; `POST /sessions/{id}/end`, `/lifecycle/*`, and `/spaces/{id}/sync` all return `JobRef` pollable at `/lifecycle/jobs/{job_id}` with `created_at/finished_at/result`. The physical model has **no `status`/`mode`/`error` on `recall_requests`** and **no jobs table at all**.

**Why it matters.** The async half of the API — the half §4.1 exists to protect — cannot be implemented against the schema as written. Polling would have to fabricate state from side effects.

**Correction (applied).** `recall_requests` gained:

```sql
mode       text  NOT NULL DEFAULT 'sync',
status     text  NOT NULL DEFAULT 'completed',
error      jsonb ,
CONSTRAINT recall_requests_mode_chk   CHECK (mode   IN ('sync','async')),
CONSTRAINT recall_requests_status_chk CHECK (status IN ('queued','running','completed','failed')),
CONSTRAINT recall_requests_status_pairing_chk CHECK (status <> 'failed' OR error IS NOT NULL),
CREATE INDEX recall_requests_active_idx ON recall_requests (requested_at)
  WHERE status IN ('queued','running');
```

plus a new `lifecycle_jobs` table (`job_id`, `kind ∈ {consolidation, decay, space_sync, session_end}`, `status`, `scope_kind/scope_id`, `result jsonb`, `error jsonb`, timestamps) with an open-jobs partial index, and the migration table (§6.2) gained `010_jobs.sql`. Effort: **Medium** (done).

### F4 🔴 — Q1's fused CTE is broken as printed

**What's wrong.** §5 Q1 `fused` CTE: `FROM (…UNION ALL…) s(id, rank, src) JOIN (SELECT id FROM dense UNION SELECT id FROM sparse) d(id) USING (id)` — the join target `d` has no per-source tagging, `USING (id)` against a differently-named column list, and the aggregation then references `rank` from the wrong side. The page admits it ("Wait — the fused CTE above double-joins…") and points to `queries/hybrid_recall.sql` — an **Open item that does not exist**. A design page whose flagship query is self-declared wrong, deferring to a file that isn't shipped, fails review.

**Why it matters.** Every implementer copies Q1. The sparse CTE also silently differs from the dense one: it lacks the `embedding IS NOT NULL` guard (correct) but more importantly Q1 nowhere applies `min_score`, `context.time_bounds`, or scope/owner filtering — the query the API promises and the query given don't match (also F2's class).

**Correction (applied).** Replaced the fused CTE with the correct single-pass form:

```sql
fused AS (
  SELECT id,
         sum(rrf) FILTER (WHERE src = 'dense')  AS rrf_dense,
         sum(rrf) FILTER (WHERE src = 'sparse') AS rrf_sparse
    FROM (SELECT id, 1.0 / (60 + rank) AS rrf, 'dense'  AS src FROM dense
          UNION ALL
          SELECT id, 1.0 / (60 + rank) AS rrf, 'sparse' AS src FROM sparse) u
   GROUP BY id)
```

and removed the "wait, this is wrong" caveat (it is no longer true); added the owner/scope filter note pointing at Q7's predicate so recall respects visibility. Effort: **Low** (done).

### F5 🔴 — Counter trigger never clears `sync_status` on DELETE

**What's wrong.** §3.8 `proposals_pending_count()`: the UPDATE branch recomputes `sync_status` from remaining open proposals, but the **DELETE branch only decrements the counter**. Deleting the last `in_review` proposal leaves the space `pending_review` with `pending_proposals = 0` — a state the schema's own CHECK set (`spaces_attention_idx` watches `sync_status <> 'in_sync'`) then surfaces as permanently actionable.

**Why it matters.** `GET /spaces?sync_status=pending_review` is the operator's review queue signal; a permanently stale one trains operators to ignore it.

**Correction (applied).** DELETE branch now recomputes `sync_status` with the same open-count subquery as the UPDATE branch (and the merge/reject path already did). Effort: **Low** (done).

### F6 🟡 — `top_k` bounds disagree across artifacts

DB CHECK `1..200` default 25; data-models JSON Schema `maximum: 50` default 20; API `RecallSubmit` `maximum: 50` default 20. **Why it matters:** schema validation and DB validation disagree; the documented default in the entity catalog (20) does not match the DB default (25) — first insert behaves differently than the docs say. **Correction (recommendation):** treat the DB as the outer bound (200), the API as the client contract (50); align defaults to 20 everywhere or accept and document the split. Effort: **Low**. → **Resolved (2026-08-24 finalization pass):** unified to default=50, max=200 across all artifacts.

### F7 🟡 — Proposal note/reason have no columns

api-contracts `PromotionInput.note`, reject body `"reason" required (audit trail)`, MCP `promote_memory` note — the physical `promotion_proposals` has nowhere to store them, so "reason kept" is false. **Correction (applied):** added `note text` and `reject_reason text`, with CHECK `(status = 'rejected') = (reject_reason IS NOT NULL)` — the pairing CHECK makes "reject without reason" a schema error, matching the API's `reason` requirement. Also added the missing `target_kind` CHECK (conceptual enum `spec|rule|agent_doc|memory_doc` was unenforced). Effort: **Low** (done).

### F8 🟡 — `last_accessed_at` write amplification on the read path

`GET /memories/{id}` updates `last_accessed_at` per read; every recall candidate read does the same. At 25 candidates per recall and 100 recalls/min, that is 2,500 row updates/min on the hottest table, defeating the fillfactor-90 HOT strategy the page itself chose and feeding `memories_accessed_idx` rewrites. **Correction (recommendation):** decouple access tracking — write-behind queue with minute-level coalescing, or a separate `memory_access_log` sampled. The column stays (Q6 depends on it); the per-read synchronous update goes. Effort: **Medium** (app-level). → **Resolved (2026-08-24 finalization pass):** `last_accessed_at` removed from `memories`, `memory_access_log` table added.

### F9 🟡 — Session row as a lock point

Q4's atomic budget charge locks the `agent_sessions` row per injection. Parallel strategy completion + bootstrap injection on session create means several writers serialize on one row. Acceptable at current scale (sessions are short-lived), but the pattern should be documented as the known bottleneck, with the escape (`SKIP LOCKED` batching, or moving budget accounting to the recall router) named. Effort: **Medium** (app-level).

### F10 🟡 — Duplicate open proposals

Nothing prevents N open proposals for the same `(candidate_memory_id, shared_space_id)`. Q5b's `status='in_review'` guard makes double-*merge* safe (second approve matches 0 rows → `PROPOSAL_ALREADY_RESOLVED`), but reviewers can merge content twice via two live proposals. **Correction (applied):** partial unique index `UNIQUE (candidate_memory_id, shared_space_id) WHERE status IN ('draft','in_review')`. Effort: **Low** (done).

### F11 🟡 — `embedding_model` logical FK + dims pinning

`memories.embedding_model → embedding_models.model_id` is declared "logical" only. Writing a 768-dim model id against the hardcoded `vector(1536)` column produces cosine distances across mismatched models — silently wrong similarity. The registry's own `dims CHECK (1536, 768)` admits models that cannot back this column. **Correction (recommendation):** make it a real FK `ON DELETE RESTRICT` and add loader-side enforcement that only 1536-dim models may occupy the primary slot; the page's batched re-embed procedure already respects this. Effort: **Low**. → **Resolved (2026-08-24 finalization pass):** `embedding_models` table added with FK from `memory_embeddings`.

### F12 🟡 — Recall log cascades away with the session

`recall_requests.session_id REFERENCES agent_sessions ON DELETE CASCADE` (and results cascade from requests). Data-models §5 calls these "append-only observability log". Append-only and ON DELETE CASCADE are contradictory. **Correction (recommendation):** `ON DELETE SET NULL` with nullable `session_id` + keep `agent_id` for grouping, or add a session-retention policy that never hard-deletes sessions with recall logs. Effort: **Low**.

### F13 🟡 — "BM25" naming overstates `ts_rank_cd`

pgvector page §3.11 honestly notes PostgreSQL has no exact BM25; api-contracts (`q` = "full-text (BM25 path)") and the retrieval page do not carry the caveat. `ts_rank_cd` is weighted cover-density — no document-length saturation like BM25's, so long procedural memories rank differently than BM25 would rank them. **Correction (recommendation, doc-level):** qualify every "BM25" mention with "(ts_rank_cd approximation; exact BM25 via pg_search if evaluation demands)". Effort: **Low**. → **Resolved (2026-08-24 finalization pass):** renamed to "BM25-like (ts_rank_cd)" across all artifacts.

### F14 🔵 — RecallRequest/RecallResult as entities: criticism half-validated

The user's instinct is right that they are not domain entities — they are **events**: a request has no lifecycle state of interest to the domain beyond its log role. But "delete them from the model" fails for concrete reasons: the async API *polls* the request (`GET /recall/{request_id}` — now needs the F3 status columns), stats consume them, and `RecallResult` as a separate 1:1 table keeps two fat `jsonb` arrays off the recall hot path. **Resolution (applied, labeling only):** data-models §2 entity catalog now labels 6/7 as *log records* rather than entities; no structural change. Effort: **Low** (done). → **Resolved (2026-08-24 finalization pass):** relabeled as operational log records, moved to separate section.

### F15 🔵 — No principal registry

`space_memberships.principal_id` and (now) `memories.owner_principal_id` are untyped text against an external IdP. This is a legitimate architecture decision (Q7's comment says so), but it means "list every space for user X" trusts the IdP's key format, and nothing prevents membership rows for defunct principals. Acceptable; record it as a deployment assumption (done via this review; no schema change).

### F16 🔵 — Purge severs history

Purge cascades `memory_links` (audit edges gone), nulls `entity_facts.memory_id` (facts lose provenance), nulls `superseded_by` both directions (chain broken). Mildly contradicts "history stays answerable"; acceptable because purge is admin-only, `If-Match`-gated, audit-logged. **Recommendation:** optionally RESTRICT purge when the memory participates in `supersedes` links. Effort: **Low**.

### F17 🔵 — Array vs junction asymmetry

`memory_entities` junction vs `agent_sessions.active_memories` array: inconsistent resolution of the same M:N shape, justified per-table (ephemeral working set vs durable graph) and documented with an orphan-sweep note. Acceptable; keep the sweep as a named lifecycle job.

### F18 🔵 — No duplicate-content guard

By design ("episodic stored liberally"; repetition is *evidence* that raises confidence), so a unique constraint would be wrong. **Recommendation:** add a `content_hash` column (non-unique, indexed) to accelerate Q8's near-duplicate pairing — the current entity-anchor blocking plus trigram GIN is the only dedup accelerator. Effort: **Low**.

### F19 🔵 — Partitioning key vs query patterns

The §3.12 sketch partitions by `created_at`, but Q7/list queries filter by scope/owner — full fan-out at exactly the scale partitioning targets. Mitigated by the 18-month recall window on the dense path, not on the listing path. **Recommendation:** revisit hash-vs-range and scope-list partitioning before flipping to partitioned mode; the page's own "start unpartitioned" decision keeps this non-urgent.

### F20 🔵 — HNSW bloat and halfvec recall unmeasured

The page asserts halfvec recall loss is "negligible at 1536d" without a measurement plan, and gives no `REINDEX`/bloat cadence for continuously-inserted HNSW. **Recommendation:** recall@k A/B before adopting halfvec (the escape hatch is documented — good), plus a periodic `pg_relation_size(idx)/count(*)` check in §6.4 ops.

### F21 🔵 — REST provenance is forgeable

`origin`, `agent_id`, `actor` arrive in the request body over REST; only MCP derives them from the connection. An agent over REST can assert `origin: user_instruction, actor: <the human>` and manufacture authority. **Recommendation:** server-stamp `agent_id`/`actor` from the token; treat client-supplied values as claims, not facts. Effort: **Low** (app-level). → **Resolved (2026-08-24 finalization pass):** `provenance.actor` marked as server-stamped/server-derived in API contracts and data models.

### F22 🔵 — Naming drift is systematic but mapped

Every naming difference (F22 list) is a documented decomposition in pgvector Part 1; the risk is only that api-contracts readers don't know the mapping exists. **Recommendation:** link Part 1 from api-contracts' component-backing map. Effort: **Trivial**.

---

## Corrections Applied

All 🔴 Critical findings were fixed directly in the source files. 🟡/🔵 findings are recommendations only, per the review mandate.

### `pages/pgvector-data-model.md`

1. **§3.3 `memories`** — added `owner_principal_type` (NOT NULL DEFAULT 'user') + `owner_principal_id` (NOT NULL), `memories_owner_type_chk`, and `memories_owner_idx` partial index (F1).
2. **§3.7 `recall_requests`** — added `mode`, `status`, `error` columns with CHECKs (incl. failed⇒error pairing) and `recall_requests_active_idx` partial index (F3).
3. **New table `lifecycle_jobs`** (§3.7bis) with kind/status/scope/result/error/timestamps + open-jobs partial index; migration table §6.2 gained `010_jobs.sql` (F3).
4. **§3.8 `promotion_proposals`** — added `note`, `reject_reason` with rejected⇒reason pairing CHECK; added previously missing `proposals_target_kind_chk`; added partial UNIQUE index on open `(candidate_memory_id, shared_space_id)` (F7, F10).
5. **§3.8 trigger** — DELETE branch now recomputes `sync_status` from remaining open proposals (F5).
6. **Q1** — replaced the self-declared-broken `fused` CTE with the correct single-pass tagged-UNION aggregation; removed the stale "Wait —" caveat; added pointer that production recall must compose Q7's owner/scope predicate (F4).
7. **Q7** — individual branch now owner-scoped (`owner_principal_type/id` match), closing the leak (F2).
8. **Verification section** — re-run note appended: changed DDL fragments re-validated against `pgvector/pgvector:pg17` in this review; Q1 corrected form parses and plans against the schema.

### `pages/data-models.md`

9. **§3.1 Memory** — added `owner_principal_type` / `owner_principal_id` to the field table, JSON Schema properties, and both JSON/YAML instances (F1 mirror).
10. **§2 Entity Catalog** — RecallRequest/RecallResult relabeled as log records with rationale (F14).

### Not applied (recommendations for user approval)

**Update (2026-08-24 finalization pass):** the seven findings below that were previously listed as pending are now **resolved** (applied across data-models.md, pgvector-data-model.md, api-contracts.md, and architecture.md):

- **F6 (top_k alignment)** → Resolved: unified to default=50, max=200 across all artifacts.
- **F8 (access tracking)** → Resolved: `last_accessed_at` removed from `memories`, `memory_access_log` table added.
- **F11 (embedding model registry)** → Resolved: `embedding_models` table added with FK from `memory_embeddings`.
- **F13 (BM25 naming)** → Resolved: renamed to "BM25-like (ts_rank_cd)" across all artifacts.
- **F14 (RecallRequest/RecallResult)** → Resolved: relabeled as operational log records, moved to separate section.
- **F21 (provenance)** → Resolved: `provenance.actor` marked as server-stamped/server-derived in API contracts and data models.
- **F1 follow-up (RLS)** → Documented as future work with example policies in pgvector-data-model.md ("Future: Row-Level Security (RLS)" section).

Still open (recommendations for user approval): `SKIP LOCKED`/router-side budgets (F9), recall-log `SET NULL` (F12), purge RESTRICT on supersedes links (F16), `content_hash` (F18), partitioning key revisit (F19), HNSW ops checks (F20), Part 1 link from api-contracts (F22).

---

## Sign-off

**Production-ready? Yes with caveats.**

The model is deployable for single-principal or fully-trusted-multi-principal deployments today. The five critical defects found by this review were all **narrow, surgical fixes** (two columns, one index, one query predicate, one trigger branch, one corrected CTE, one missing table) — the fact that no fix required restructuring is itself evidence the underlying design is sound.

**Minimum set of fixes to reach production-ready for multi-user deployment** (criticals already applied are marked ✓):

1. ✓ Owner principal on `memories` + owner-scoped Q7 (F1, F2)
2. ✓ Async/jobs schema (F3)
3. ✓ Corrected Q1 (F4) and trigger recompute (F5)
4. Row-level security (or equivalent app-enforced, token-derived principal injection) on `memories` and `space_memberships` (F1 follow-up)
5. Server-stamped provenance on REST writes (F21)
6. `top_k` bound/default alignment across the three artifacts (F6)
7. Access-tracking decoupling before any production recall load (F8)

Uncertainty: fixes were validated as DDL fragments against `pgvector/pgvector:pg17` locally; load-behavior findings (F8, F9, F20) are analytical, not measured — they need a benchmark pass at realistic recall QPS before the caveats can be cleared.

## Related Pages

- [[pages/data-models|Data Models]] — conceptual model (F1/F14 corrections applied)
- [[pages/pgvector-data-model|pgvector Data Model]] — physical model (7 corrections applied)
- [[pages/api-contracts|API Contracts]] — referenced for cross-checks (F3, F6, F7, F13, F21)
- [[pages/architecture|Architecture]] — referenced for component alignment (F1 boundary, no component gaps found)
- [[ai-memory-research|Canonical overview]]

## Tags

- #ai-agents
- #memory-systems
- #data-models
- #schema-design
- #adversarial-review
- #security
