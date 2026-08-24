-- +goose Up
-- DDL: extensions and domains (pgvector-data-model.md §3.0)
CREATE EXTENSION IF NOT EXISTS vector;      -- HNSW ANN search
CREATE EXTENSION IF NOT EXISTS pg_trgm;     -- fuzzy lexical fallback

-- Tag syntax from data-models §3.1: ^[a-z0-9][a-z0-9_-]*$
CREATE DOMAIN tag_name AS text
  CHECK (VALUE ~ '^[a-z0-9][a-z0-9_-]*$');

COMMENT ON DOMAIN tag_name IS 'Lowercase tag token; pattern enforced per data-models §3.1';

-- array_to_string is STABLE, so it cannot feed a STORED generated column;
-- this wrapper is genuinely immutable (output depends only on the arguments).
-- coalesce: array_to_string is strict — NULL tags would otherwise NULL the
-- whole search_tsv concatenation and silently kill the sparse path.
CREATE FUNCTION ai_array_to_string(arr text[], sep text) RETURNS text
LANGUAGE sql IMMUTABLE AS $$ SELECT array_to_string(coalesce(arr, '{}'), sep) $$;

COMMENT ON FUNCTION ai_array_to_string(text[], text) IS
  'Immutable wrapper for use in generated tsvector columns';

-- +goose Down
DROP FUNCTION IF EXISTS ai_array_to_string(text[], text);
DROP DOMAIN IF EXISTS tag_name;
DROP EXTENSION IF EXISTS pg_trgm;
DROP EXTENSION IF EXISTS vector;
