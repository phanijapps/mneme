-- +goose Up
-- DDL: agent_sessions (pgvector-data-model.md §3.2)
CREATE TABLE agent_sessions (
  session_id             uuid        PRIMARY KEY,
  agent_type             text        NOT NULL,
  user_id                text        NOT NULL,
  shared_space_id        uuid        REFERENCES shared_memory_spaces (id) ON DELETE SET NULL,
  model                  text        NOT NULL,
  max_tokens             integer     NOT NULL CHECK (max_tokens >= 1),
  used_tokens            integer     NOT NULL DEFAULT 0 CHECK (used_tokens >= 0),
  instruction_slot_budget integer    NOT NULL CHECK (instruction_slot_budget >= 0),
  slots_used             integer     NOT NULL DEFAULT 0 CHECK (slots_used >= 0),
  active_memories        uuid[]      NOT NULL DEFAULT '{}',
  injection_order        text[]      ,
  created_at             timestamptz NOT NULL DEFAULT now(),
  ended_at               timestamptz ,
  summary                text        ,
  CONSTRAINT agent_sessions_agent_type_chk CHECK (
    agent_type IN ('claude-code', 'codex', 'cursor', 'letta', 'custom')),
  CONSTRAINT agent_sessions_timing_chk CHECK (
    ended_at IS NULL OR ended_at > created_at),
  CONSTRAINT agent_sessions_slot_budget_chk CHECK (
    slots_used <= instruction_slot_budget OR instruction_slot_budget = 0)
);

-- +goose Down
DROP TABLE IF EXISTS agent_sessions;
