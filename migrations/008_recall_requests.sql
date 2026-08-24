-- +goose Up
-- DDL: recall_requests (pgvector-data-model.md §3.7, incl. mode/status/error from review F3)
CREATE TABLE recall_requests (
  request_id   uuid        PRIMARY KEY,
  query        text        NOT NULL,
  context      jsonb       NOT NULL,
  trigger      text        NOT NULL,
  agent_id     text        NOT NULL,
  session_id   uuid        NOT NULL REFERENCES agent_sessions (session_id) ON DELETE CASCADE,
  strategies   text[]      NOT NULL,
  top_k        integer     NOT NULL DEFAULT 50 CHECK (top_k BETWEEN 1 AND 200),
  rerank       text        NOT NULL DEFAULT 'cross_encoder',
  min_score    numeric(3,2) NOT NULL DEFAULT 0.35 CHECK (min_score BETWEEN 0 AND 1),
  slot_budget  integer     ,
  token_budget integer     ,
  mode         text        NOT NULL DEFAULT 'sync',
  status       text        NOT NULL DEFAULT 'completed',
  error        jsonb       ,
  requested_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT recall_requests_trigger_chk CHECK (
    trigger IN ('task_context', 'user_query', 'temporal', 'associative', 'session_start')),
  CONSTRAINT recall_requests_rerank_chk CHECK (rerank IN ('cross_encoder', 'none')),
  CONSTRAINT recall_requests_strategies_chk CHECK (
    strategies <@ ARRAY['vector', 'bm25', 'graph', 'temporal']::text[]
    AND array_length(strategies, 1) >= 1),
  CONSTRAINT recall_requests_budgets_chk CHECK (
    (slot_budget IS NULL OR slot_budget >= 0) AND (token_budget IS NULL OR token_budget >= 0)),
  CONSTRAINT recall_requests_mode_chk CHECK (mode IN ('sync', 'async')),
  CONSTRAINT recall_requests_status_chk CHECK (
    status IN ('queued', 'running', 'completed', 'failed')),
  CONSTRAINT recall_requests_status_pairing_chk CHECK (
    status <> 'failed' OR error IS NOT NULL)
);

-- +goose Down
DROP TABLE IF EXISTS recall_requests;
