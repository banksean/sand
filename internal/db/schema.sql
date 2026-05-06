CREATE TABLE IF NOT EXISTS sandboxes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active',
    container_id TEXT,
    host_origin_dir TEXT NOT NULL,
    sandbox_work_dir TEXT NOT NULL,
    image_name TEXT NOT NULL,
    dns_domain TEXT,
    env_file TEXT,
    agent_type TEXT DEFAULT 'default',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    original_git_origin TEXT,
    original_git_branch TEXT,
    original_git_commit TEXT,
    original_git_is_dirty BOOLEAN NOT NULL DEFAULT 0,
    allowed_domains TEXT,
    default_username TEXT,
    default_uid TEXT,
    deleted_at DATETIME,
    trash_work_dir TEXT
);

CREATE INDEX IF NOT EXISTS idx_container_id ON sandboxes(container_id);
CREATE INDEX IF NOT EXISTS idx_sandboxes_state ON sandboxes(state);
CREATE UNIQUE INDEX IF NOT EXISTS idx_active_sandbox_name ON sandboxes(name) WHERE state = 'active';
