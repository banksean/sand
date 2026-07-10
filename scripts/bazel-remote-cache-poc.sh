#!/usr/bin/env bash
set -euo pipefail

CONTAINER_DNS_DOMAIN="${CONTAINER_DNS_DOMAIN:-dev.local}"
CACHE_CONTAINER_NAME="${CACHE_CONTAINER_NAME:-sand-bazel-cache}"
CACHE_IMAGE="${CACHE_IMAGE:-docker.io/buchgr/bazel-remote-cache:latest}"
CACHE_MAX_SIZE_GIB="${CACHE_MAX_SIZE_GIB:-10}"
CACHE_PORT="${CACHE_PORT:-8080}"

default_app_base_dir() {
  if [ -n "${SAND_APP_BASE_DIR:-}" ]; then
    printf '%s\n' "$SAND_APP_BASE_DIR"
    return
  fi
  printf '%s\n' "${HOME}/Library/Application Support/Sand"
}

APP_BASE_DIR="$(default_app_base_dir)"
CACHE_DIR="${CACHE_DIR:-${APP_BASE_DIR}/caches/bazel-remote}"
CACHE_URL="http://${CACHE_CONTAINER_NAME}.${CONTAINER_DNS_DOMAIN}:${CACHE_PORT}"

usage() {
  cat <<EOF
Usage: $0 <start|stop|status|demo> [args]

Commands:
  start                 Start the shared bazel-remote cache container.
  stop                  Remove the shared cache container.
  status                Query the cache /status endpoint.
  demo <workspace> [target]
                        Build the same Bazel target in two sandboxes.

Environment:
  CONTAINER_DNS_DOMAIN  Container DNS domain. Default: dev.local
  SAND_APP_BASE_DIR     Sand app data dir. Default: ~/Library/Application Support/Sand
  CACHE_IMAGE           bazel-remote image. Default: buchgr/bazel-remote-cache:latest
  CACHE_MAX_SIZE_GIB    Max cache size in GiB. Default: 10
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

start_cache() {
  require_cmd container
  mkdir -p "$CACHE_DIR"

  container rm -f "$CACHE_CONTAINER_NAME" >/dev/null 2>&1 || true
  container run \
    --name "$CACHE_CONTAINER_NAME" \
    --detach \
    --cpus 2 \
    --memory 1024M \
    -p "${CACHE_PORT}:${CACHE_PORT}/tcp" \
    --volume "${CACHE_DIR}:/data" \
    "$CACHE_IMAGE" \
    --dir=/data \
    "--max_size=${CACHE_MAX_SIZE_GIB}" \
    "--http_address=0.0.0.0:${CACHE_PORT}" \
    --grpc_address=none

  echo "started ${CACHE_CONTAINER_NAME}"
  echo "cache URL: ${CACHE_URL}"
  echo "cache dir: ${CACHE_DIR}"
}

stop_cache() {
  require_cmd container
  container rm -f "$CACHE_CONTAINER_NAME"
}

status_cache() {
  require_cmd curl
  curl -fsS "${CACHE_URL}/status"
  printf '\n'
}

run_build() {
  local sandbox="$1"
  local workspace="$2"
  local target="$3"

  sand --caches-bazel exec \
    -d "$workspace" \
    "$sandbox" \
    sh -lc 'bazel clean --expunge >/dev/null 2>&1 || true; bazel build --color=no --show_result=0 "$1"' sh "$target"
}

demo_cache() {
  require_cmd sand
  require_cmd curl

  local workspace="${1:-}"
  local target="${2:-//...}"
  if [ -z "$workspace" ]; then
    echo "demo requires a Bazel workspace path" >&2
    usage >&2
    exit 1
  fi
  if [ ! -d "$workspace" ]; then
    echo "workspace does not exist: $workspace" >&2
    exit 1
  fi

  start_cache
  echo "initial cache status:"
  status_cache || true

  echo "building ${target} in bazel-cache-a"
  run_build "bazel-cache-a" "$workspace" "$target"

  echo "cache status after first build:"
  status_cache || true

  echo "building ${target} in bazel-cache-b"
  run_build "bazel-cache-b" "$workspace" "$target"

  echo "cache status after second build:"
  status_cache || true
}

cmd="${1:-}"
case "$cmd" in
  start)
    start_cache
    ;;
  stop)
    stop_cache
    ;;
  status)
    status_cache
    ;;
  demo)
    shift
    demo_cache "$@"
    ;;
  -h|--help|help|"")
    usage
    ;;
  *)
    echo "unknown command: $cmd" >&2
    usage >&2
    exit 1
    ;;
esac
