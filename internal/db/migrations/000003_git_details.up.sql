ALTER TABLE sandboxes ADD COLUMN original_git_origin TEXT;
ALTER TABLE sandboxes ADD COLUMN original_git_branch TEXT;
ALTER TABLE sandboxes ADD COLUMN original_git_commit TEXT;
ALTER TABLE sandboxes ADD COLUMN original_git_is_dirty BOOLEAN NOT NULL DEFAULT 0;
