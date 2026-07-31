# devcontainer.json support (design proposal)

> **Status:** design proposal, not yet implemented.

## Context

`sand` today has no awareness of `.devcontainer/devcontainer.json` (the [Development Containers spec](https://containers.dev/implementors/spec/)). Every sandbox is built from a single pinned base image (`ghcr.io/banksean/sand/base:<tag>`, `internal/cli/cli.go:41` `DefaultImageName()`), customized entirely through hardcoded, per-agent first-start/start hooks (`internal/containerruntime/`). Many projects that already use VS Code Dev Containers or GitHub Codespaces carry a `devcontainer.json` describing their expected image, environment, mounts, and setup commands — teaching `sand` to read this file lets those projects get a working sandbox with zero sand-specific configuration.

Given the size of the full devcontainer.json spec and several outright conflicts with sand's design (it always mirrors the host user's uid rather than an image-declared `remoteUser`; it always mounts the project at `/app` rather than a configurable `workspaceFolder`), this plan deliberately targets a **core subset**, gated behind an explicit opt-in flag so existing behavior is unaffected by default.

**Scope decisions (confirmed):**
- Support only: `image`, `containerEnv`, `remoteEnv`, `mounts`, `postCreateCommand`, `postStartCommand`. Explicitly NOT in scope: `build` (Dockerfile builds — sand has no image-build capability at all today), `features`, `forwardPorts`/`appPort`, `remoteUser`, `workspaceFolder`, `customizations`.
- Activation: new `--devcontainer` flag, read only on `sand new` (not `exec`/`oneshot` — see below). No auto-detection.
- `remoteUser`/`workspaceFolder`: ignored entirely, not even a warning — sand's fixed host-uid user and `/app` mount are deliberate.
- `${localWorkspaceFolder}` is the only variable substitution supported, in `mounts`/`postCreateCommand`/`postStartCommand` strings — substituted with the resolved project directory.
- Missing devcontainer.json when `--devcontainer` is passed → hard error (the user explicitly asked for it).
- `--devcontainer` is a `NewCmd`-only flag, not added to the shared `SandboxCreationFlags` (avoids a flag that silently no-ops on `exec`/`oneshot`).

## New package: `internal/devcontainer/`

A parsing-only package with no sand-internal dependencies (verified against `internal/profiles/` as the closest precedent for a project-config loader).

- **`config.go`** — `Config{Image string, ContainerEnv map[string]string, RemoteEnv map[string]string, Mounts []Mount, PostCreateCommand Command, PostStartCommand Command}`.
- **`mount.go`** — `Mount{Source, Target, Type string}` with custom `UnmarshalJSON` accepting both the object form and the devcontainer shorthand string (`"source=...,target=...,type=bind"`); `(m Mount) ToMountFlag() string` renders `source=...,target=...` for the existing `--mount`-style pipeline. Reject/error on `type=volume` (sand has no named-volume concept) — only `type=bind` is supported.
- **`command.go`** — `Command{Shell string, Argv []string}` with custom `UnmarshalJSON` handling the spec's string-or-`[]string` union; `(c Command) Empty() bool`.
- **`loader.go`** — `Find(projectDir string) (string, error)` checks `<projectDir>/.devcontainer/devcontainer.json` then `<projectDir>/.devcontainer.json` (fixed project-root paths per spec, no parent-walk like `.sand.yaml`'s `FindProjectConfig`); returns a distinguishable not-found error since `--devcontainer` + missing file is a hard error per the scope decision. `Load(projectDir string) (*Config, string, error)` finds + reads + JSONC-strips + unmarshals.
- **`jsonc.go`** — `StripJSONC(data []byte) []byte`, a hand-rolled single-pass stripper for `//` and `/* */` comments and trailing commas before `}`/`]`, tracking in-string/escape state so comment-like sequences inside string literals aren't touched. No new dependency (real-world devcontainer.json files only use these two JSONC features).
- **`substitute.go`** — `SubstituteLocalWorkspaceFolder(s, projectDir string) string`, replaces literal `${localWorkspaceFolder}` occurrences; applied to `Mount.Source`/`Target` and `Command.Shell`/`Argv` entries during/after parsing.
- **Tests**: `loader_test.go`, `jsonc_test.go` (table-driven: line comments, block comments incl. one spanning lines and one containing `//` inside a string, trailing commas in objects/arrays), `command_test.go`, `mount_test.go`, `config_test.go` — following the `t.TempDir()`/`os.WriteFile` pattern already used in `internal/profiles/config_test.go`.

## CLI: `sand new --devcontainer`

- `internal/cli/new_cmd.go`: add `Devcontainer bool` directly to `NewCmd` (not `SandboxCreationFlags`), with help text noting it executes `postCreateCommand`/`postStartCommand` from the project's own file.
- In `NewCmd.Run`, right after `CloneFromDir` is resolved and before the existing `if c.ImageName == "" { c.ImageName = DefaultImageName() }` / `mc.EnsureImage(...)` block (`new_cmd.go:103-109`):
  - If `c.Devcontainer`, call `devcontainer.Load(c.CloneFromDir)`. Missing file → return the error directly (hard error, per scope decision).
  - Apply `${localWorkspaceFolder}` substitution using `c.CloneFromDir`.
  - If `c.ImageName == "" && devCfg.Image != ""`, set `c.ImageName = devCfg.Image` — so explicit `--image` still wins, devcontainer's `image` wins over `DefaultImageName()`.
  - Convert `devCfg.Mounts` to `source=...,target=...` strings and append to the mount list passed as `CreateSandboxOpts.Mounts` (merges into the same pipeline `--mount` already uses — no new mount plumbing needed).
  - Pass `devCfg.ContainerEnv`, `devCfg.RemoteEnv`, `devCfg.PostCreateCommand`, `devCfg.PostStartCommand` into the `CreateSandboxOpts` sent over gRPC (new fields, see below).
- `remoteEnv` merge: near where `agentEnv`/`plainCommandEnv` are assembled for shell/agent launch (`new_cmd.go` around the `mergeEnv(agentEnv, ...)` call, and `internal/cli/helpers.go`'s `plainCommandProjectEnv`), merge in the sandbox's persisted `RemoteEnv` map (round-tripped back from the daemon via `daemonpb.Sandbox`).

## Threading through the daemon

1. **`internal/daemon/daemonpb/daemon.proto`**: add `container_env`/`remote_env` (`map<string,string>`) and `post_create_command`/`post_create_command_shell`/`post_start_command`/`post_start_command_shell` (repeated string + string, to represent the argv-vs-shell union) to both `CreateSandboxRequest` and `Sandbox` messages. Run `task proto:generate` to regenerate `daemon.pb.go`/`daemon_grpc.pb.go`.
2. **`internal/daemon/daemon_server.go`**: add the same fields to `CreateSandboxOpts` (`:616`); pass through in `createSandbox()` (`:685`) into `boxer.NewSandboxOpts`.
3. **`internal/daemon/daemon_grpc_streams.go`**: extend `createSandboxOptsToProto`/`createSandboxOptsFromProto` for the new fields, and `proto_converters.go`'s `sandboxToProto`/`sandboxFromProto` (needed for the CLI-side `remoteEnv` merge).
4. **`internal/sandtypes/box.go`**: add to `Box` — `ContainerEnv map[string]string`, `RemoteEnv map[string]string`, `PostCreateCommand devcontainer.Command`, `PostStartCommand devcontainer.Command`.
5. **`internal/daemon/boxer/boxer.go`**: add the 4 fields to `NewSandboxOpts` (`:257`); set them on the constructed `sandtypes.Box` in `NewSandbox()` (`:362-388`, alongside the existing `Mounts`/`CPUs` assignments). Mounts need no special handling — devcontainer mounts already arrive pre-merged in `opts.Mounts` and flow through the existing `sb.prepareMountRequests(...)` call (`:328`) exactly like `--mount`.
6. **Persistence** (`internal/db/`): new migration `internal/db/migrations/000012_devcontainer_config.up.sql` / `.down.sql` adding one nullable `devcontainer_config TEXT` column to `sandboxes`, storing a JSON blob of `{ContainerEnv, RemoteEnv, PostCreateCommand, PostStartCommand}` — mirroring the existing `mount_specs TEXT` column and its `mountRequestsToNullString`/`mountRequestsFromNullString` helpers (`boxer.go:960-982`). Add matching `devcontainerConfigToNullString`/`FromNullString` helpers, wire into `SaveSandbox` (write) and the row-hydration function (read, alongside `mountRequestsFromNullString(s.MountSpecs)`). Update `internal/db/queries.sql` and run `task generate` (sqlc). This persistence is what makes a later `StartExistingContainer` correctly re-run `postStartCommand` after a restart.
7. **`internal/daemon/lifecycle/service.go`**:
   - `CreateContainer` (`:201-254`): set `ProcessOptions.Env = sb.ContainerEnv` in the `hostops.CreateContainer{...}` literal (`:246-250`) — `hostops.ProcessOptions.Env map[string]string` already exists (`internal/hostops/options.go:105-107`) and is currently unused here; `processEnvironment()` (`internal/hostops/container_ops_xpc_darwin.go`) already layers image env → env-file → process env, so this correctly bakes `containerEnv` at container-create time with the right override semantics. No new hostops field needed.
   - `StartNewContainer` (`:333-348`): after `hooks := startHooks(agentConfig.Configuration.GetFirstStartHooks(artifacts))`, append a hook built from `sb.PostCreateCommand` if non-empty.
   - `StartExistingContainer` (`:350-362`): same pattern for `sb.PostStartCommand`, appended after `GetStartHooks(artifacts)`. Since this method runs on every restart, persisting `PostStartCommand` (step 6) is sufficient for it to be correctly re-applied — no extra bookkeeping.
8. **New helper `internal/containerruntime/devcontainer_hook.go`**: `DevcontainerCommandHook(name string, cmd devcontainer.Command) (sandtypes.ContainerHook, bool)`, following the shape of `installAgentHook` (`internal/containerruntime/definition_configuration.go:67-95`). Dispatch: `cmd.Shell != ""` → `exec.ExecStream(ctx, &stdout, &stderr, "/bin/sh", "-c", cmd.Shell)` (shell-interpreted, matches devcontainer's string-form semantics); else `exec.ExecStream(ctx, &stdout, &stderr, cmd.Argv[0], cmd.Argv[1:]...)` (exec'd literally, no shell, matches devcontainer's array-form semantics). Verified against `internal/hostops/container_ops_xpc_darwin.go`'s `applyExecOptions` substitution behavior — get the shell-vs-argv dispatch right here, it's easy to accidentally double-wrap.

## Implementation order

1. `internal/devcontainer/` package + full unit tests (self-contained, build/verify with `go build ./...` early to confirm no import-cycle surprises when later imported from `sandtypes`).
2. `internal/containerruntime/devcontainer_hook.go` + test against the existing fake `HookStreamer` test double (check `internal/containerruntime/*_test.go` for one to reuse).
3. `sandtypes.Box` new fields.
4. DB migration + `queries.sql` + `task generate`.
5. `boxer.go`: `NewSandboxOpts` fields, `NewSandbox()` wiring, persistence helpers.
6. `daemon.proto` additions + `task proto:generate`.
7. `daemon_server.go`: `CreateSandboxOpts` fields + passthrough.
8. `daemon_grpc_streams.go` + `proto_converters.go`: proto conversions both directions.
9. `lifecycle/service.go`: `ProcessOptions.Env` wiring; hook-appending in both start paths.
10. `internal/cli/new_cmd.go`: `Devcontainer` flag, load/merge/substitute logic, mount merging, `remoteEnv` merge into launch env.
11. End-to-end manual verification (below).

## Verification

- Unit tests per package as listed above (`go test ./internal/devcontainer/... ./internal/containerruntime/... ./internal/daemon/boxer/...`).
- `task build` and `task test` (regenerates proto/sqlc first, per project convention).
- Manual end-to-end:
  1. Scratch project with `.devcontainer/devcontainer.json`:
     ```json
     {
       "image": "ghcr.io/banksean/sand/base:latest",
       "containerEnv": { "MY_VAR": "baked" },
       "remoteEnv": { "MY_REMOTE_VAR": "exec-time" },
       "mounts": ["source=${localWorkspaceFolder}/data,target=/data,type=bind"],
       "postCreateCommand": "echo postCreate-ran > /tmp/postcreate.marker",
       "postStartCommand": ["sh", "-c", "date >> /tmp/poststart.log"]
     }
     ```
  2. `sand new --devcontainer -d <that-dir>`; confirm the devcontainer-specified image is pulled.
  3. Once shelled in: `env | grep MY_VAR` (baked into every process), `cat /tmp/postcreate.marker` exists, `/data` is mounted and resolves to the substituted host path, `MY_REMOTE_VAR` present in the shell/agent env but absent from `container inspect`'s `Config.Env` (proves containerEnv/remoteEnv are distinct).
  4. Stop and restart the sandbox; confirm `/tmp/poststart.log` gets a second line (postStartCommand re-ran) while `/tmp/postcreate.marker`'s content is unchanged (postCreateCommand did not re-run).
  5. `sand new --devcontainer -d <dir-with-no-devcontainer.json>` → confirm it errors clearly rather than silently creating a plain sandbox.

## Critical files

- `internal/devcontainer/` (new: `config.go`, `mount.go`, `command.go`, `loader.go`, `jsonc.go`, `substitute.go` + tests)
- `internal/containerruntime/devcontainer_hook.go` (new)
- `internal/sandtypes/box.go`
- `internal/daemon/boxer/boxer.go`
- `internal/daemon/daemon_server.go`
- `internal/daemon/daemon_grpc_streams.go`, `internal/daemon/proto_converters.go`
- `internal/daemon/daemonpb/daemon.proto`
- `internal/daemon/lifecycle/service.go`
- `internal/cli/new_cmd.go`
- `internal/db/migrations/000012_devcontainer_config.up.sql` / `.down.sql`, `internal/db/queries.sql`
