-- +goose Up
-- DDL: memory_access_log (review F8, pgvector-data-model.md §3.7b)
-- Replaces memories.last_accessed_at: reads append here instead of updating the
-- hottest table (2,500 row-updates/min at 100 recalls/min x 25 candidates).
CREATE TABLE memory_access_log (
  id          bigserial   PRIMARY KEY,
  memory_id   uuid        NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  accessed_by uuid        NOT NULL,
  access_type text        NOT NULL CHECK (access_type IN ('recall', 'direct_get', 'session_activate')),
  accessed_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS memory_access_log;
