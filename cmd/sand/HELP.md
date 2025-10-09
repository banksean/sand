# `sand` command reference

Manage lightweight linux container sandboxes on MacOS.

Requires apple container CLI: https://github.com/apple/container/releases/tag/0.5.0

## Global Flags

- `-h, --help` - Show context-sensitive help.
- `--log-file` _`<log-file-path>`_ - location of log file (leave empty for a random tmp/ path) (default: `/tmp/sand/log`)
- `--log-level` _`<debug|info|warn|error>`_ - the logging level (debug, info, warn, error) (default: `info`)
- `--app-base-dir` _`<app-base-dir>`_ - root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'

## Subcommands

## `sand new`

create a new sandbox and shell into its container

**Usage:**

```
sand new [flags] [ID]
```

**Flags:**

- `-i, --image-name` _`<container-image-name>`_ - name of container image to use (default: `sandbox`)
- `-d, --docker-file-dir` _`<docker-file-dir>`_ - location of directory with docker file from which to build the image locally. Uses an embedded dockerfile if unset.
- `-s, --shell` _`<shell-command>`_ - shell command to exec in the container (default: `/bin/zsh`)
- `-c, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `-e, --env-file` _`STRING`_ - path to env file to use when creating a new shell
- `-b, --branch` - create a new git branch inside the sandbox _container_ (not on your host workdir)
- `--rm` - remove the sandbox after the shell terminates

## `sand shell`

shell into a sandbox container (and start the container, if necessary)

**Usage:**

```
sand shell [flags] [ID]
```

**Flags:**

- `-s, --shell` _`<shell-command>`_ - shell command to exec in the container (default: `/bin/zsh`)
- `-e, --env-file` _`STRING`_ - path to env file to use when creating a new shell

## `sand exec`

execute a single command in a sanbox

**Usage:**

```
sand exec [flags] <ID> <ARG>...
```

**Flags:**

- `-i, --image-name` _`<container-image-name>`_ - name of container image to use (default: `sandbox`)
- `-d, --docker-file-dir` _`<docker-file-dir>`_ - location of directory with docker file from which to build the image locally. Uses an embedded dockerfile if unset.
- `-c, --clone-from-dir` _`<project-dir>`_ - directory to clone into the sandbox. Defaults to current working directory, if unset.
- `-e, --env-file` _`STRING`_ - path to env file to use when creating a new shell
- `--rm` - remove the sandbox after the shell terminates

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
sand rm [flags] [ID]
```

**Flags:**

- `-a, --all` - remove all sandboxes

## `sand stop`

stop sandbox container

**Usage:**

```
sand stop [flags] [ID]
```

**Flags:**

- `-a, --all` - stop all sandboxes

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
sand git diff [flags] <SANDBOX-ID>
```

**Flags:**

- `-b, --branch` _`<branch name>`_ - remote branch to diff against (default: active git branch name in cwd)
- `-u, --include-uncommitted` - include uncommitted changes from sandbox working tree (default: `false`)

### `sand git status`

show git status of sandbox working tree

**Usage:**

```
sand git status <SANDBOX-ID>
```

### `sand git log`

show git log of sandbox working tree

**Usage:**

```
sand git log <SANDBOX-ID>
```

## `sand doc`

print complete command help formatted as markdown

**Usage:**

```
sand doc
```

## `sand daemon`

start or stop the sandmux daemon

**Usage:**

```
sand daemon [ACTION]
```

## `sand version`

print version infomation about this command

**Usage:**

```
sand version
```

