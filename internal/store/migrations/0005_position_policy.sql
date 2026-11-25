-- 0005_position_policy (JSK-T205, docs/research/providers/05 section 6): the single-provider apply lock is released IN THE SAME TRANSACTION as the
-- application transition that ends it, by the database itself, so no code path can forget it. Terminal states other than sent/tracked, and a
-- return to draft_prepared (an approval that no longer stands), release the lock; sent and tracked keep it.
CREATE TRIGGER position_apply_lock_release AFTER UPDATE OF state ON application
WHEN NEW.state IN ('rejected_by_human','expired','failed_terminal','withdrawn','draft_prepared','failed_retryable','pending_draft')
BEGIN
  DELETE FROM position_apply_lock WHERE application_id = NEW.id;
END;
