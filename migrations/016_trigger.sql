-- +goose Up
-- Maintains shared_memory_spaces.pending_proposals (denormalized counter, §1.3)
-- DDL: proposals counter trigger (pgvector-data-model.md §3.8)
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION proposals_pending_count() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    UPDATE shared_memory_spaces
       SET pending_proposals = pending_proposals + 1,
           sync_status = 'pending_review',
           updated_at = now()
     WHERE id = NEW.shared_space_id;
  ELSIF TG_OP = 'UPDATE' AND OLD.status <> NEW.status THEN
    IF OLD.status IN ('draft', 'in_review') AND NEW.status IN ('merged', 'rejected') THEN
      UPDATE shared_memory_spaces
         SET pending_proposals = greatest(pending_proposals - 1, 0),
             sync_status = CASE
                 WHEN (SELECT count(*) FROM promotion_proposals p
                        WHERE p.shared_space_id = NEW.shared_space_id
                          AND p.status IN ('draft', 'in_review')) = 0
                 THEN 'in_sync' ELSE 'pending_review' END,
             updated_at = now()
       WHERE id = NEW.shared_space_id;
    ELSIF NEW.status IN ('draft', 'in_review') AND OLD.status IN ('merged', 'rejected') THEN
      UPDATE shared_memory_spaces
         SET pending_proposals = pending_proposals + 1,
             sync_status = 'pending_review',
             updated_at = now()
       WHERE id = NEW.shared_space_id;
    END IF;
  ELSIF TG_OP = 'DELETE' AND OLD.status IN ('draft', 'in_review') THEN
    -- Review F5: recompute sync_status, not just the counter — otherwise deleting
    -- the last open proposal leaves the space stuck in 'pending_review'.
    UPDATE shared_memory_spaces
       SET pending_proposals = greatest(pending_proposals - 1, 0),
           sync_status = CASE
               WHEN (SELECT count(*) FROM promotion_proposals p
                      WHERE p.shared_space_id = OLD.shared_space_id
                        AND p.status IN ('draft', 'in_review')) = 0
               THEN 'in_sync' ELSE 'pending_review' END,
           updated_at = now()
     WHERE id = OLD.shared_space_id;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_proposals_pending_count
AFTER INSERT OR UPDATE OF status OR DELETE ON promotion_proposals
FOR EACH ROW EXECUTE FUNCTION proposals_pending_count();
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_proposals_pending_count ON promotion_proposals;
DROP FUNCTION IF EXISTS proposals_pending_count();
