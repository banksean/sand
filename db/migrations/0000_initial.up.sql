CREATE TABLE IF NOT EXISTS sandboxes (
    id TEXT PRIMARY KEY,
    container_id TEXT,
    host_origin_dir TEXT NOT NULL,
    sandbox_work_dir TEXT NOT NULL,
    image_name TEXT NOT NULL,
    dns_domain TEXT,
    env_file TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_container_id ON sandboxes(container_id);
