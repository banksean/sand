DROP INDEX IF EXISTS idx_active_sandbox_name;
DROP INDEX IF EXISTS idx_sandboxes_state;
ALTER TABLE sandboxes DROP COLUMN trash_work_dir;
ALTER TABLE sandboxes DROP COLUMN deleted_at;
ALTER TABLE sandboxes DROP COLUMN state;
ALTER TABLE sandboxes DROP COLUMN name;
