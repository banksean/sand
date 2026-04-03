# `sand` command reference

Manage lightweight linux container sandboxes on MacOS.

Requires apple container CLI: https://github.com/apple/container/releases/tag/0.11.0

## Global Flags

- `-h, --help` - Show context-sensitive help.
- `--log-file` _`<log-file-path>`_ - location of log file (leave empty for a random tmp/ path) (default: `/tmp/sand/outie/log`)
- `--log-level` _`<debug|info|warn|error>`_ - the logging level (debug, info, warn, error) (default: `info`)
- `--app-base-dir` _`<app-base-dir>`_ - root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'
- `--timeout` _`0s`_ - if set to anything other than 0s, overrides the default timeout for an operation (default: `0s`)

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

- `-i, --image-name` _`<container-image-name>`_ - name of container image to use
- `-d, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `-e, --env-file` _`".env"`_ - path to env file to use when creating a new shell (default: `.env`)
- `--rm` - remove the sandbox after the command terminates
- `--allowed-domains-file` _`<file-path>`_ - path to allowed-domains.txt file for DNS egress filtering (overrides the init image default)
- `-v, --volume` _`<host-path:container-path>,...`_ - bind mount a volume (can be specified multiple times)
- `--cpu` _`2`_ - number of CPUs to allocate to the container (default: `2`)
- `--memory` _`1024`_ - how much memory in MiB to allocate to the container (default: `1024`)
- `-s, --shell` _`<shell-command>`_ - shell command to exec in the container (default: `/bin/zsh`)
- `-a, --agent` _`<claude|default|opencode>`_ - name of coding agent to use (default: `default`)
- `-b, --branch` - create a new git branch inside the sandbox _container_ (not on your host workdir)
- `-p, --prompt` _`<prompt>`_ - start the agent with this prompt in non-interactive (one-shot) mode and return immediately

## `sand shell`

shell into a sandbox container (and start the container, if necessary)

**Usage:**

```
sand shell [flags] <SANDBOX-NAME>
```

**Flags:**

- `-s, --shell` _`<shell-command>`_ - shell command to exec in the container (default: `/bin/zsh`)

## `sand exec`

execute a single command in a sanbox

**Usage:**

```
sand exec [flags] <SANDBOX-NAME> <ARG>...
```

**Flags:**

- `-i, --image-name` _`<container-image-name>`_ - name of container image to use
- `-d, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `-e, --env-file` _`".env"`_ - path to env file to use when creating a new shell (default: `.env`)
- `--rm` - remove the sandbox after the command terminates
- `--allowed-domains-file` _`<file-path>`_ - path to allowed-domains.txt file for DNS egress filtering (overrides the init image default)
- `-v, --volume` _`<host-path:container-path>,...`_ - bind mount a volume (can be specified multiple times)
- `--cpu` _`2`_ - number of CPUs to allocate to the container (default: `2`)
- `--memory` _`1024`_ - how much memory in MiB to allocate to the container (default: `1024`)

## `sand ls`

list sandboxes

**Usage:**

```
sand ls
```

## `sand rm`

remove sandbox container and its clone directory

**Usage:**

```
sand rm [flags] [SANDBOX-NAME]
```

**Flags:**

- `-a, --all` - all sandboxes

## `sand stop`

stop sandbox container

**Usage:**

```
sand stop [flags] [SANDBOX-NAME]
```

**Flags:**

- `-a, --all` - all sandboxes

## `sand start`

start sandbox container

**Usage:**

```
sand start [flags] [SANDBOX-NAME]
```

**Flags:**

- `-a, --all` - all sandboxes

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

## `sand version`

print version infomation about this command

**Usage:**

```
sand version
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

## `sand export-image`

export a container image based on a stopped sandbox

**Usage:**

```
sand export-image [flags] <SANDBOX-NAME>
```

**Flags:**

- `-i, --image-name` _`<container-image-name>`_ - name of container image to export

## `sand stats`

list container stats for sandboxes

**Usage:**

```
sand stats [flags] [SANDBOX-NAME]
```

**Flags:**

- `-a, --all` - all sandboxes

