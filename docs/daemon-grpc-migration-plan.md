# Save Daemon gRPC Migration Plan

This document records the implementation plan for migrating the save daemon
protocol from HTTP-only endpoints to a staged gRPC interface.

## Current Status

The gRPC migration and cleanup slices have landed.

Completed:

- Added `internal/daemon/daemonpb/daemon.proto`.
- Generated and committed `daemon.pb.go` and `daemon_grpc.pb.go`.
- Added host-side gRPC listener support on `sandd.grpc.sock`.
- Added initial gRPC `Ping` and `Version` methods.
- Added a temporary Unix-socket gRPC client for the migration period.
- Added per-sandbox gRPC socket creation under `containergrpc/<id>`.
- Mounted per-sandbox gRPC sockets into containers at
  `/run/host-services/sandd.grpc.sock` while keeping HTTP mounted at
  `/run/host-services/sandd.sock`.
- Added tests for host gRPC socket startup, per-sandbox HTTP+gRPC socket
  creation, and container socket mount wiring.
- Added gRPC streaming RPCs for `CreateSandbox` and `EnsureImage`.
- Migrated the default Unix-socket client to use gRPC for `CreateSandbox` and
  `EnsureImage`.
- Added gRPC streaming client tests for progress, success, and streamed errors.
- Added daemon gRPC streaming integration coverage for `EnsureImage` and
  `CreateSandbox` streamed errors.
- Added unary gRPC RPCs for `Shutdown`, `LogSandbox`, `ListSandboxes`,
  `GetSandbox`, `RemoveSandbox`, `StopSandbox`, `StartSandbox`,
  `ResolveAgentLaunchEnv`, `ExportImage`, `Stats`, and `VSC`.
- Migrated the default Unix-socket client to use gRPC for all non-bootstrap
  daemon methods.
- Added unary gRPC client coverage for request mapping and JSON response
  decoding.
- Added protobuf conversion coverage for `CreateSandboxOpts`.
- Removed migrated HTTP handlers and the HTTP client fallback path.
- Removed the host-side HTTP socket for daemon IPC; host clients now use
  `sandd.grpc.sock`.
- Kept the per-sandbox HTTP socket only for `/sandbox-config`.
- Added developer documentation for regenerating protobuf output.

Remaining:

- None.

## Socket Strategy

Run bootstrap HTTP and daemon gRPC on separate Unix sockets. Do not add `cmux`
or another multiplexing dependency.

Host-side sockets:

- gRPC uses `sandd.grpc.sock`.
- There is no host-side HTTP daemon IPC socket.

Container-side sockets:

- HTTP remains `/run/host-services/sandd.sock` for `/sandbox-config` only.
- gRPC uses `/run/host-services/sandd.grpc.sock` for daemon IPC.

For per-sandbox host-side gRPC sockets, use a short directory name such as
`containergrpc/<id>`. Keep this path short enough to stay comfortably under
Unix socket path length limits.

Keep `/sandbox-config` HTTP-only. It is part of the container bootstrap path
and does not need to move to gRPC. Do not register migrated daemon methods on
the HTTP socket.

Existing running sandboxes do not need upgrade compatibility. The migration
may assume sandboxes are restarted onto the new socket layout.

## Code Generation

Use `protoc` only as a code generation prerequisite. Generated `.pb.go` files
must be committed to the repository so normal builds and tests do not require
`protoc`.

Developers who edit protobuf definitions need `protoc` and the Go protobuf
plugins installed locally. Developers who only build, test, or run the project
should not need them.

Install the generator plugins:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1
```

Regenerate after editing `internal/daemon/daemonpb/daemon.proto`:

```sh
protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  internal/daemon/daemonpb/daemon.proto
```

Commit `daemon.proto`, `daemon.pb.go`, and `daemon_grpc.pb.go` together.

## Implementation Stages

1. Transport proof (done)
   - Add protobuf service definitions for the save daemon protocol.
   - Generate and commit the Go gRPC/protobuf files.
   - Start a host-side gRPC listener on `sandd.grpc.sock` beside the existing
     HTTP listener on `sandd.sock`.
   - Add a minimal unary method to prove request/response plumbing and error
     handling.

2. Container-side socket (done)
   - Expose the host gRPC socket into the container at
     `/run/host-services/sandd.grpc.sock`.
   - Keep the existing HTTP socket mounted at `/run/host-services/sandd.sock`.
   - Use a per-sandbox host-side gRPC socket path under `containergrpc/<id>`.
   - Verify bootstrap still uses HTTP `/sandbox-config`.

3. Streaming methods (done)
   - Migrate streaming operations first, because they benefit most from gRPC.
   - Preserve the existing behavior and ordering guarantees at the API
     boundary.
   - Add integration coverage for stream lifecycle, cancellation, EOF, and
     error propagation.

4. Unary methods (done)
   - Move remaining non-bootstrap HTTP calls to unary gRPC methods.
   - Keep conversions between existing domain types and protobuf messages
     explicit and covered by tests.
   - Preserve old HTTP handlers only as long as they are still used by
     unmigrated call sites.

5. Cleanup (done)
   - Remove migrated HTTP endpoints and unused client code.
   - Keep `/sandbox-config` on HTTP.
   - Remove obsolete socket wiring and temporary transport proof code.
   - Update developer documentation for editing protobuf definitions and
     regenerating committed outputs.

## Test Plan

- Add socket integration tests proving the host listens on `sandd.grpc.sock`
  for daemon IPC.
- Add container socket tests proving `/run/host-services/sandd.sock` and
  `/run/host-services/sandd.grpc.sock` are wired independently.
- Add gRPC streaming tests for normal completion, cancellation, server-side
  errors, and client disconnect behavior.
- Add conversion tests for domain types to and from protobuf messages.
- Run `go test ./...` before merging each stage.

## Non-Goals

- Do not multiplex HTTP and gRPC over one Unix socket.
- Do not require compatibility with already-running sandboxes.
- Do not require `protoc` for ordinary builds or tests.
- Do not migrate `/sandbox-config` to gRPC.
