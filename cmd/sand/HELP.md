# `sand` command reference

Manage lightweight linux container sandboxes on MacOS.

Requires apple container CLI: https://github.com/apple/container/releases/tag/0.12.0

## Global Flags

- `-h, --help` - Show context-sensitive help.
- `--log-file` _`<log-file-path>`_ - location of log file (leave empty for a random tmp/ path) (default: `/tmp/sand/outie/log`)
- `--log-level` _`<debug|info|warn|error>`_ - the logging level (debug, info, warn, error) (default: `info`)
- `--app-base-dir` _`<app-base-dir>`_ - root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'
- `--timeout` _`0s`_ - if set to anything other than 0s, overrides the default timeout for an operation (default: `0s`)
- `--version` - Print version and exit.
- `--dry-run` - just print out the operations instead of executing them (default: `false`)
- `--caches-mise` - enable mise cache (default: `true`)
- `--caches-apk` - enable apk cache (default: `true`)

## Subcommands

## `sand completion`

Outputs shell code for initialising tab completions

**Usage:**

```
sand completion [flags] [SHELL]
```

**Flags:**

- `-c, --code` - Generate the initialization code

## `sand new`

create a new sandbox and shell into its container

**Usage:**

```
sand new [flags] [SANDBOX-NAME]
```

**Flags:**

- `--ssh-agent` - enable ssh-agent forwarding for the container
- `-i, --image` _`<container-image-name>`_ - name of container image to use
- `-d, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `--profile` _`<profile-name>`_ - profile policy from .sand.yaml to associate with the sandbox (default: `default`)
- `-e, --env-file` _`<file-path>`_ - legacy env file path used when no default profile is configured (default: `.env`)
- `--rm` - remove the sandbox after the command terminates
- `--allowed-domains-file` _`<file-path>`_ - path to allowed-domains.txt file for DNS egress filtering (overrides the init image default)
- `-v, --volume` _`<host-path:container-path>,...`_ - bind mount a volume (can be specified multiple times)
- `--cpu` _`2`_ - number of CPUs to allocate to the container (default: `2`)
- `--memory` _`1024`_ - how much memory in MiB to allocate to the container (default: `1024`)
- `--project-env` - pass project-scoped profile env to plain shell/exec/git commands
- `-s, --shell` _`<shell-command>`_ - shell command to exec in the container (default: `/bin/zsh`)
- `-t, --tmux` - create or reconnect to a container-side tmux session
- `--atch` - create or reconnect to a container-side atch session
- `-a, --agent` _`<claude|codex|gemini|opencode>`_ - name of coding agent to use
- `-b, --branch` - create a new git branch, with the same name as the sandbox, inside the sandbox _container_ (not on your host workdir) (default: `false`)
- `--username` _`STRING`_ - name of default user to create (defaults to $USER)
- `--uid` _`STRING`_ - id of default user to create (defaults to $UID)

## `sand oneshot`

run an AI agent non-interactively with a prompt

**Usage:**

```
sand oneshot [flags] <PROMPT>
```

**Flags:**

- `--ssh-agent` - enable ssh-agent forwarding for the container
- `-i, --image` _`<container-image-name>`_ - name of container image to use
- `-d, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `--profile` _`<profile-name>`_ - profile policy from .sand.yaml to associate with the sandbox (default: `default`)
- `-e, --env-file` _`<file-path>`_ - legacy env file path used when no default profile is configured (default: `.env`)
- `--rm` - remove the sandbox after the command terminates
- `--allowed-domains-file` _`<file-path>`_ - path to allowed-domains.txt file for DNS egress filtering (overrides the init image default)
- `-v, --volume` _`<host-path:container-path>,...`_ - bind mount a volume (can be specified multiple times)
- `--cpu` _`2`_ - number of CPUs to allocate to the container (default: `2`)
- `--memory` _`1024`_ - how much memory in MiB to allocate to the container (default: `1024`)
- `-a, --agent` _`<claude|codex|gemini|opencode>`_ - coding agent to use
- `--username` _`STRING`_ - name of default user to create (defaults to $USER)
- `--uid` _`STRING`_ - id of default user to create (defaults to $UID)
- `-n, --sandbox-name` _`<name>`_ - name of the sandbox to use (generated if omitted)
- `--stop` - stop the container when the command completes

## `sand shell`

shell into a sandbox container (and start the container, if necessary)

**Usage:**

```
sand shell [flags] <SANDBOX-NAME>
```

**Flags:**

- `-s, --shell` _`<shell-command>`_ - shell command to exec in the container (default: `/bin/zsh`)
- `-t, --tmux` - create or reconnect to a container-side tmux session
- `--atch` - create or reconnect to a container-side atch session
- `--project-env` - pass project-scoped profile env to plain shell/exec/git commands
- `--ssh-agent` - enable ssh-agent forwarding for the container

## `sand exec`

execute a single command in a sanbox

**Usage:**

```
sand exec [flags] <SANDBOX-NAME> <ARG>...
```

**Flags:**

- `--ssh-agent` - enable ssh-agent forwarding for the container
- `-i, --image` _`<container-image-name>`_ - name of container image to use
- `-d, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `--profile` _`<profile-name>`_ - profile policy from .sand.yaml to associate with the sandbox (default: `default`)
- `-e, --env-file` _`<file-path>`_ - legacy env file path used when no default profile is configured (default: `.env`)
- `--rm` - remove the sandbox after the command terminates
- `--allowed-domains-file` _`<file-path>`_ - path to allowed-domains.txt file for DNS egress filtering (overrides the init image default)
- `-v, --volume` _`<host-path:container-path>,...`_ - bind mount a volume (can be specified multiple times)
- `--cpu` _`2`_ - number of CPUs to allocate to the container (default: `2`)
- `--memory` _`1024`_ - how much memory in MiB to allocate to the container (default: `1024`)
- `--project-env` - pass project-scoped profile env to plain shell/exec/git commands
- `--username` _`STRING`_ - name of user to exec as (defaults to $USER)
- `--uid` _`STRING`_ - id of user to exec as (defaults to $UID)

## `sand ls`

list sandboxes

**Usage:**

```
sand ls
```

## `sand log`

print sandbox lifecycle and daemon events

**Usage:**

```
sand log <SANDBOX-NAME>
```

## `sand rm`

soft-delete sandbox container and move its clone directory to trash

**Usage:**

```
sand rm [flags] [SANDBOX-NAMES]...
```

**Flags:**

- `-a, --all` - all sandboxes
- `-f, --force` - move sandbox to trash without confirmation

## `sand stop`

stop sandbox container

**Usage:**

```
sand stop [flags] [SANDBOX-NAMES]...
```

**Flags:**

- `-a, --all` - all sandboxes

## `sand start`

start sandbox container

**Usage:**

```
sand start [flags] [SANDBOX-NAMES]...
```

**Flags:**

- `-a, --all` - all sandboxes
- `--ssh-agent` - enable ssh-agent forwarding for the container

## `sand git`

git operations with sandboxes

**Usage:**

```
sand git
```

### `sand git diff`

diff current working directory with sandbox clone

**Usage:**

```
sand git diff [flags] <SANDBOX-NAME>
```

**Flags:**

- `-b, --branch` _`<branch name>`_ - remote branch to diff against (default: active git branch name in cwd)
- `-u, --include-uncommitted` - include uncommitted changes from sandbox working tree (default: `false`)

### `sand git status`

show git status of sandbox working tree

**Usage:**

```
sand git status <SANDBOX-NAME>
```

### `sand git log`

show git log of sandbox working tree

**Usage:**

```
sand git log <SANDBOX-NAME>
```

## `sand doc`

print complete command help formatted as markdown

**Usage:**

```
sand doc
```

## `sand build-info`

print version infomation about this command

**Usage:**

```
sand build-info
```

## `sand vsc`

launch a vscode remote window connected to the sandbox's container

**Usage:**

```
sand vsc <SANDBOX-NAME>
```

## `sand install-ebpf-support`

install the BPFFS-enabled kernel build

**Usage:**

```
sand install-ebpf-support
```

## `sand export-fs`

export a container's filesystem

**Usage:**

```
sand export-fs [flags] <SANDBOX-NAME>
```

**Flags:**

- `-o, --output-path` _`<host FS path>`_ - where to write the exported FS archive to

## `sand stats`

list container stats for sandboxes

**Usage:**

```
sand stats [flags] [SANDBOX-NAMES]...
```

**Flags:**

- `-a, --all` - all sandboxes

## `sand config`

list, get, or set default values for flags

**Usage:**

```
sand config
```

### `sand config ls`

show effective configuration with sources

**Usage:**

```
sand config ls
```
