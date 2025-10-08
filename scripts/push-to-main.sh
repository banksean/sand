#!/bin/bash

# Script to push commits to main with automatic rebase handling
# Called from GitHub Actions workflow

set -euo pipefail

# Enhanced error reporting
trap 'echo Error in $0 at line $LINENO: $(cd "'"${PWD}"'" && awk "NR == $LINENO" $0)' ERR

# Function to log messages both to stdout and GitHub step summary if available
log_message() {
    echo "$1"
    if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
        echo "$1" >> "${GITHUB_STEP_SUMMARY}"
    fi
}

# Assert that required environment variables are set
required_vars=(
    "GITHUB_TOKEN"
    "REBASE_ATTEMPT"
    "REPOSITORY"
    "CURRENT_BRANCH"
)

for var in "${required_vars[@]}"; do
    if [[ -z "${!var:-}" ]]; then
        echo "ERROR: ${var} environment variable is required"
        exit 1
    fi
done

# Extract values from environment variables
# shellcheck disable=SC2154 # Environment variables set by GitHub Actions
ATTEMPT="${REBASE_ATTEMPT}"
FORMAT_SHA="${FORMAT_SHA:-}"
ORIGINAL_RUN_NAME="${ORIGINAL_RUN_NAME:-}"
COMMIT_MESSAGE="${COMMIT_MESSAGE:-}"
# Extract branch name from full ref (refs/heads/branch-name)
CURRENT_BRANCH_NAME="${CURRENT_BRANCH#refs/heads/}"

echo "Current rebase attempt: ${ATTEMPT}"

# If attempt is 2 or higher, abort
if [[ "${ATTEMPT}" -ge "2" ]]; then
    log_message "ERROR: Maximum rebase attempts (2) exceeded. Aborting."
    exit 1
fi

COMMIT_TO_PUSH="HEAD"
if [[ -n "${FORMAT_SHA}" ]]; then
    echo "Using formatted commit: ${FORMAT_SHA}"
    COMMIT_TO_PUSH="${FORMAT_SHA}"
fi

git reset --hard "${COMMIT_TO_PUSH}"
git remote -v

TARGET="main"

# Fail fast if it's not a fast-forward, and can't be pushed.
# (set -e exits at this point if necessary)
git push --dry-run origin HEAD:"${TARGET}"

# Fail fast if there are any merge commits
if git rev-list --merges "origin/${TARGET}"..HEAD | grep -q .; then
  echo "There are merge commits in the range we're about to push; refusing to continue."
  exit 1
fi

git push --dry-run origin HEAD:"${TARGET}"

git push origin HEAD:"${TARGET}"

log_message "Successfully pushed to ${TARGET} branch on attempt ${ATTEMPT}."