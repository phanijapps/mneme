-- +goose Up
-- DDL: shared_memory_spaces (pgvector-data-model.md §3.1)
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

-- +goose Down
DROP TABLE IF EXISTS shared_memory_spaces;
