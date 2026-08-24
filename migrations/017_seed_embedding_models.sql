-- +goose Up
-- Seed the registry (F11). NOTE: pgvector-data-model.md §3.9 lists all four
-- known models, but embedding_models_dims_chk (exact validated DDL) admits only
-- dims IN (1536, 768) — matching the typed columns of memory_embeddings and
-- domain.SupportedDimensions. Seeding 3072/384 would violate the CHECK, so only
-- the two usable models are registered; the others are added when their typed
-- columns exist.
INSERT INTO embedding_models (model_id, provider, dims) VALUES
  ('text-embedding-3-small', 'openai', 1536),
  ('bge-base-en-v1.5',       'bge', 768)
ON CONFLICT (model_id) DO NOTHING;

-- +goose Down
DELETE FROM embedding_models WHERE model_id IN (
  'text-embedding-3-small',
  'bge-base-en-v1.5'
);
