# GEMINI.md

This file provides guidance to the Gemini CLI (or any Gemini-based AI agent) when working with code in this repository.

## What this project is

`sand` creates lightweight, disposable Linux sandboxes for AI coding agents on Apple Silicon Macs. It uses APFS copy-on-write clones for fast filesystem isolation and Apple Containerization (Kata-based) for hardware-level process isolation. The primary use case is running AI agents (such as Gemini, Claude Code, Codex, opencode) against a cloned project without risk to the host filesystem.

**Platform requirements:** macOS 26+, Apple Silicon, Apple `container` CLI v1.1.0.

## Build and development commands

This project uses [Task](https://taskfile.dev/) (`Taskfile.yaml`), not Make. Tool versions are pinned via [mise](https://mise.en.dev/) (`mise.toml`).

```sh
task build          # go generate + go build ./...
task test           # go generate + gotestsum ./...
task install        # stop daemon, go generate, go install ./cmd/...
task generate       # run proto:generate + go generate ./...
task proto:generate # regenerate gRPC/protobuf bindings from daemon.proto
```

Run a single test:
```sh
go test -run TestFunctionName ./internal/package
go test -v -run TestFunctionName ./internal/cli
```

`task build` and `task test` both depend on `generate`, so protobuf and sqlc code is always regenerated first. The generated files (`internal/daemon/daemonpb/*.go`, `internal/db/queries.sql.go`) are committed.

## Architecture & Codebase Design

- **The Host-Daemon model**: All sandbox state is managed by the long-lived host daemon (`cmd/sandd`), storing details in a SQLite database. The client CLI (`cmd/sand`) is a thin wrapper that delegates work over a gRPC Unix socket.
- **Innie & Outie**:
  - **`cmd/sand` (Outie)**: The host-side CLI (`main_darwin.go`).
  - **`Innie`**: The container-side CLI (`main_linux.go`).
- **Agent Integration**: Built-in agent specifications (e.g., Gemini, Claude, Codex) are defined in `internal/agentdefs/agentdefs.go`.
  - For Gemini: The agent uses `@google/gemini-cli` with `--approval-mode=yolo` for hands-off execution.

## Package Architecture

| Package | Role |
|---|---|
| `internal/cli/` | Struct-based commands utilizing Kong field tags for flag parsing (e.g., `new_cmd.go`). |
| `internal/daemon/` | gRPC server handlers (`daemon_grpc_unary.go`, `daemon_grpc_streams.go`) and Model Context Protocol (MCP) integrations. |
| `internal/daemon/boxer/` | Sandbox database, repository, copy-on-write workspace manager, image fetcher, SSH key provisioning, and Squid HTTP cache coordination. |
| `internal/daemon/lifecycle/` | Container state machine, mount setup, and startup hooks execution. |
| `internal/daemon/daemonpb/` | Protobuf contract definitions (`daemon.proto`) and generated Go schemas. |
| `internal/hostops/` | OS-specific abstraction layers (`ContainerOps`, `ImageOps`, etc.) interfacing with Apple Containerization. |
| `internal/applecontainer/` | Lower-level client communicating with Apple's `com.apple.container.apiserver` via macOS `libxpc` and Cgo blocks. |
| `internal/cloning/` | High-performance filesystem preparation, APFS copy-on-write setup, and local Git mirrors. |
| `internal/agents/` | Agent-specific config registry bridging cloner preparation and runtime container details. |
| `internal/containerruntime/` | Custom image initialization, bootstrap routines, and agent installation hooks. |
| `internal/agentdefs/` | Declarations for all supported developer agent CLIs and their default commands. |
| `internal/db/` | SQLite migration files, DB connection configurations, and `sqlc`-generated active queries. |
| `internal/sshimmer/` | Safe container access via custom Ed25519 CA-based host and user certificate generation. |
| `internal/sandboxlog/` | Log filtering and sensitive environment variable scrubbers. |
| `internal/profiles/` | Policy configurations (Dotfiles, Environment, SSH, Resource Limits) associated with sandboxes. |

## Guidelines for Modifying Code

1. **Daemon as Source of Truth**: Never implement state-changing operations directly inside `internal/cli/`. All business logic must be handled via gRPC requests to the daemon, ensuring SQLite database state is updated appropriately.
2. **Database Migrations**: When changing database tables, write raw SQL migrations in `internal/db/migrations/` and modify `internal/db/queries.sql` as needed. Run `task generate` to run `sqlc` and update `internal/db/queries.sql.go`. Never modify `schema.sql` by hand.
3. **gRPC Protocols**: If extending API payloads or RPCs, edit `internal/daemon/daemonpb/daemon.proto`, and then execute `task proto:generate` to recreate bindings.
4. **macOS 26+ Requirements**: Note that the codebase enforces a pre-requisite check (`internal/runtimedeps/runtimedeps.go`) requiring macOS major version 26 or greater. Mock wrappers or stub functions exist for running development checks on non-compatible platforms.
5. **No Context Discarding**: Avoid discarding contexts unless absolutely necessary for background processes (such as SSH tunnels). Any background goroutines or processes should ideally be bound to context lifetimes.
