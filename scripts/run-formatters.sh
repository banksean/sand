#!/usr/bin/env bash
set -e

# Script to run formatters for both Go and webui files
# Usage: bin/run-formatters.sh [check]
# If 'check' is provided, only checks formatting without making changes

CHECK_MODE=false
if [ "$1" = "check" ]; then
  CHECK_MODE=true
  echo "Running in check mode (formatting will not be modified)"
else
  echo "Running in fix mode (formatting will be modified)"
fi

# Go formatting with gofumpt
echo "Checking Go formatting..."
if [ "$CHECK_MODE" = true ]; then
  # In check mode, we want to display the files that need formatting and exit with error if any
  FILES_TO_FORMAT=$(gofumpt -l .)
  if [ -n "$FILES_TO_FORMAT" ]; then
    echo "The following Go files need formatting:"
    echo "$FILES_TO_FORMAT"
    exit 1
  else
    echo "Go formatting check passed"
  fi
else
  # In fix mode, we apply the formatting
  echo "Fixing Go formatting with gofumpt"
  gofumpt -w .
  echo "Go formatting complete"
fi

if [ "$CHECK_MODE" = true ]; then
  echo "All formatting checks passed"
else
  echo "All formatting has been fixed"
fi
