-- +goose Up
-- DDL: memory_links (pgvector-data-model.md §3.4)
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

-- +goose Down
DROP TABLE IF EXISTS memory_links;
