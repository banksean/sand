# AGENTS.md

## Project overview

`sand` creates lightweight, disposable Linux sandboxes for AI coding agents on
Apple Silicon Macs. It uses APFS copy-on-write clones for filesystem isolation
and Apple Containerization for process isolation.

The supported host environment is macOS 26 or newer on Apple Silicon with the
Apple `container` CLI v1.1.0.

## Build and test commands

Use [Task](https://taskfile.dev/) via `Taskfile.yaml`; do not add Make-based
workflows. Tool versions are pinned in `mise.toml`.

```sh
task build          # Generate code and build all Go packages
task test           # Generate code and run all Go tests with gotestsum
task generate       # Regenerate protobuf, gRPC, schema, and sqlc output
task proto:generate # Regenerate protobuf and gRPC bindings only
task install        # Stop the daemon, generate code, and install commands
```

For a focused test, run:

```sh
go test -run TestFunctionName ./internal/package
go test -v -run TestFunctionName ./internal/cli
```

Prefer the narrowest relevant test while iterating, then run `task test` for
changes that may affect multiple packages or generated output.

Do not run the destructive smoke test unless the user explicitly requests it.
It removes sandboxes, configuration, installed binaries, and local state.

## Architecture

- `cmd/sand` is the thin host-side CLI. It parses flags and communicates with
  `sandd` through the `daemon.Client` interface over gRPC. It must not import
  daemon internals.
- `cmd/sandd` is the long-lived host daemon and source of truth for sandbox
  state. State is stored in SQLite using soft deletion.
- `internal/cli/` contains CLI subcommands.
- `internal/daemon/` contains gRPC handlers and MCP integration.
- `internal/daemon/boxer/` prepares and manages sandbox repositories,
  workspaces, images, SSH keys, mirrors, and cleanup.
- `internal/daemon/lifecycle/` orchestrates containers, mounts, and hooks.
- `internal/hostops/` defines host-operation interfaces and their Apple
  Containerization implementations.
- `internal/cloning/` handles APFS clones, dotfiles, mirrors, remotes, and path
  registration.
- `internal/agents/`, `internal/agentdefs/`, and
  `internal/containerruntime/` define agents and compose their workspace,
  installation, authentication, and runtime behavior.
- `internal/db/` contains SQLite migrations, queries, and generated database
  code.
- `vminit/` builds the Linux VM initialization components.

When adding a built-in agent, begin with `internal/agentdefs/`, then compose its
workspace preparation and container configuration through `internal/agents/`.

## Generated files

Do not edit generated files directly.

- For database schema changes, add or update migrations under
  `internal/db/migrations/`.
- For query changes, edit `internal/db/queries.sql`.
- Never edit `internal/db/schema.sql` by hand; it is generated from migrations.
- After database changes, run `task generate`. The sqlc-generated Go files are
  committed.
- For API changes, edit `internal/daemon/daemonpb/daemon.proto`, then run
  `task proto:generate`. Commit the generated `*.pb.go` and `*_grpc.pb.go`
  files.

## Change guidelines

- Preserve the daemon as the owner of sandbox state and lifecycle decisions.
- Keep the CLI as a transport and presentation layer.
- Follow existing package boundaries and nearby Go patterns.
- Add or update tests for behavior changes.
- Keep documentation and task commands consistent with `Taskfile.yaml` and
  `mise.toml`.
