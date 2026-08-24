-- +goose Up
-- All COMMENT statements from pgvector-data-model.md Part 3.

-- §3.0
COMMENT ON DOMAIN tag_name IS 'Lowercase tag token; pattern enforced per data-models §3.1';
COMMENT ON FUNCTION ai_array_to_string(text[], text) IS
  'Immutable wrapper for use in generated tsvector columns';

-- §3.1 shared_memory_spaces
COMMENT ON TABLE shared_memory_spaces IS
  'Access-controlled shared space (SharedMemorySpace §3.5); artifacts is a manifest, not a membership list';
COMMENT ON COLUMN shared_memory_spaces.artifacts IS
  'Externalized reviewable representations; populated only when backend includes files';

-- §3.2 agent_sessions
COMMENT ON TABLE agent_sessions IS
  'Runtime binding per data-models §3.4; working tier is the context window, this table tracks its shape and budget';
COMMENT ON COLUMN agent_sessions.active_memories IS
  'Working-set memory ids in context; no FK (array) — app maintains, orphan sweep in lifecycle job';

-- §3.3 memories
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

-- §3.4 memory_links
COMMENT ON TABLE memory_links IS
  'Directed weighted edges (MemoryLink §3.2); the graph path traverses these with recursive CTEs';

-- §3.5 space_memberships
COMMENT ON TABLE space_memberships IS
  'Junction for SharedMemorySpace.participants[]; principal_id has no FK — principals are external';

-- §3.6 entities and children
COMMENT ON TABLE entities IS 'Entity registry (Entity §3.3); entity-centric anchors for retrieval';
COMMENT ON TABLE entity_facts IS
  'Temporal facts per entity (Entity.facts[]); Zep-style validity intervals [source: 03-zep]';
COMMENT ON TABLE memory_entities IS 'M:N anchor between Memory.entities[] and the registry';

-- §3.7 recall_requests / recall_results
COMMENT ON COLUMN recall_requests.status IS
  'Async recall lifecycle (review F3): queued|running|completed|failed; error jsonb required on failed';
COMMENT ON TABLE recall_requests IS
  'OPERATIONAL LOG TABLE (F14), not a core memory entity; retain 30 days, then archive/purge. Append-only recall log (RecallRequest §3.6); partitioning candidate at scale (§3.12)';
COMMENT ON TABLE recall_results IS
  'OPERATIONAL LOG TABLE (F14), not a core memory entity; same 30-day retention as recall_requests. Merged + re-ranked answer per request (RecallResult §3.7); UNIQUE(request_id) enforces 1:1';

-- §3.11b lifecycle_jobs
COMMENT ON TABLE lifecycle_jobs IS
  'Async job ledger for lifecycle/sync operations (api-contracts §1.2.5, §4.1); added by review F3';

-- §3.7b memory_access_log
COMMENT ON TABLE memory_access_log IS
  'Append-only access log (F8): can be partitioned by accessed_at; aggregate last_accessed_at = MAX(accessed_at) GROUP BY memory_id when needed';

-- §3.8 promotion_proposals
COMMENT ON TABLE promotion_proposals IS
  'Human-review promotion path (PromotionProposal §3.8); candidate FK is RESTRICT — audit survives purge';

-- §3.9 embedding_models / memory_embeddings
COMMENT ON TABLE embedding_models IS
  'Registry of embedding models; dims constrained to the two typed columns of memory_embeddings';
COMMENT ON TABLE memory_embeddings IS
  'Alternate-model vectors; exactly one typed column populated per row, matching registered dims';

-- +goose Down
COMMENT ON DOMAIN tag_name IS NULL;
COMMENT ON FUNCTION ai_array_to_string(text[], text) IS NULL;
COMMENT ON TABLE shared_memory_spaces IS NULL;
COMMENT ON COLUMN shared_memory_spaces.artifacts IS NULL;
COMMENT ON TABLE agent_sessions IS NULL;
COMMENT ON COLUMN agent_sessions.active_memories IS NULL;
COMMENT ON TABLE memories IS NULL;
COMMENT ON COLUMN memories.embedding IS NULL;
COMMENT ON COLUMN memories.validity_range IS NULL;
COMMENT ON COLUMN memories.search_tsv IS NULL;
COMMENT ON COLUMN memories.embedding_model IS NULL;
COMMENT ON COLUMN memories.source_ref IS NULL;
COMMENT ON COLUMN memories.deleted_at IS NULL;
COMMENT ON TABLE memory_links IS NULL;
COMMENT ON TABLE space_memberships IS NULL;
COMMENT ON TABLE entities IS NULL;
COMMENT ON TABLE entity_facts IS NULL;
COMMENT ON TABLE memory_entities IS NULL;
COMMENT ON COLUMN recall_requests.status IS NULL;
COMMENT ON TABLE recall_requests IS NULL;
COMMENT ON TABLE recall_results IS NULL;
COMMENT ON TABLE lifecycle_jobs IS NULL;
COMMENT ON TABLE memory_access_log IS NULL;
COMMENT ON TABLE promotion_proposals IS NULL;
COMMENT ON TABLE embedding_models IS NULL;
COMMENT ON TABLE memory_embeddings IS NULL;
