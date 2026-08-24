-- +goose Up
-- DDL: entities and children (pgvector-data-model.md §3.6)
CREATE TABLE entities (
  entity_id    uuid        PRIMARY KEY,
  name         text        NOT NULL CHECK (length(btrim(name)) > 0),
  entity_type  text        NOT NULL,
  aliases      text[]      ,
  created_at   timestamptz NOT NULL DEFAULT now(),
  search_tsv   tsvector    GENERATED ALWAYS AS (
               setweight(to_tsvector('english', coalesce(name, '')), 'A') ||
               setweight(to_tsvector('english', ai_array_to_string(aliases, ' ')), 'C')
             ) STORED,
  CONSTRAINT entities_type_chk CHECK (
    entity_type IN ('person', 'project', 'repository', 'tool', 'organization', 'concept')),
  CONSTRAINT entities_unique_name UNIQUE (name, entity_type)
);

CREATE TABLE entity_facts (
  id          uuid        PRIMARY KEY,
  entity_id   uuid        NOT NULL REFERENCES entities (entity_id) ON DELETE CASCADE,
  fact        text        NOT NULL,
  memory_id   uuid        REFERENCES memories (id) ON DELETE SET NULL,
  valid_from  timestamptz NOT NULL,
  valid_until timestamptz ,
  fact_range  tstzrange   GENERATED ALWAYS AS (tstzrange(valid_from, valid_until)) STORED,
  CONSTRAINT entity_facts_range_chk CHECK (valid_until IS NULL OR valid_until > valid_from)
);

CREATE TABLE memory_entities (
  memory_id  uuid        NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  entity_id  uuid        NOT NULL REFERENCES entities (entity_id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (memory_id, entity_id)
);

-- +goose Down
DROP TABLE IF EXISTS memory_entities;
DROP TABLE IF EXISTS entity_facts;
DROP TABLE IF EXISTS entities;
