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
    image_name, dns_domain, env_file
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    container_id = excluded.container_id,
    host_origin_dir = excluded.host_origin_dir,
    sandbox_work_dir = excluded.sandbox_work_dir,
    image_name = excluded.image_name,
    dns_domain = excluded.dns_domain,
    env_file = excluded.env_file,
    updated_at = CURRENT_TIMESTAMP;

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
