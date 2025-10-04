#!/bin/bash

GIT_REPO=$(git config --get remote.origin.url)
GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
GIT_COMMIT=$(git rev-parse HEAD)
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

go build -ldflags "\
    -X 'main.GitRepo=${GIT_REPO}' \
    -X 'main.GitBranch=${GIT_BRANCH}' \
    -X 'main.GitCommit=${GIT_COMMIT}' \
    -X 'main.BuildTime=${BUILD_TIME}'" \
    -o ./bin/sand ./cmd/sand