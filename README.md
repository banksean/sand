[![Go Reference](https://pkg.go.dev/badge/github.com/banksean/sand.svg)](https://pkg.go.dev/github.com/banksean/sand) 
[![Main Commit Queue](https://github.com/banksean/sand/actions/workflows/queue-main.yml/badge.svg)](https://github.com/banksean/sand/actions/workflows/queue-main.yml)

# sand

Lightweight, disposable Linux sandboxes for AI coding agents on Apple Silicon.

## Why sand?

Running an AI coding agent like Claude Code, Codex, Gemini, or opencode directly on your workstation is risky: agents can delete files, install packages, or make sweeping changes you didn't intend. But setting up isolation manually with `docker` or `apple/container` requires configuring bind mounts, SSH keys, DNS, agent CLIs, and credentials from scratch every time.

`sand` handles all of that in a single command.

### What you get that `docker` or `apple/container` alone don't provide

| | docker / container | sand |
|---|---|---|
| Isolated workspace (not your live dir) | manual bind mount setup | automatic CoW clone |
| SSH key forwarding | manual | opt-in |
| DNS name for container | manual | automatic |
| Agent CLI setup | manual | `--agent claude\|codex\|gemini\|opencode` |
| Git-awareness | none | shows FROM GIT vs CURRENT GIT in `sand ls` |
| Host files safe from `rm -rf` | risky with live bind mount | CoW clone, host unaffected |
| Network access, exfil control | manual | kernel (eBPF) layer egress filtering with `--allowed-domains-file` |

- **Isolated workspace**: your project is cloned into the container using APFS copy-on-write (`clonefile`), so it's instant, space-efficient, and changes inside the container cannot affect your real working directory.
- **One-command agent launch**: `sand new -a claude` starts a sandboxed agent session with your workspace, credentials, and agent CLI all wired up. Interactive agent support currently includes `claude`, `codex`, `gemini`, and `opencode`.
- **Lightweight**: built on [Apple Containerization](https://github.com/apple/containerization) — hardware-isolated VMs via Apple Silicon with low memory overhead and fast start times.
- **Git-aware**: `sand ls` shows which git commit each sandbox was created from vs. where it is now. When you pass `--ssh-agent`, git over SSH works inside the container without leaving credentials lying around.
- **Familiar lifecycle**: treat sandboxes like git branches or tmux sessions — create, list, stop, delete.

# TL;DR

```sh
brew install banksean/tap/sand
sand new -a claude
```

For DNS egress filtering with `--allowed-domains-file`, run `sand install-ebpf-support` first.

## Installation

Note: `sand` only runs on Apple Silicon Macs with macOS 26 or later, and depends on Apple's `container` CLI.

Install via Homebrew (recommended):
```sh
brew install banksean/tap/sand
```

Install from source:
```sh
go install github.com/banksean/sand/cmd/...
```

## Usage

Manual, no agent:
```sh
> sand new my-sandbox
container hostname: my-sandbox
⚡ ⌨️  # shell prompt, go crazy, rm -rf whatever
```

Use with a coding agent, like `claude`, `codex`, `gemini`, or `opencode`:
```sh
> sand new -a claude
container hostname: shy-snow

 ▐▛███▜▌   Claude Code v2.1.71
▝▜█████▛▘  Sonnet 4.6 · API Usage Billing
  ▘▘ ▝▝    /app

──────────────────────────────────────────────────── ▪▪▪ ─
❯  
```

And in another shell window from your host MacOS, use `sand ls` to list the sandboxes:
```sh
> sand ls
SANDBOX NAME  STATUS   FROM DIR     FROM GIT          CURRENT GIT       IMAGE NAME
shy-snow      running  ~/code/sand  *2be518ca readme  *2be518ca readme  claude:latest
my-sandbox    running  ~/code/sand  *2be518ca readme  *2be518ca readme  default:latest
```
## Details

Under the hood, the `sand new` command:
- creates a copy-on-write clone your current working directory (traversing up to git root, if necessary)
- creates a local linux container with that cloned working directory mounted at `/app` 
- configures the container with keys for bidirectional ssh authentication
- makes the container visible to your host OS via DNS
- ssh's you into that container
- runs your coding agent's CLI (if you specified an agent)

# Design

## Trade-offs

`sand` runs entirely on your workstation — no remote hosting, no third-party access to your files. The trade-off is that it is bounded by your local hardware resources.

`sand` is agent-agnostic, which means it can't exploit deep agent-specific features. The upside is that sandbox lifecycles are independent of agent session lifecycles, and sandboxes are equally useful for manual coding without any agent.

`sand` achieves speed partly by doing less — it won't automate `git` or `tmux` workflows beyond what's needed for sandbox management. You can create a new sandbox from an existing sandbox as easily as branching in git.

## Implementation Choices

- Isolation Model: [Apple Containerization](https://github.com/apple/containerization)
  - hardware isolation via Apple Silicon
  - low memory overhead, fast start times
  - kernel based on [Kata](https://katacontainers.io/)
  - used via Apple's [`container` CLI](https://github.com/apple/container), currently requiring version `0.11.0`
  - supported on macOS 26 and up
- Filesystem:
    - Base container image: Minimal, with some dynamic provisioning based on which agent you're using
    - Agent workspaces: `/app` is mounted from the APFS CoW clone, must be same APFS disk as the original project dir
    - Host filesystem access: limited to the CoW clone directory. (Apple Containerization uses virtiofs to bridge the macOS-to-VM boundary, and then uses a bind mount inside the Linux micro-VM to present that path to the container.)
- Execution interface: 
  - A CLI with a fast exec path, and a session path for interactive use
  - A daemon on the host OS handles sandbox lifecycle management, with the CLI just thin wrapper that makes IPC calls to the daemon
  - The container environment *also* has a `sand` CLI, which uses container-to-host networking to make IPCs to that same daemon

Implementation decisions I'm still investigating:
- Lifecycle & Pooling Strategy: Currently `sand` will just spawn containers on demand, with no pooling. It does not monitor container activity or utilization etc. You can stop and start sandbox containers manually, but `sand` does not try to do any of that automatically yet.
- Network Topology: Per-container isolated network vs. shared host network vs. a managed bridge, vs ... something else perhaps? A lot of this aspect is constrained by what MacOS and Apple Containers will support.

# Usage Notes

See [cmd/sand/HELP.md](./cmd/sand/HELP.md) for a full CLI reference.

## You work with a sandboxed clone of `./`
The sandbox starts out with a clone of your current directory from MacOS, mounted as `/app` inside the container. 

This cloning operation actually uses much less disk space than a full copy of the original directory, because `sand` clones it using copy-on-write (via APFS's `clonefile(2)` call). Additional disk space is only consumed by the sandbox when the cloned files are modified.

Note: The original files on your MacOS host filesystem are not affected by changes made to the clones of those files inside the sandbox.  You can `rm -rf /` in the sandbox container and it won't affect your original working directory at all.

## Getting changes out of the sandbox

You can use `git` commands to push changes from the container to github, or wherever your origin is. 

Git ssh authentication can pass from your MacOS host through `sand` containers via `ssh-agent`. Commands can opt in with `--ssh-agent`. That means if the git checkout on your MacOS host is authenticated with ssh (`git remote -v origin` prints something that starts with `git@github.com:...`), then you don't need to log in again inside the container to make git push/pull to work.

Using `ssh-agent` also means you don't have to leave copies of your github credentials scattered around in places where they shouldn't be.

See [doc/GIT_REMOTES.md](doc/GIT_REMOTES.md) for a more detailed explanation of how `sand` uses git locally to link the MacOS-side clones back to your original working directory.  If you are a git enthusiast, you can probably figure out how move changes around between your MacOS host and your sandbox containers without involving github at all.

## Non-interactive (one-shot) agent runs

`sand oneshot` runs an agent non-interactively with a single prompt and streams its output to stdout. Useful for scripting or CI pipelines:

```sh
$ sand oneshot --agent claude --rm "Summarize the open TODOs in this repo"
creating new sandbox...
executing in sanbox: small-pond
[...]
```

Will create new sandbox, run claude the that prompt, write Claude's summary to stdout and then remove the sandbox.

```sh
$ sand oneshot --agent claude "Add unit tests for the auth package and commit"
creating new sandbox...
executing in sanbox: holy-waterfall
[...]
```

Will create a new sandbox, have Claude add unit tests and commit them, leaving the sandbox running. Adding a `--stop` flag will stop the sandbox container. Either way, whatever changes Claude committed will be available in the git remote `sand/holy-waterfall` (which you can pull using regular git commands).

The sandbox is created fresh (or reused by name with `-n`). Pass `--rm` to tear it down automatically when the agent finishes, or `--stop` to just stop it.

## Configuration defaults

`sand config ls` shows the effective configuration, merged from built-in defaults, your user-level `~/.sand.yaml`, and the project-level `./.sand.yaml`, with a comment next to each value indicating which source set it (unless that value came from the hard-coded flag defaults).

Here's mine, for example. I use a combination of global defaults in `~/.sand.yaml` and project defaults in `.sand.yaml`: 
```sh
$ sand config ls
app-base-dir: /Users/seanmccullough/Library/Application Support/Sand
dry-run: false
log-file: /tmp/sand/outie/log
log-level: info
timeout: 0s
exec:
  cpu: 2
  env-file: .env
  memory: 1024
git:
  diff:
    include-uncommitted: true # /Users/seanmccullough/.sand.yaml
new:
  cpu: 2
  env-file: .env
  memory: 1024
  shell: /bin/zsh
  tmux: true # /Users/seanmccullough/.sand.yaml
oneshot:
  agent: claude # ./.sand.yaml
  cpu: 2
  env-file: .env
  memory: 1024
  stop: true # ./.sand.yaml
shell:
  shell: /bin/zsh
  tmux: true # /Users/seanmccullough/.sand.yaml
```

You can commit a `.sand.yaml` at the root of a project to share default flag values (image name, allowed-domains file, CPU/memory limits, etc.) with your team.

If you plan to use `--allowed-domains-file`, install the custom init image and BPFFS-enabled kernel first:

```sh
sand install-ebpf-support
```

For shared runtime caches across sandboxes, including the Go module cache and Go build cache under the shared mise cache mount, add this to `~/.sand.yaml` or a project `.sand.yaml`:

```yaml
caches:
  mise: true
```

That makes new sandboxes mount a sand-managed host cache directory for mise and for Go's `GOMODCACHE`/`GOCACHE`, so repeated `mise install`, `go mod download`, `go test`, and `go build` work can be reused across containers. Older `caches.go.*` settings are still accepted as compatibility aliases for the same shared mise-backed cache.

## Some other handy commands

```sh
$ sand --help # a much more complete list of commands and flags
$ sand ls # lists your current sandboxes
$ sand git status your-sandbox-name # prints the results of running "git status" in the sandbox's working directory
$ sand git diff your-sandbox-name # compares your working directory to the sandbox's clone of your working directory
$ sand vsc your-sandbox-name # launches a vscode window, connected "remotely" to your-sandbox-name
$ sand shell your-sandbox-name # open a new shell into the your-sandbox-name's container
$ sand stop your-sandbox-name # stops the sandbox container, but does *not* delete its filesystem
$ sand rm your-sandbox-name # stops and removes the container, and *does* remove the sandbox's filesystem.
```

For more information about `sand`'s subcommands and other options, see [cmd/sand/HELP.md](./cmd/sand/HELP.md).

## Requirements
- Only works on Apple hardware (of course).
- Apple Silicon Mac
- macOS 26 or later
- Install [`apple/container`](https://github.com/apple/container) CLI version `0.11.0` first
