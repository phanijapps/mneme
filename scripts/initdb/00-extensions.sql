-- Runs once on first init of the pgdata volume, before mneme-api ever
-- connects. mneme-api registers pgvector types on every pooled connection
-- (AfterConnect), which fails unless the extension already exists — so we
-- cannot rely on migration 001 to create it. Migration 001 re-issues these
-- with IF NOT EXISTS and stays idempotent.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
