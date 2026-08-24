-- +goose Up
-- DDL: lifecycle_jobs (review F3, pgvector-data-model.md §3.11b)
-- Backs GET /lifecycle/jobs/{id} and every JobRef poll target in api-contracts
-- (session end, consolidate, decay, space sync).
CREATE TABLE lifecycle_jobs (
  job_id      uuid        PRIMARY KEY,
  kind        text        NOT NULL,
  status      text        NOT NULL DEFAULT 'queued',
  scope_kind  text        ,
  scope_id    text        ,
  result      jsonb       ,
  error       jsonb       ,
  created_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz ,
  CONSTRAINT lifecycle_jobs_kind_chk CHECK (
    kind IN ('consolidation', 'decay', 'space_sync', 'session_end')),
  CONSTRAINT lifecycle_jobs_status_chk CHECK (
    status IN ('queued', 'running', 'completed', 'failed')),
  CONSTRAINT lifecycle_jobs_terminal_chk CHECK (
    (status IN ('completed', 'failed')) = (finished_at IS NOT NULL)),
  CONSTRAINT lifecycle_jobs_error_pairing_chk CHECK (
    status <> 'failed' OR error IS NOT NULL)
);

-- +goose Down
DROP TABLE IF EXISTS lifecycle_jobs;
