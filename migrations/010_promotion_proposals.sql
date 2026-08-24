-- +goose Up
-- DDL: promotion_proposals (pgvector-data-model.md §3.8, incl. note/reject_reason
-- from review F7; partial unique index from F10 lives in 015_indexes.sql)
CREATE TABLE promotion_proposals (
  proposal_id         uuid        PRIMARY KEY,
  shared_space_id     uuid        NOT NULL REFERENCES shared_memory_spaces (id) ON DELETE CASCADE,
  candidate_memory_id uuid        NOT NULL REFERENCES memories (id) ON DELETE RESTRICT,
  target_path         text        NOT NULL,
  target_kind         text        NOT NULL,
  target_role         text        NOT NULL,
  diff                text        NOT NULL,
  status              text        NOT NULL DEFAULT 'draft',
  note                text        ,   -- review F7: proposer rationale (api-contracts PromotionInput.note)
  reject_reason       text        ,   -- review F7: required-on-reject audit trail (api-contracts reject body)
  proposed_at         timestamptz NOT NULL DEFAULT now(),
  resolved_at         timestamptz ,
  reviewer            text        ,
  CONSTRAINT proposals_status_chk CHECK (status IN ('draft', 'in_review', 'merged', 'rejected')),
  CONSTRAINT proposals_role_chk CHECK (target_role IN ('procedural', 'semantic', 'episodic')),
  CONSTRAINT proposals_target_kind_chk CHECK (
    target_kind IN ('spec', 'rule', 'agent_doc', 'memory_doc')),
  CONSTRAINT proposals_resolution_chk CHECK (
    (status IN ('merged', 'rejected')) = (resolved_at IS NOT NULL)),
  CONSTRAINT proposals_reject_reason_chk CHECK (
    (status = 'rejected') = (reject_reason IS NOT NULL))
);

-- +goose Down
DROP TABLE IF EXISTS promotion_proposals;
