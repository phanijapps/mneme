-- +goose Up
-- DDL: memories — core store (pgvector-data-model.md §3.3)
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

-- +goose Down
DROP TABLE IF EXISTS memories;
