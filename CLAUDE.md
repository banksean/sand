# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

`sand` creates lightweight, disposable Linux sandboxes for AI coding agents on Apple Silicon Macs. It uses APFS copy-on-write clones for fast filesystem isolation and Apple Containerization (Kata-based) for hardware-level process isolation. The primary use case is running AI agents (Claude Code, Codex, Gemini, opencode) against a cloned project without risk to the host filesystem.

**Platform requirements:** macOS 26+, Apple Silicon, Apple `container` CLI v1.0.0.

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

## Two binaries

- **`cmd/sand`** — host-side CLI (`main_darwin.go`). Thin wrapper: parses flags with [kong](https://github.com/alecthomas/kong), connects to `sandd` over a gRPC unix socket, delegates all work.
- **`cmd/sandd`** — long-lived host daemon. Manages sandbox lifecycle, serves `$AppBaseDir/sandd.grpc.sock`.

The CLI **never** imports daemon internals — only the `daemon.Client` interface. The daemon communicates with both the host CLI and container-side `sand` CLI through the same gRPC socket.

## Package architecture

| Package | Role |
|---|---|
| `internal/cli/` | One file per subcommand (`new_cmd.go`, `shell_cmd.go`, `git_cmd.go`, etc.). Commands are structs with kong field tags. |
| `internal/daemon/` | gRPC server handlers (`daemon_grpc_unary.go`, `daemon_grpc_streams.go`), MCP integration (`mcp.go`). |
| `internal/daemon/internal/boxer/` | Core orchestration: creates APFS clones, pulls images, creates containers, provisions SSH keys, runs hooks. |
| `internal/daemon/daemonpb/` | Protobuf/gRPC definitions (`daemon.proto`) and generated Go bindings. |
| `internal/hostops/` | Interfaces (`ContainerOps`, `ImageOps`, `GitOps`, `FileOps`) and their Apple Containerization implementations. |
| `internal/applecontainer/` | Low-level wrapper around the `container` CLI; XPC protocol handler. |
| `internal/cloning/` | Agent plugin registry (`registry.go`), workspace preparation, agent-specific container config. |
| `internal/agentdefs/` | Agent definitions: auth requirements, install specs for each supported agent. |
| `internal/db/` | SQLite schema (`schema.sql`), sqlc-generated queries (`queries.sql.go`), migrations. |
| `internal/sshimmer/` | Ed25519 SSH key provisioning for container access. |
| `internal/sandboxlog/` | Log redaction handler that scrubs secrets. |
| `internal/profiles/` | Per-sandbox env var and resource limit profiles. |
| `internal/observability/` | Optional OpenTelemetry tracing (OTLP exporter). |
| `internal/runtimedeps/` | Validates environment: container system version, macOS version, DNS domain. |

## Key design patterns

**Daemon is the source of truth.** All sandbox state lives in SQLite (soft-delete pattern: sandboxes are marked deleted, not dropped). The CLI is purely a thin transport layer.

**Agent plugin system.** Adding a new agent means implementing the `AgentRegistry` interface in `internal/cloning/` and registering it in `internal/agentdefs/`. Each agent defines its auth flow, CLI install spec, and container setup hooks.

**Execution flow for `sand new -a claude`:**
1. CLI → gRPC RPC to daemon
2. Boxer: APFS clone of project dir → pull/verify container image → create container with clone mounted at `/app`
3. SSH shimmer provisions keys; agent setup hooks run inside container
4. User shells in; container-side `sand` CLI communicates back to host daemon through container networking

## Database changes

Edit `internal/db/schema.sql` or `internal/db/queries.sql`, then run `task generate` to regenerate `queries.sql.go` via sqlc. Migrations in `internal/db/migrations/` are applied automatically by Boxer on daemon startup.

## Protobuf changes

Edit `internal/daemon/daemonpb/daemon.proto`, then run `task proto:generate`. The generated `*.pb.go` and `*_grpc.pb.go` files are committed.

## Optional observability

```sh
task start-observability   # starts Grafana + Tempo containers
task stop-observability
```

Set `OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317` to enable tracing. See `observability/README.md`.
