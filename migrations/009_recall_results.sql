-- +goose Up
-- DDL: recall_results (pgvector-data-model.md §3.7)
CREATE TABLE recall_results (
  result_id      uuid        PRIMARY KEY,
  request_id     uuid        NOT NULL UNIQUE REFERENCES recall_requests (request_id) ON DELETE CASCADE,
  candidates     jsonb       NOT NULL CHECK (jsonb_typeof(candidates) = 'array'),
  injection_plan jsonb       NOT NULL CHECK (jsonb_typeof(injection_plan) = 'array'),
  slots_used     integer     NOT NULL DEFAULT 0 CHECK (slots_used >= 0),
  tokens_used    integer     NOT NULL DEFAULT 0 CHECK (tokens_used >= 0),
  latency_ms     integer     CHECK (latency_ms >= 0),
  created_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS recall_results;
