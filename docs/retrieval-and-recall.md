# Retrieval and Recall Mechanisms

How AI agent memory systems retrieve relevant information at the right time.

## Hybrid Search Strategies

Production memory systems combine multiple retrieval modalities rather than relying on any single approach [source: 06-hybrid-retrieval, 02-mem0]:

1. **Semantic (vector) search** — embedding-based similarity over stored memory chunks. The dominant baseline.
2. **BM25 keyword search** — classic full-text retrieval for exact terms and phrases; complements semantic search's conceptual matches. *(In the pgvector implementation this is realized as BM25-like (`ts_rank_cd`) cover-density ranking, not true BM25 with IDF weighting — sufficient for most cases; evaluate ParadeDB `pg_search` if lexical quality disappoints (F13).)*
3. **Graph traversal** — expanding through precomputed link signals: entity co-occurrence, semantic kNN, causal chains.
4. **Temporal search** — time-bounded recall with spreading activation over event sequences.

The Hindsight memory system implements all four as a four-way hybrid [source: 06-hybrid-retrieval].

## Dual-Pipeline Retrieval (Mem0)

Mem0's arXiv paper (2504.19413) describes a dual-approach strategy [source: 02-mem0]:

- **Dense pipeline** — fast retrieval for straightforward queries; minimizes token usage.
- **Graph pipeline (Mem0g)** — structured graph representations for complex event sequencing and multi-hop reasoning.
- **Entity-centric method** — first identifies key entities within a query, then leverages semantic similarity to find related memories.

## Vector-Graph Hybrid Architecture

FalkorDB and similar systems (production-grade) co-locate vector and graph operations [source: 06-hybrid-retrieval]:

- Use vector search to identify relevant entry-point entities, then expand context through graph traversal.
- Store embeddings as node properties in the graph database, avoiding a separate vector index.
- Eliminates the network hop between separate vector and graph stores.

## Re-ranking

After initial retrieval, most systems apply **re-ranking** with a cross-encoder model over the top 20–50 candidates [source: 06-hybrid-retrieval]. This is treated as a standard production pattern.

## Session-Start Retrieval

At the start of a new session, Mem0 retrieves relevant memories using semantic similarity, keyword matching, and entity matching, then injects them into the context window before the model responds [source: 02-mem0].

Claude Code auto-loads its memory hierarchy at session start: Enterprise/User memory → Global CLAUDE.md → Project CLAUDE.md → project rules → conditional rules [source: 07-claude-code].

Cursor's **Recall tool** retrieves stored memories automatically at the start of every session, with context injected directly into the conversation [source: 09-cursor].

## Context Window Management

Context windows are filled with top-k retrieved items, scored by similarity or path metrics [source: 06-hybrid-retrieval]. Strategies for managing freshness and size:

- **TTL expiration** and **versioning** to handle updates without full re-indexing [source: 06-hybrid-retrieval].
- Claude Code can reliably follow ~150–200 distinct instructions; Claude Code's own system prompt consumes ~50 slots, leaving ~100–150 usable instruction slots [source: 07-claude-code].
- Letta handles context overflow via **recursive summarization** of evicted messages — nothing is ever truly lost [source: 01-letta].

## Multi-Hop Reasoning

Graph traversal excels at multi-hop reasoning: identifying all entities within two hops of a target node, then ranking associated text chunks via vector search [source: 06-hybrid-retrieval]. Zep's Graphiti engine is designed specifically for this pattern [source: 03-zep].

## Performance Targets

- Zep achieves **sub-200ms retrieval** with SOC 2 compliance [source: 03-zep]. This is the clearest performance benchmark in the source material.

## Uncertainty Notes

- The four-way hybrid (Hindsight) [source: 06-hybrid-retrieval] is attributed to one system; adoption breadth across the industry is unclear.
- Mem0's dual-pipeline distinction (dense vs. graph) is **single-source** [source: 02-mem0] — not yet confirmed by independent implementations.
- Sub-200ms retrieval [source: 03-zep] is a single-system claim; comparable latency data from other frameworks is not available in the source material.
