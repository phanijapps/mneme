# Architecture — AI Agent Memory & Recall

A three-layer architecture (conceptual, logical, physical) for AI agent memory and recall, derived from the synthesized research in [[pages/memory-taxonomy|Memory Taxonomy]] and [[pages/retrieval-and-recall|Retrieval and Recall]], grounded in 10 sources covering Letta, Mem0, Zep, A-MEM, CoALA, hybrid retrieval, Claude Code, Codex/Copilot, Cursor, and Spec Driven Development.

---

## 1. Conceptual Architecture

The conceptual layer defines *what* the memory system does: the lifecycle every memory follows, which memory types are active at which workflow stage, what triggers recall, and where individual memory ends and team memory begins.

### 1.1 Memory Lifecycle

Every memory moves through five stages. Each stage has grounding in the source research:

```
 ┌────────┐   ┌───────┐   ┌───────┐   ┌──────────┐   ┌────────────────────┐
 │ ENCODE │──▶│ STORE │──▶│ INDEX │──▶│ RETRIEVE │──▶│ DECAY / CONSOLIDATE│
 └────────┘   └───────┘   └───────┘   └──────────┘   └────────────────────┘
      ▲                                                        │
      └────────────────── re-encoded as consolidated memory ◀──┘
```

| Stage | What happens | Evidence |
|---|---|---|
| **Encode** | Extract facts, events, and skills from interaction; classify by memory type (episodic vs. semantic); attach provenance, entities, and timestamps. | Mem0 extracts facts during conversations and indexes by user/session/agent identifiers [source: 02-mem0]; Zep constructs a context graph automatically from interactions [source: 03-zep]; A-MEM assigns tags, category, timestamp at `add_note()` time [source: 04-a-mem]. |
| **Store** | Persist into the tier matching the memory type — in-context core, episodic recall store, or long-term semantic/procedural archive. | Letta's three tiers: core (prompt-resident) / recall (conversation archive) / archival (deep long-term) [source: 01-letta-memgpt]. |
| **Index** | Build all retrieval structures: embeddings, keyword (BM25) terms, graph edges, temporal validity ranges. | Four-way indexing: semantic, BM25, graph link signals, temporal [source: 06-hybrid-retrieval]; Mem0 hybrid semantic+keyword+entity [source: 02-mem0]. |
| **Retrieve** | Match a recall trigger to a retrieval strategy, gather candidates, re-rank, and inject into context. | Cross-encoder re-ranking over top 20–50 candidates is standard [source: 06-hybrid-retrieval]; session-start injection before the model responds [source: 02-mem0]. |
| **Decay / Consolidate** | Expire stale entries (TTL), supersede with versioned updates, and compact dense history into summaries. | TTL expiration and versioning to avoid full re-indexing [source: 06-hybrid-retrieval]; Letta replaces evicted messages with recursive summaries — "nothing is ever truly lost" [source: 01-letta-memgpt]; Zep temporal validity ranges supersede rather than delete [source: 03-zep]. |

**Design decision:** decay is *supersession-first, deletion-second*. Zep's temporal validity pattern keeps history queryable ("Emily preferred jogging until 2024-11-14") rather than erasing it [source: 03-zep]. Deletion is reserved for explicit TTL expiry and user-forgetting requests.

### 1.2 Taxonomy Mapped to Agent Workflow Stages

The CoALA taxonomy [source: 05-coala] maps onto the stages of an agent's working cycle rather than being a static classification:

```
 Agent workflow stage          Active memory types              CoALA tier
 ─────────────────────         ───────────────────────          ──────────
 Session start                 Procedural (rules/instructions)  loaded, becomes working
 │   Context bootstrap         Semantic (facts)                 → working
 ▼
 Task comprehension            Working memory (query, goals)    in-context
 │                             Episodic (similar past tasks)    retrieved
 ▼
 Execution / tool use          Procedural (how to use tools)    in-context or retrieved
 │   New observations          → ENCODE: episodic candidates
 ▼
 Reflection / decision         Semantic (entity state)          retrieved
 │                             Episodic (precedents)            retrieved
 ▼
 Session end                   Consolidation: episodic → semantic summaries
 │                             Eviction: working → recursive summary [01-letta-memgpt]
 ▼
 Cross-session / team handoff  Procedural + semantic → shared store (specs, rules)
```

- **Session start** loads procedural and semantic memory into working memory — this is Claude Code's hierarchy injection [source: 07-claude-code], Cursor's recall tool [source: 09-cursor], and Mem0's session-start retrieval [source: 02-mem0].
- **Execution** is where encoding happens opportunistically: each tool result and user correction is an episodic candidate.
- **Session end** is the consolidation point: episodic detail is compressed into semantic facts (Letta's recursive summarization [source: 01-letta-memgpt]; Mem0's interaction summaries [source: 02-mem0]). SDD practice formalizes this as the session-end spec update [source: 10-sdd].
- **Working memory is bounded and shared** — all other tiers exist to manage that one constraint [source: 05-coala, 01-letta-memgpt].

### 1.3 Recall Triggers

Four trigger classes determine *when* retrieval fires and *which* strategy serves it:

| Trigger | Fires when | Primary retrieval strategy | Evidence |
|---|---|---|---|
| **Task-context** | Agent begins a task matching prior task signatures | Semantic similarity over task-embedded memories; multi-hop graph expansion for dependencies | Mem0 session-start semantic+keyword+entity retrieval [source: 02-mem0]; two-hop graph traversal then vector ranking [source: 06-hybrid-retrieval] |
| **User-query** | A user message arrives | Hybrid: dense vector for simple queries, graph pipeline for event sequencing / multi-hop | Mem0 dual pipeline (dense vs. Mem0g) [source: 02-mem0] |
| **Temporal** | Time-based relevance: recency, scheduled validity, anniversaries of events | Temporal search with spreading activation over event sequences; validity-range filtering | Hindsight temporal search [source: 06-hybrid-retrieval]; Zep validity ranges [source: 03-zep] |
| **Associative** | An entity mentioned in context co-occurs with stored entities | Graph traversal from entry-point entities located by vector search | FalkorDB pattern: vector finds entry points, graph expands context [source: 06-hybrid-retrieval]; A-MEM link generation between notes [source: 04-a-mem] |

**Design decision:** a single recall event may fire multiple triggers; the Recall Router (§2.2) merges their candidate sets before re-ranking, mirroring the hybrid-retrieval consensus [source: 06-hybrid-retrieval, 02-mem0].

### 1.4 Team vs. Individual Memory Boundaries

| Boundary | Individual memory | Team memory |
|---|---|---|
| **Scope** | Per-user, per-agent, per-session identifiers [source: 02-mem0] | Per-repository, per-project, per-organization [source: 10-sdd] |
| **Write authority** | The agent itself (auto memory, episodic capture) | Humans via review + agents via session-end spec update [source: 10-sdd, 07-claude-code] |
| **Mutability** | High — decayed, consolidated, superseded freely | Low — version-controlled; changes are commits, not overwrites [source: 10-sdd] |
| **Content** | Preferences, interaction history, entity state with temporal validity [source: 03-zep] | Design decisions, conventions, plans, data models — the "memory bank" of general context [source: 10-sdd] |
| **Ephemerality fix** | survives sessions via the store | survives sessions *and* team members *and* tool switches [source: 10-sdd] |

**Ownership rule (design decision):** individual memory is *agent-written by default*; team memory is *human-approved by default*. This mirrors Cursor's explicitness-over-opacity philosophy [source: 09-cursor] and Claude Code's split between user-authored CLAUDE.md and agent-authored auto memory [source: 07-claude-code]. The promotion path is: individual episodic → agent-consolidated semantic → proposed diff → human-merged team memory (spec/rules).

**Shared-memory mechanism:** teams do not share a live database; they share a *version-controlled artifact* (specs, rules files) that every agent loads at session start. Augment Cosmos demonstrates the propagation property: when shared context changes, updates reach all active agents through the shared filesystem [source: 10-sdd]. The SDD insight that matters structurally: version the specs, not just the code they produced [source: 10-sdd].

### 1.5 Application to Real Tools

| Tool | Conceptual fit |
|---|---|
| **Claude Code** | Working = context window; procedural = CLAUDE.md + `.claude/rules/*.md` (loaded in fixed hierarchy at session start, concatenated not overridden); episodic/semantic = auto memory + MEMORY.md scaffolded by standing instruction. Instruction budget: ~150–200 reliable instruction slots, ~50 consumed by the system prompt [source: 07-claude-code]. |
| **Codex (OpenAI)** | Minimal native memory — session history is client-side only (local rollout files); context resets between sessions. Persistent memory is an *integration concern*, solved via MCP memory servers [source: 08-codex-copilot]. |
| **Cursor** | Procedural = `.cursor/rules/*.md` (legacy `.cursorrules` deprecated; `AGENTS.md` at root also honored); no default cross-session memory; Memory Bank community pattern adds episodic structure; Recall tool injects memories at session start [source: 09-cursor]. |
| **Spec Driven Development** | Team semantic + procedural memory: `specs/[feature]/` with plan/research/data-model/quickstart persisting design decisions across sessions, tools, and team members [source: 10-sdd]. |

---

## 2. Logical Architecture

The logical layer defines the components, their contracts, and the data flow between them. Components are implementation-agnostic; §3 binds them to physical backends.

### 2.1 Component Overview

```
                         ┌─────────────────────────────────────────────┐
                         │                AGENT RUNTIME                │
                         │  (Claude Code / Codex / Cursor / Letta...)  │
                         └──────┬───────────────────────────▲──────────┘
                        writes  │                           │ context
                    (observations,                    (re-ranked,
                     corrections)                       injected memories)
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
                                   │ MEMORY LIFECYCLE MGR│◀────┘ (decay events,
                                   └──────────┬──────────┘      consolidation output)
                                              │ promoted/synced memory
                                   ┌──────────▼──────────┐
                                   │ TEAM SYNC LAYER     │◀──▶ git / shared filesystem
                                   └─────────────────────┘        (specs, rules)
```

### 2.2 Components and Interface Contracts

#### Memory Encoder
Converts raw interaction artifacts into typed memory records.
- **Input:** `observation stream` — messages, tool results, diffs, corrections; plus session metadata (agent_id, user_id, session_id).
- **Output:** `MemoryRecord[]` — each with `type` (episodic|semantic|procedural), `content`, `entities[]`, `timestamp`, `provenance`, `tags[]`, `confidence`.
- **Grounding:** Mem0 extracts facts during conversation and indexes by user/session/agent identifiers [source: 02-mem0]; A-MEM's `add_note(content, tags, category, timestamp)` is this exact contract [source: 04-a-mem]; Zep's automatic context-graph construction is encoder behavior [source: 03-zep].
- **Encoding policy (decision):** episodic records are stored liberally; promotion to semantic requires either repetition across sessions or explicit consolidation — preventing single-interaction noise from becoming durable "facts."

#### Memory Store
The system of record for all tiers.
- **Input:** `MemoryRecord[]` (from Encoder), queries (from Retrieval Engine), mutations (from Lifecycle Manager).
- **Output:** persisted records; query result sets.
- **Tier contract:**
  - *Working tier* — not persisted here; lives in the agent runtime's context window [source: 01-letta-memgpt, 05-coala].
  - *Episodic tier* — full-fidelity interaction history, append-mostly [source: 01-letta-memgpt].
  - *Semantic tier* — entity-centric facts with temporal validity ranges [source: 03-zep, 02-mem0].
  - *Procedural tier* — rules, skills, tool-usage patterns; in agent-tool deployments this tier is *files the human owns* (CLAUDE.md, `.cursor/rules/`) rather than agent-owned records [source: 07-claude-code, 09-cursor].

#### Index Manager
Maintains every retrieval structure over the store.
- **Input:** store change events (write/update/supersede/expire).
- **Output:** updated indexes — vector embeddings, BM25 terms, graph edges (`MemoryLink` records), temporal validity intervals.
- **Grounding:** four index families correspond to the four-way hybrid [source: 06-hybrid-retrieval]; FalkorDB pattern of storing embeddings as node properties allows one index to serve both vector and graph access [source: 06-hybrid-retrieval].
- **Update policy (decision):** TTL expiration and versioning handle updates without full re-indexing [source: 06-hybrid-retrieval]; supersession closes the old temporal interval and opens a new one rather than mutating history [source: 03-zep].

#### Retrieval Engine
Executes candidate retrieval for a single strategy or blend.
- **Input:** `RetrievalQuery` — strategy hint(s), embedded query, entities, time bounds, top-k.
- **Output:** `MemoryCandidate[]` — record + score + provenance, pre-re-rank, typically top 20–50.
- **Strategies (from research):** dense vector search; BM25 keyword; graph traversal (entry points by vector, expansion by edges, e.g. two-hop); temporal search with spreading activation [source: 06-hybrid-retrieval]; entity-centric query decomposition [source: 02-mem0]. **Recall Router budget: `top_k` default 50, max 200, configurable per recall request** (F6) — 50 covers typical recall depth; 200 accommodates the ~150–200 instruction-slot upper bound of agent context budgets.
- **Re-ranking:** cross-encoder over the candidate set is a mandatory final step in production patterns [source: 06-hybrid-retrieval]. Engine returns re-ranked top-k sized to the context budget.

#### Recall Router
Decides *when* and *how much* to recall — the policy brain.
- **Input:** runtime events (session start, user message, task start, entity mention, timer) + current context budget.
- **Output:** `RecallRequest[]` to the Retrieval Engine; final `context injection plan` (what enters working memory, in what order, within budget).
- **Grounding:** session-start retrieval and pre-response injection [source: 02-mem0]; Claude Code's fixed injection order (enterprise → global → project → rules → conditional → first message) is a routing policy [source: 07-claude-code]; Cursor's recall-tool-at-session-start is the same trigger [source: 09-cursor].
- **Budget policy (decision):** the router enforces an instruction budget, not just a token budget — ~150–200 reliable instruction slots of which ~50 are system-consumed [source: 07-claude-code]. Injections are counted against that budget before entering working memory.

#### Memory Lifecycle Manager
Runs decay, consolidation, and eviction.
- **Input:** timers, session-end events, capacity signals from the runtime.
- **Output:** `MutationOps` — expire (TTL), supersede (new validity interval), consolidate (episodic set → semantic summary), compact (conversation prefix → recursive summary).
- **Grounding:** TTL + versioning [source: 06-hybrid-retrieval]; Letta's evict-and-summarize with nothing truly lost [source: 01-letta-memgpt]; Zep supersession semantics [source: 03-zep]; session-end spec update as the team-facing consolidation event [source: 10-sdd].
- **Consolidation contract (decision):** consolidation produces *candidate* semantic records; those promoted to team memory leave as diffs for human review (§1.4 ownership rule).

#### Shared Memory Sync Layer
Binds individual memory to the shared, version-controlled artifact space.
- **Input:** promoted memories (from Lifecycle Manager), external changes (git pulls on specs/rules).
- **Output:** commits/PRs to the shared memory artifacts; change notifications that invalidate stale local caches in active agents.
- **Grounding:** Augment Cosmos — shared filesystem propagates updates to all active agents [source: 10-sdd]; spec directory structure (`specs/[feature]/plan.md`, `research.md`, `data-model.md`, `quickstart.md`) [source: 10-sdd]; version the specs, not just generated code [source: 10-sdd].

### 2.3 Data Flows

**Flow A — Encode (during session):**
```
Agent runtime ──observation──▶ Memory Encoder
  Encoder: classify type, extract entities, tag, timestamp
  ──MemoryRecord[]──▶ Memory Store (persist)
  Store ──change event──▶ Index Manager
  Index Manager: embed, term-index, link, interval-index
```

**Flow B — Recall (triggered):**
```
Runtime event ──▶ Recall Router
  Router: classify trigger (task-context | user-query | temporal | associative)
        : check instruction/token budget
  ──RecallRequest[]──▶ Retrieval Engine
  Engine: parallel strategies (vector ∥ BM25 ∥ graph ∥ temporal)
        : merge candidate sets (top 20–50)
        : cross-encoder re-rank
  ──re-ranked top-k──▶ Router ──context injection──▶ Agent runtime (working memory)
```

**Flow C — Session end (consolidate + handoff):**
```
Session-end event ──▶ Memory Lifecycle Manager
  Manager: compact conversation (recursive summary)
         : consolidate episodic set → semantic candidates
         : expire TTL'd records, supersede invalidated facts
  ──MutationOps──▶ Memory Store ──▶ Index Manager (re-index deltas)
  Manager: ──promotion candidates──▶ Shared Memory Sync Layer
  Sync Layer: draft diff against specs/rules ──▶ human review ──▶ commit
  git change ──▶ notification ──▶ invalidate caches in other active agents
```

**Flow D — Session start (bootstrap):**
```
Session start ──▶ Recall Router (fixed-order policy)
  1. procedural: enterprise/user memory, global CLAUDE.md     [07-claude-code]
  2. procedural: project CLAUDE.md / .cursor/rules/           [07, 09]
  3. conditional rules (if applicable)                        [07]
  4. semantic: entity state for this user/project             [02-mem0]
  5. episodic: similar-past-task recall                       [02-mem0]
  ──ordered injection──▶ Agent runtime context window
```

### 2.4 Retrieval Strategy → Component Mapping

| Research strategy | Owning component | Mechanism |
|---|---|---|
| Semantic vector search [02, 06] | Retrieval Engine (dense path) | embedding similarity over Index Manager's vector structures |
| BM25 keyword [06] | Retrieval Engine (sparse path) | lexical terms indexed by Index Manager |
| Graph traversal / multi-hop [03, 06] | Retrieval Engine (graph path) | vector entry points → edge expansion (≤2 hops) → vector ranking of associated chunks |
| Temporal search [03, 06] | Retrieval Engine (temporal path) | validity-interval filtering + spreading activation over event order |
| Entity-centric decomposition [02] | Recall Router → Engine | router extracts entities from trigger; engine runs entity-boosted retrieval |
| Cross-encoder re-ranking [06] | Retrieval Engine (final stage) | mandatory post-merge re-rank before injection |
| Session-start injection [02, 07, 09] | Recall Router | fixed-order bootstrap policy (Flow D) |
| Dense vs. graph dual pipeline [02] | Recall Router | router routes simple queries to dense path, sequencing/multi-hop queries to graph path *(single-source: Mem0 paper)* |

---

## 3. Physical Architecture

The physical layer binds components to concrete storage backends, deployment shapes, and real-tool integration points.

### 3.1 Storage Backends

| Backend | Serves | Format / engine | Evidence |
|---|---|---|---|
| **File-based (markdown/YAML)** | Procedural tier; team semantic memory; spec artifacts | CLAUDE.md, MEMORY.md, `.cursor/rules/*.md`, `AGENTS.md`, `specs/[feature]/*.md` under git | [source: 07-claude-code, 09-cursor, 10-sdd] |
| **Vector DB** | Semantic tier; dense retrieval path; embeddings for episodic chunks | ChromaDB (A-MEM's choice); vector indexes over Mem0's stores | [source: 04-a-mem, 02-mem0, 06-hybrid-retrieval] |
| **Graph DB** | Episodic event sequences; entity relationships; temporal validity; multi-hop | Graphiti engine (Zep); FalkorDB with embeddings-as-node-properties (co-located vector+graph) | [source: 03-zep, 06-hybrid-retrieval] |
| **Relational / metadata store** | Memory record metadata, provenance, timestamps, link weights, sync state | conventional RDBMS alongside the specialized stores; Letta uses conventional DBs for recall memory + vector DB for archival | [source: 01-letta-memgpt] |

**Decision — hybrid co-location where possible:** prefer a store that holds vectors *inside* the graph (FalkorDB pattern) to eliminate the network hop between vector and graph operations [source: 06-hybrid-retrieval]. Where interoperation matters more, Zep's Graphiti demonstrates the dedicated-temporal-graph route [source: 03-zep].

**Decision — files are the procedural/team tier, always:** even in fully database-backed deployments, procedural and team memory should remain human-readable files under version control, because review-ability is the property that makes team memory trustworthy [source: 10-sdd, 09-cursor].

### 3.2 Deployment Patterns

**Pattern L — Local (single agent, single user).**
```
 ┌─────────────────────────────┐
 │ Agent runtime (Claude Code, │
 │ Codex CLI, Cursor)          │
 │  ├─ context window (working)│
 │  ├─ memory files (proc.)    │   ./CLAUDE.md, ./MEMORY.md,
 │  └─ MCP client ─────────┐   │   ./.cursor/rules/*.md, ./AGENTS.md
 └─────────────────────────┼───┘
                           ▼
                 local memory server (optional)
                 vector+graph store, e.g. ChromaDB/FalkorDB
```
Codex CLI is the archetype: no server-side persistence at all, client-side rollout files only, and memory added via MCP [source: 08-codex-copilot]. Claude Code without auto-memory is the file-only variant [source: 07-claude-code]. Cursor is file-only by default; Memory Bank/Recall are opt-in structure [source: 09-cursor].

**Pattern S — Shared (team-level, spec-driven).**
```
 ┌────────┐  ┌────────┐  ┌────────┐        git repository (shared filesystem)
 │Agent A │  │Agent B │  │Agent C │            │
 │(Claude)│  │(Codex) │  │(Cursor)│            ├─ specs/[feature]/{plan,research,
 └───┬────┘  └───┬────┘  └───┬────┘            │   data-model,quickstart}.md  [10-sdd]
     │           │           │                 ├─ .claude/rules/*.md           [07]
     └───────────┴───────────┴────────▶ load ──├─ .cursor/rules/*.md           [09]
                                              └─ AGENTS.md                    [09]
```
All agents bootstrap from the same version-controlled artifacts; a change to shared context propagates to every active agent through the shared filesystem (Augment Cosmos property) [source: 10-sdd]. Tool-specific rule files can be generated from or kept aligned with a canonical source (e.g. AGENTS.md) so memory is tool-portable [source: 09-cursor, 10-sdd].

**Pattern H — Hybrid (local stores + shared service).**
```
 Agent A ─┐                          ┌─ shared memory service (per-tenant,
 Agent B ─┼─▶ memory API/service ───▶│  per-user layer; sub-200ms target [03-zep])
 Agent C ─┘        │                 └─ episodic chats + semantic entity graph
                   │                      + group subgraphs (Graphiti) [03-zep]
                   └──▶ team memory: still git-versioned specs/rules [10-sdd]
```
Individual semantic/episodic memory lives in the service (Mem0 and Zep's deployment shape) [source: 02-mem0, 03-zep]; team memory stays in files even in this pattern — the service never becomes the system of record for team decisions [source: 10-sdd].

### 3.3 Concrete Integration Points

**Claude Code** [source: 07-claude-code]
- Files: `./CLAUDE.md` (project), `~/.claude/CLAUDE.md` (global), enterprise-managed memory, `.claude/rules/*.md` (project rules), conditional rules.
- Loading: all levels concatenated into context at session start — not precedence-overridden. Injection order: enterprise/user → global → project → project rules → conditional → first user message.
- Inspection: `/memory` command lists loaded memory files.
- Auto-memory: agent-written notes; a standing CLAUDE.md instruction can scaffold a `MEMORY.md` per project for cross-session persistence.
- Budget: ~150–200 reliable instruction slots total; ~50 used by Claude Code's own system prompt → keep total rule/instruction content within ~100–150 slots.

**Codex (OpenAI)** [source: 08-codex-copilot]
- Native: client-side session state only (local rollout files); no server-side conversation retention; context resets between sessions.
- Persistent memory via MCP: configure an MCP memory server (e.g. a Mem0-class or custom server) so project context, decisions, and patterns persist across sessions; agents auto-store context to a memory workspace during sessions via third-party MCP integrations.

**Cursor** [source: 09-cursor]
- Files: `.cursor/rules/*.md` (or `.mdc`) — current format; `AGENTS.md` at project root; legacy root `.cursorrules` is deprecated.
- Loading: rules act as persistent system prompt, prepended to every conversation.
- Cross-session: no default; add Memory Bank structure (markdown context files) and/or a recall instruction that retrieves stored memories at session start with direct context injection; Notepads for persistent in-editor context documents.

**GitHub Copilot (adjacent)** [source: 08-codex-copilot]
- Rolling-out native persistent Memory for repository-level context and personal coding preferences; VS Code agent memory documentation covers cross-session preference retention.

**Spec Driven Development** [source: 10-sdd]
- Directory contract: `specs/[###-feature]/` containing `plan.md`, `research.md`, `data-model.md`, `quickstart.md` (Spec Kit layout).
- Sharing mechanism: git version control; specs are human+agent readable markdown; session-end spec update is the mandated write discipline.
- The two layers that must be shared, visible, and version-controlled: the Situational Layer (memory) and the Operational Layer (rules).

### 3.4 Spec Driven Development as Team Memory (synthesis)

SDD is the physical realization of the Shared Memory Sync Layer: specs are the team's semantic memory (design decisions, rationale), rules files are the team's procedural memory (how agents must operate), and git is the lifecycle manager — commits are consolidation events, history is the episodic record, and reverts are the decay mechanism. SDD is one example of shared memory sync, not the only pattern — any versioned, human+agent readable artifact space (shared filesystems, wiki/ward pages, database-backed team stores) can fill this role. This inverts the default failure mode: teams routinely version generated code while letting the specs that produced it drift, when the specs are the memory that actually compounds [source: 10-sdd].

---

## 4. Cross-Cutting Design Decisions

1. **Tiering is non-negotiable** — every deployment has working/episodic/semantic separation, even if the episodic tier is only git history [source: 01-letta-memgpt, 05-coala].
2. **Hybrid retrieval over any single strategy** — vector alone misses exact terms; BM25 alone misses concepts; graph alone needs entry points; temporal alone misses similarity. Merge then re-rank [source: 06-hybrid-retrieval, 02-mem0].
3. **Supersede, don't delete** — temporal validity ranges keep history answerable [source: 03-zep].
4. **Nothing is truly lost** — eviction produces summaries, not destruction [source: 01-letta-memgpt].
5. **Explicitness for team memory, autonomy for individual memory** — files+review for shared; automatic capture for personal [source: 07-claude-code, 09-cursor, 10-sdd].
6. **Budget by instruction slots as well as tokens** — usable instruction capacity is ~100–150 after system-prompt overhead [source: 07-claude-code].
7. **Files remain the interop format** — markdown rules/specs are the only memory format all three major tools read natively [source: 07-claude-code, 08-codex-copilot, 09-cursor, 10-sdd].

## Uncertainty Notes

- Dual-pipeline routing (dense vs. graph by query type) is a single-source Mem0 design [source: 02-mem0]; treat as one viable policy, not a validated standard.
- Sub-200ms retrieval is Zep's claim only [source: 03-zep]; no comparable latency data exists for other systems in the source set.
- Four-way hybrid breadth (Hindsight) reflects one system; production consensus is firmly behind 2–3 way blends plus re-ranking [source: 06-hybrid-retrieval].
- CoALA extended types (retrieval, parametric, prospective, compressed, hierarchical) are taxonomy proposals, not observed production tiers [source: 05-coala]; this architecture adopts only compressed (as consolidation) and hierarchical (as tiering), where production evidence exists.
- Instruction-slot budgeting (~150–200) is Claude-specific guidance [source: 07-claude-code]; the *principle* of bounded instruction capacity generalizes, the number may not.

## Related Pages

- [[pages/memory-taxonomy|Memory Taxonomy]] — the type taxonomy this architecture instantiates
- [[pages/retrieval-and-recall|Retrieval and Recall]] — the retrieval evidence behind the Retrieval Engine and Recall Router
- [[ai-memory-research|Canonical overview]]

## Tags

- #ai-agents
- #memory-systems
- #architecture
- #retrieval
- #context-management
- #spec-driven-development
