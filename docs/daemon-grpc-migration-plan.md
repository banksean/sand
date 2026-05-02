# Save Daemon gRPC Migration Plan

This document records the implementation plan for migrating the save daemon
protocol from HTTP-only endpoints to a staged gRPC interface.

## Current Status

The transport, socket wiring, and streaming slices have landed.

Completed:

- Added `internal/daemon/daemonpb/daemon.proto`.
- Generated and committed `daemon.pb.go` and `daemon_grpc.pb.go`.
- Added host-side gRPC listener support on `sandd.grpc.sock` while keeping
  host HTTP on `sandd.sock`.
- Added initial gRPC `Ping` and `Version` methods.
- Added a temporary Unix-socket gRPC client for the migration period.
- Added per-sandbox gRPC socket creation under `containergrpc/<id>`.
- Mounted per-sandbox gRPC sockets into containers at
  `/run/host-services/sandd.grpc.sock` while keeping HTTP mounted at
  `/run/host-services/sandd.sock`.
- Added tests for host HTTP+gRPC socket startup, per-sandbox HTTP+gRPC socket
  creation, and container socket mount wiring.
- Added gRPC streaming RPCs for `CreateSandbox` and `EnsureImage`.
- Migrated the default Unix-socket client to use gRPC for `CreateSandbox` and
  `EnsureImage`, with HTTP fallback retained for direct test clients.
- Added gRPC streaming client tests for progress, success, and streamed errors.
- Added daemon gRPC streaming integration coverage for `EnsureImage` and
  `CreateSandbox` streamed errors.

Remaining:

- Migrate remaining non-bootstrap unary methods.
- Remove migrated HTTP endpoints and temporary migration-only client code.
- Add developer documentation for regenerating protobuf output.

## Socket Strategy

Run HTTP and gRPC on separate Unix sockets. Do not add `cmux` or another
multiplexing dependency.

Host-side sockets:

- HTTP remains `sandd.sock`.
- gRPC uses `sandd.grpc.sock`.

Container-side sockets:

- HTTP remains `/run/host-services/sandd.sock`.
- gRPC uses `/run/host-services/sandd.grpc.sock`.

For per-sandbox host-side gRPC sockets, use a short directory name such as
`containergrpc/<id>`. Keep this path short enough to stay comfortably under
Unix socket path length limits.

Keep `/sandbox-config` HTTP-only. It is part of the container bootstrap path
and does not need to move to gRPC.

Existing running sandboxes do not need upgrade compatibility. The migration
may assume sandboxes are restarted onto the new socket layout.

## Code Generation

Use `protoc` only as a code generation prerequisite. Generated `.pb.go` files
must be committed to the repository so normal builds and tests do not require
`protoc`.

Developers who edit protobuf definitions need `protoc` and the Go protobuf
plugins installed locally. Developers who only build, test, or run the project
should not need them.

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

4. Unary methods
   - Move remaining non-bootstrap HTTP calls to unary gRPC methods.
   - Keep conversions between existing domain types and protobuf messages
     explicit and covered by tests.
   - Preserve old HTTP handlers only as long as they are still used by
     unmigrated call sites.

5. Cleanup
   - Remove migrated HTTP endpoints and unused client code.
   - Keep `/sandbox-config` on HTTP.
   - Remove obsolete socket wiring and temporary transport proof code.
   - Update developer documentation for editing protobuf definitions and
     regenerating committed outputs.

## Test Plan

- Add socket integration tests proving the host listens on both `sandd.sock`
  and `sandd.grpc.sock`.
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
