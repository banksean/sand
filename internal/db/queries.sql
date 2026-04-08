-- name: GetSandbox :one
SELECT * FROM sandboxes
WHERE id = ?
LIMIT 1;

-- name: ListSandboxes :many
SELECT * FROM sandboxes
ORDER BY created_at DESC;

-- name: UpsertSandbox :exec
INSERT INTO sandboxes (
    id, container_id, host_origin_dir, sandbox_work_dir,
    image_name, dns_domain, env_file, agent_type,
    original_git_origin, original_git_branch, original_git_commit,
    original_git_is_dirty, allowed_domains,
    cpu, memory_mb, default_username, default_uid
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    container_id = excluded.container_id,
    host_origin_dir = excluded.host_origin_dir,
    sandbox_work_dir = excluded.sandbox_work_dir,
    image_name = excluded.image_name,
    dns_domain = excluded.dns_domain,
    env_file = excluded.env_file,
    agent_type = excluded.agent_type,
    updated_at = CURRENT_TIMESTAMP,
    original_git_origin = excluded.original_git_origin,
    original_git_branch = excluded.original_git_branch,
    original_git_commit = excluded.original_git_commit,
    original_git_is_dirty = excluded.original_git_is_dirty,
    allowed_domains = excluded.allowed_domains,
    cpu = excluded.cpu,
    memory_mb = excluded.memory_mb,
    default_username = excluded.default_username,
    default_uid = excluded.default_uid;

-- name: UpdateContainerID :exec
UPDATE sandboxes
SET container_id = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteSandbox :exec
DELETE FROM sandboxes
WHERE id = ?;

-- name: GetSandboxesByImage :many
SELECT * FROM sandboxes
WHERE image_name = ?
ORDER BY created_at DESC;
