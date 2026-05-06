ALTER TABLE sandboxes ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE sandboxes ADD COLUMN state TEXT NOT NULL DEFAULT 'active';
ALTER TABLE sandboxes ADD COLUMN deleted_at DATETIME;
ALTER TABLE sandboxes ADD COLUMN trash_work_dir TEXT;

UPDATE sandboxes SET name = id WHERE name = '';

CREATE INDEX IF NOT EXISTS idx_sandboxes_state ON sandboxes(state);
CREATE UNIQUE INDEX IF NOT EXISTS idx_active_sandbox_name ON sandboxes(name) WHERE state = 'active';
