# `sand` command flags and subcommands

## Global Flags

- `-h, --help` - Show context-sensitive help.
- `--log-file` _`<log-file-path>`_ - location of log file (leave empty for a random tmp/ path) (default: `/tmp/sand/log`)
- `--log-level` _`<debug|info|warn|error>`_ - the logging level (debug, info, warn, error) (default: `info`)
- `--clone-root` _`<clone-root-dir>`_ - root dir to store sandbox clones of working directories (default: `/tmp/sand/boxen`)

## Subommands

## `sand shell`

create or revive a sandbox and shell into its container

**Usage:**

```
sand shell [flags] [ID]
```

**Flags:**

- `--image-name` _`<container-image-name>`_ - name of container image to use (default: `sandbox`)
- `--docker-file-dir` _`<docker-file-dir>`_ - location of directory with docker file from which to build the image locally. Uses an embedded dockerfile if unset.
- `--shell` _`<shell-command>`_ - shell command to exec in the container (default: `/bin/zsh`)
- `--rm` - remove the sandbox after the shell terminates

## `sand exec`

execute a single command in a sanbox

**Usage:**

```
sand exec [flags] <ARG>...
```

**Flags:**

- `--image-name` _`<container-image-name>`_ - name of container image to use (default: `sandbox`)
- `--docker-file-dir` _`<docker-file-dir>`_ - location of directory with docker file from which to build the image locally. Uses an embedded dockerfile if unset.
- `--rm` - remove the sandbox after the exec terminates
- `--id` _`<sandbox-id>`_ - ID of the sandbox to create, or re-attach to

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

- `--all` - remove all sandboxes

## `sand stop`

stop sandbox container

**Usage:**

```
sand stop [ID]
```

## `sand doc`

print complete command help formatted as markdown

**Usage:**

```
sand doc
```

