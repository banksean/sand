ALTER TABLE sandboxes ADD COLUMN session_archive_enabled BOOLEAN NOT NULL DEFAULT 0;

CREATE TABLE agent_sessions (
    id TEXT PRIMARY KEY,
    agent_type TEXT NOT NULL,
    native_session_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'raw-only',
    normalized_path TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    started_at DATETIME,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    adapter_version INTEGER NOT NULL DEFAULT 1,
    UNIQUE(agent_type, native_session_id)
);

CREATE TABLE agent_session_sources (
    session_id TEXT NOT NULL,
    sandbox_id TEXT NOT NULL,
    sandbox_name TEXT NOT NULL,
    native_path TEXT NOT NULL,
    raw_path TEXT PRIMARY KEY,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(session_id) REFERENCES agent_sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_agent_sessions_updated_at ON agent_sessions(updated_at DESC);
CREATE INDEX idx_agent_session_sources_sandbox_id ON agent_session_sources(sandbox_id);

CREATE TABLE agent_session_launches (
    id TEXT PRIMARY KEY,
    sandbox_id TEXT NOT NULL,
    agent_type TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME
);

CREATE INDEX idx_agent_session_launches_active ON agent_session_launches(sandbox_id, ended_at);
