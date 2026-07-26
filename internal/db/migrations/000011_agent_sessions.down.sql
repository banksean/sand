DROP INDEX IF EXISTS idx_agent_session_sources_sandbox_id;
DROP INDEX IF EXISTS idx_agent_session_launches_active;
DROP TABLE IF EXISTS agent_session_launches;
DROP INDEX IF EXISTS idx_agent_sessions_updated_at;
DROP TABLE IF EXISTS agent_session_sources;
DROP TABLE IF EXISTS agent_sessions;
ALTER TABLE sandboxes DROP COLUMN session_archive_enabled;
