[![Go Reference](https://pkg.go.dev/badge/github.com/banksean/sand.svg)](https://pkg.go.dev/github.com/banksean/sand)
[![CI](https://github.com/banksean/sand/actions/workflows/queue-main.yml/badge.svg)](https://github.com/banksean/sand/actions/workflows/queue-main.yml)
![Discord](https://img.shields.io/discord/1501043729832870008)

# sand

Lightweight, disposable Linux sandboxes for AI coding agents on Apple Silicon.

Running an AI coding agent like Claude Code, Codex, Gemini, or opencode directly on your workstation is risky: agents can delete files, install packages, or make sweeping changes you did not intend. `sand` gives the agent a cloned workspace inside a local Linux container, while your real working directory stays on the host.

## What sand does

- Creates an APFS copy-on-write clone of your project and mounts it at `/app`.
- Starts a local Linux container using Apple Containerization.
- Wires up agent CLIs with `--agent claude|codex|gemini|opencode`.
- Shows sandbox status and git drift with `sand ls`, `sand git status`, and `sand git diff`.
- Keeps sandbox lifecycle separate from agent lifecycle: create, shell in, stop, restart, remove.

## Requirements

- Apple Silicon Mac
- macOS 26 or later
- Apple [`container`](https://github.com/apple/container) CLI version `0.12.0`

## Quickstart

Install with Homebrew:

```sh
brew install banksean/tap/sand
```

Start a sandboxed agent session from a project directory:

```sh
sand new -a claude
```

Or start a plain shell with no agent:

```sh
sand new my-sandbox
```

## Basic workflow

List your sandboxes from another host shell:

```sh
sand ls
```

Inspect work done in a sandbox:

```sh
sand git status my-sandbox
sand git diff my-sandbox
```

Open another shell into a sandbox:

```sh
sand shell my-sandbox
```

Stop or remove a sandbox:

```sh
sand stop my-sandbox
sand rm my-sandbox
```

## Getting changes back

Each sandbox is a separate git working tree. To bring committed sandbox work back to your original checkout, pull from the host side:

```sh
git pull sand/my-sandbox <branchname>
```

See [Git remotes between host and sandbox](doc/GIT_REMOTES.md) for the full workflow.

## More docs

- [Command reference](cmd/sand/HELP.md)
- [Common workflows](doc/WORKFLOWS.md)
- [Configuration defaults](doc/CONFIGURATION.md)
- [Sandbox profiles](doc/CONFIGURATION.md#profiles)
- [Non-interactive oneshot runs](doc/ONESHOT.md)
- [Network filtering](doc/NETWORK_FILTERING.md)
- [Architecture and trade-offs](doc/ARCHITECTURE.md)
- [Claude Code setup](doc/CLAUDE_CODE_HOWTO.md)
- [Troubleshooting](doc/TROUBLESHOOTING.md)
