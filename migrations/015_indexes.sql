-- +goose Up
-- All indexes from pgvector-data-model.md Part 3, grouped by table.

-- §3.1 shared_memory_spaces
CREATE INDEX spaces_owner_idx    ON shared_memory_spaces (owner_type, owner_id);
CREATE INDEX spaces_attention_idx ON shared_memory_spaces (sync_status)
  WHERE sync_status <> 'in_sync';

-- §3.2 agent_sessions
CREATE INDEX agent_sessions_user_idx
  ON agent_sessions (user_id, created_at DESC);
CREATE INDEX agent_sessions_active_idx
  ON agent_sessions (created_at DESC)
  WHERE ended_at IS NULL;
CREATE INDEX agent_sessions_active_memories_idx
  ON agent_sessions USING gin (active_memories);

-- §3.3 memories
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

-- §3.4 memory_links
CREATE INDEX memory_links_source_idx ON memory_links (source_id, relationship_type);
CREATE INDEX memory_links_target_idx ON memory_links (target_id, relationship_type);
CREATE INDEX memory_links_chain_idx  ON memory_links (source_id)
  WHERE relationship_type = 'supersedes';

-- §3.5 space_memberships
CREATE INDEX memberships_principal_idx ON space_memberships (principal_type, principal_id);

-- §3.6 entities and children
CREATE INDEX entities_search_tsv_idx ON entities USING gin (search_tsv);
CREATE INDEX entities_aliases_idx    ON entities USING gin (aliases);
CREATE INDEX entities_type_idx       ON entities (entity_type);
CREATE INDEX entity_facts_entity_idx ON entity_facts (entity_id);
CREATE INDEX entity_facts_range_idx  ON entity_facts USING gist (fact_range);
CREATE INDEX entity_facts_current_idx ON entity_facts (entity_id)
  WHERE valid_until IS NULL;
CREATE INDEX memory_entities_entity_idx ON memory_entities (entity_id, created_at DESC);

-- §3.7 recall_requests / recall_results
CREATE INDEX recall_requests_session_idx  ON recall_requests (session_id, requested_at DESC);
CREATE INDEX recall_requests_time_idx     ON recall_requests (requested_at DESC);
CREATE INDEX recall_requests_trigger_idx  ON recall_requests (trigger, requested_at DESC);
CREATE INDEX recall_requests_active_idx   ON recall_requests (requested_at)
  WHERE status IN ('queued', 'running');
CREATE INDEX recall_results_created_idx ON recall_results (created_at DESC);

-- §3.11b lifecycle_jobs
CREATE INDEX lifecycle_jobs_open_idx ON lifecycle_jobs (kind, created_at)
  WHERE status IN ('queued', 'running');
CREATE INDEX lifecycle_jobs_scope_idx ON lifecycle_jobs (scope_kind, scope_id);

-- §3.7b memory_access_log
-- Recent-access queries only; full history is served by sequential scans / partitions
CREATE INDEX memory_access_log_recent_idx ON memory_access_log (memory_id, accessed_at DESC);

-- §3.8 promotion_proposals
-- Review queue: small, hot partial index
CREATE INDEX proposals_open_idx ON promotion_proposals (shared_space_id, proposed_at)
  WHERE status IN ('draft', 'in_review');
CREATE INDEX proposals_space_idx    ON promotion_proposals (shared_space_id);
CREATE INDEX proposals_candidate_idx ON promotion_proposals (candidate_memory_id);

-- Review F10: at most one open proposal per (candidate, space) — prevents
-- duplicate merges of the same memory into the same space via parallel proposals.
CREATE UNIQUE INDEX proposals_one_open_idx ON promotion_proposals (candidate_memory_id, shared_space_id)
  WHERE status IN ('draft', 'in_review');

-- §3.9 memory_embeddings
CREATE INDEX memory_embeddings_hnsw1536_idx ON memory_embeddings
  USING hnsw ((vec_1536::halfvec(1536)) halfvec_cosine_ops) WITH (m = 16, ef_construction = 200)
  WHERE vec_1536 IS NOT NULL;
CREATE INDEX memory_embeddings_hnsw768_idx ON memory_embeddings
  USING hnsw ((vec_768::halfvec(768)) halfvec_cosine_ops) WITH (m = 16, ef_construction = 200)
  WHERE vec_768 IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS memory_embeddings_hnsw768_idx;
DROP INDEX IF EXISTS memory_embeddings_hnsw1536_idx;
DROP INDEX IF EXISTS proposals_one_open_idx;
DROP INDEX IF EXISTS proposals_candidate_idx;
DROP INDEX IF EXISTS proposals_space_idx;
DROP INDEX IF EXISTS proposals_open_idx;
DROP INDEX IF EXISTS memory_access_log_recent_idx;
DROP INDEX IF EXISTS lifecycle_jobs_scope_idx;
DROP INDEX IF EXISTS lifecycle_jobs_open_idx;
DROP INDEX IF EXISTS recall_results_created_idx;
DROP INDEX IF EXISTS recall_requests_active_idx;
DROP INDEX IF EXISTS recall_requests_trigger_idx;
DROP INDEX IF EXISTS recall_requests_time_idx;
DROP INDEX IF EXISTS recall_requests_session_idx;
DROP INDEX IF EXISTS memory_entities_entity_idx;
DROP INDEX IF EXISTS entity_facts_current_idx;
DROP INDEX IF EXISTS entity_facts_range_idx;
DROP INDEX IF EXISTS entity_facts_entity_idx;
DROP INDEX IF EXISTS entities_type_idx;
DROP INDEX IF EXISTS entities_aliases_idx;
DROP INDEX IF EXISTS entities_search_tsv_idx;
DROP INDEX IF EXISTS memberships_principal_idx;
DROP INDEX IF EXISTS memory_links_chain_idx;
DROP INDEX IF EXISTS memory_links_target_idx;
DROP INDEX IF EXISTS memory_links_source_idx;
DROP INDEX IF EXISTS memories_superseded_idx;
DROP INDEX IF EXISTS memories_ttl_idx;
DROP INDEX IF EXISTS memories_decay_idx;
DROP INDEX IF EXISTS memories_owner_idx;
DROP INDEX IF EXISTS memories_session_idx;
DROP INDEX IF EXISTS memories_scope_idx;
DROP INDEX IF EXISTS memories_type_idx;
DROP INDEX IF EXISTS memories_created_idx;
DROP INDEX IF EXISTS memories_validity_idx;
DROP INDEX IF EXISTS memories_tags_idx;
DROP INDEX IF EXISTS memories_content_trgm_idx;
DROP INDEX IF EXISTS memories_search_tsv_idx;
DROP INDEX IF EXISTS memories_embedding_hnsw_idx;
DROP INDEX IF EXISTS agent_sessions_active_memories_idx;
DROP INDEX IF EXISTS agent_sessions_active_idx;
DROP INDEX IF EXISTS agent_sessions_user_idx;
DROP INDEX IF EXISTS spaces_attention_idx;
DROP INDEX IF EXISTS spaces_owner_idx;
