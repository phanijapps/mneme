-- +goose Up
-- DDL: space_memberships (pgvector-data-model.md §3.5)
CREATE TABLE space_memberships (
  space_id        uuid        NOT NULL REFERENCES shared_memory_spaces (id) ON DELETE CASCADE,
  principal_type  text        NOT NULL,
  principal_id    text        NOT NULL,
  access_level    text        NOT NULL,
  granted_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (space_id, principal_type, principal_id),
  CONSTRAINT memberships_principal_type_chk CHECK (principal_type IN ('user', 'agent', 'session', 'group')),
  CONSTRAINT memberships_access_level_chk CHECK (access_level IN ('read', 'write', 'promote', 'admin'))
);

-- +goose Down
DROP TABLE IF EXISTS space_memberships;
