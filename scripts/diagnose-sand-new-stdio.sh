#!/usr/bin/env bash
set -euo pipefail

if [ ! -t 0 ]; then
	echo "error: run this from a real interactive terminal, not a pipe" >&2
	exit 1
fi

SANDBOX_NAME=${SANDBOX_NAME:-stdio-diag}
SNAPSHOT_DIR=${SNAPSHOT_DIR:-"/tmp/sand-stdio-diag-$(date +%Y%m%d-%H%M%S)"}
mkdir -p "$SNAPSHOT_DIR"

if [ "$#" -eq 0 ]; then
	set -- new --tmux=false --atch=false "$SANDBOX_NAME"
fi

TTY_PATH=$(tty)
LOG_FILE="$SNAPSHOT_DIR/report.txt"

snapshot() {
	local label=$1
	{
		echo "==== $label ===="
		echo "date: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
		echo "tty: $TTY_PATH"
		echo
		echo "-- stty -a --"
		stty -a < "$TTY_PATH" || true
		echo
		echo "-- lsof tty --"
		if command -v lsof >/dev/null 2>&1; then
			lsof "$TTY_PATH" || true
		else
			echo "lsof not found"
		fi
		echo
		echo "-- possible apple/container processes --"
		ps -axo pid,ppid,stat,command | grep -E '([c]ontainer|[a]piserver|[s]and)' || true
		echo
	} >> "$LOG_FILE"
}

echo "Writing diagnostic snapshots to $SNAPSHOT_DIR"
echo "Command: sand $*" | tee "$LOG_FILE"
echo "TTY: $TTY_PATH" | tee -a "$LOG_FILE"

snapshot "before sand"

set +e
sand "$@"
SAND_STATUS=$?
set -e

snapshot "immediately after sand"
sleep 2
snapshot "two seconds after sand"

echo
echo "Type or paste a short test string, then press return."
read -r -p "> " typed
printf 'typed length: %d\n' "${#typed}" | tee -a "$LOG_FILE"
printf 'typed value: %q\n' "$typed" | tee -a "$LOG_FILE"

snapshot "after read test"

echo "sand exit status: $SAND_STATUS" | tee -a "$LOG_FILE"
echo "report: $LOG_FILE"
exit "$SAND_STATUS"
