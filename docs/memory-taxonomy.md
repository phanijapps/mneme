# Memory Taxonomy

A synthesized taxonomy of memory types in AI agent systems, derived from multiple frameworks and production systems.

## Core Taxonomy (CoALA Framework)

The dominant memory taxonomy for LLM agents comes from the **CoALA framework** (Zhang et al., Princeton, 2023) [source: 05-coala]. It defines four primary memory types:

| Type | Description | Example Systems |
|------|-------------|-----------------|
| **Working Memory** | Active information held in-context during a session; bounded by context window size | All systems — context window is the shared constraint |
| **Episodic Memory** | Instance-specific, context-preserved records of past events or interactions | Mem0 summaries, Zep conversation graphs, Letta recall tier |
| **Semantic Memory** | Abstracted, generalized facts and knowledge stripped of specific context | Mem0 entity store, Zep knowledge graph, Letta archival tier |
| **Procedural Memory** | Skills, code patterns, and learned behaviors (how to use a tool) | CoALA framework, Cursor .cursorrules, Claude CODE.md |

## Extended Memory Types

Beyond the CoALA four, several systems introduce additional categories [source: 05-coala]:

- **Retrieval Memory** — cached results from prior RAG/retrieval operations [source: 05-coala]
- **Parametric Memory** — knowledge encoded in the LLM's pre-training weights [source: 05-coala]
- **Prospective Memory** — planning and future-oriented recall [source: 05-coala]
- **Compressed Memory** — summarization/compaction of dense interactions [source: 05-coala]
- **Hierarchical Memory** — organization at multiple granularity levels (sentence, entity, chunk) [source: 05-coala, 04-a-mem]

## Tiered Memory Architectures

Letta (formerly MemGPT) implements the most explicit tiered model [source: 01-letta]:

1. **Core Memory** — in-context (prompt-resident); persona, user state, goals. Editable by the agent. No retrieval needed.
2. **Recall Memory** — archived conversation history outside the prompt; retrieved on demand.
3. **Archival Memory** — deep long-term storage; agent searches and retrieves as needed.

This tiering maps directly to CoALA: core → working memory; recall → episodic; archival → semantic [source: 01-letta, 05-coala].

Mem0 similarly distinguishes episodic (interaction summaries) from semantic (durable facts, preferences) [source: 02-mem0].

Zep's Graphiti engine powers a three-layer memory: episodic chats, semantic entities, and group-level subgraphs [source: 03-zep].

## Entity and Temporal Memory

A distinctive pattern in production systems is **entity-centric memory** with temporal validity:

- Zep tracks people, entities, and relationships with **temporal validity ranges** (e.g., "Emily prefers cycling to jogging (Valid: 2024-11-14 — present)") [source: 03-zep]
- Mem0 extracts and indexes facts by user, session, and agent identifiers [source: 02-mem0]
- A-MEM uses Zettelkasten-style structured notes with tags, categories, and timestamps [source: 04-a-mem]

## Memory in Agent-Tool Systems

Agent coding tools use a different memory vocabulary:

- **Claude Code**: CLAUDE.md files + auto-generated MEMORY.md as persistent cross-session memory [source: 07-claude-code]
- **Cursor**: .cursor/rules/*.md as persistent system prompts; Memory Bank community pattern for cross-session context [source: 09-cursor]
- **GitHub Copilot**: rolling-out persistent memory for repository-level context and preferences [source: 08-codex]
- **Spec Driven Development**: specs as shared design-memory across team members [source: 10-sdd]

## Uncertainty Notes

- The extended memory types (retrieval, parametric, prospective, compressed, hierarchical) are **single-source** claims from [source: 05-coala] and should be treated as proposed rather than broadly adopted.
- A-MEM's Zettelkasten approach [source: 04-a-mem] is one system's design; generalizability is unconfirmed across other frameworks.
- Temporal validity ranges [source: 03-zep] are a specific Zep feature; other systems do not clearly implement this pattern.
