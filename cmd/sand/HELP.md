# `sand` command reference

Manage lightweight linux container sandboxes on MacOS.

Requires apple container CLI: https://github.com/apple/container/releases/tag/0.10.0

## Global Flags

- `-h, --help` - Show context-sensitive help.
- `--log-file` _`<log-file-path>`_ - location of log file (leave empty for a random tmp/ path) (default: `/tmp/sand/outie/log`)
- `--log-level` _`<debug|info|warn|error>`_ - the logging level (debug, info, warn, error) (default: `info`)
- `--app-base-dir` _`<app-base-dir>`_ - root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'

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

- `-i, --image-name` _`<container-image-name>`_ - name of container image to use (default: `ghcr.io/banksean/sand/default:latest`)
- `-d, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `-e, --env-file` _`".env"`_ - path to env file to use when creating a new shell (default: `.env`)
- `--rm` - remove the sandbox after the command terminates
- `-s, --shell` _`<shell-command>`_ - shell command to exec in the container (default: `/bin/zsh`)
- `-c, --cloner` _`<claude|default|opencode>`_ - name of workspace cloner to use (default: `default`)
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

- `-i, --image-name` _`<container-image-name>`_ - name of container image to use (default: `ghcr.io/banksean/sand/default:latest`)
- `-d, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `-e, --env-file` _`".env"`_ - path to env file to use when creating a new shell (default: `.env`)
- `--rm` - remove the sandbox after the command terminates

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

