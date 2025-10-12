#!/bin/bash

GIT_REPO=$(git config --get remote.origin.url)
GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
GIT_COMMIT=$(git rev-parse HEAD)
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

go build -ldflags "\
    -X 'github.com/banksean/sand/version.GitRepo=${GIT_REPO}' \
    -X 'github.com/banksean/sand/version.GitBranch=${GIT_BRANCH}' \
    -X 'github.com/banksean/sand/version.GitCommit=${GIT_COMMIT}' \
    -X 'github.com/banksean/sand/version.BuildTime=${BUILD_TIME}'" \
    -o ./bin/sand ./cmd/sand