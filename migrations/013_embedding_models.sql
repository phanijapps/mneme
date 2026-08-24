-- +goose Up
-- DDL: embedding_models (review F11, pgvector-data-model.md §3.9)
CREATE TABLE embedding_models (
  model_id        text        PRIMARY KEY,
  provider        text        NOT NULL,
  dims            integer     NOT NULL,
  distance_metric text        NOT NULL DEFAULT 'cosine',
  is_active       boolean     NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT embedding_models_metric_chk CHECK (distance_metric IN ('cosine', 'l2', 'ip')),
  CONSTRAINT embedding_models_dims_chk CHECK (dims IN (1536, 768))
);

-- +goose Down
DROP TABLE IF EXISTS embedding_models;
