-- +goose Up
-- DDL: memory_embeddings (pgvector-data-model.md §3.9)
CREATE TABLE memory_embeddings (
  memory_id  uuid        NOT NULL REFERENCES memories (id) ON DELETE CASCADE,
  model_id   text        NOT NULL REFERENCES embedding_models (model_id),
  vec_1536   vector(1536),
  vec_768    vector(768),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (memory_id, model_id),
  CONSTRAINT memory_embeddings_one_vec_chk CHECK (
    (vec_1536 IS NOT NULL)::int + (vec_768 IS NOT NULL)::int = 1)
  -- dims↔model consistency is validated by the loader; a CHECK cannot
  -- reference embedding_models (subqueries are not allowed in CHECK)
);

-- +goose Down
DROP TABLE IF EXISTS memory_embeddings;
