#!/usr/bin/env bash
set -euo pipefail

if [[ "${SAND_DESTRUCTIVE_SMOKE_TEST:-}" != "1" ]]; then
	cat >&2 <<'EOF'
This destructive smoke test has moved to Go.

It removes sandboxes, sand configuration, local sand binaries, and sand state.
Run it explicitly with:

  SAND_DESTRUCTIVE_SMOKE_TEST=1 go test -tags destructive_smoke ./internal/smoketest -run TestDestructiveSmoke -timeout 2h -v

To include the optional VS Code launch step, also set:

  SAND_DESTRUCTIVE_SMOKE_VSC=1
EOF
	exit 2
fi

exec go test -tags destructive_smoke ./internal/smoketest -run TestDestructiveSmoke -timeout 2h -v
