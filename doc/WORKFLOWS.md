# Common Workflows

This page gives task-oriented examples. For every command and flag, see the [command reference](../cmd/sand/HELP.md).

## Start a sandbox

Start a manual shell:

```sh
sand new my-sandbox
```

Start with a coding agent:

```sh
sand new -a claude
```

Interactive agent support currently includes `claude`, `codex`, `gemini`, and `opencode`.

## Inspect sandboxes

List current sandboxes:

```sh
sand ls
```

Show git status in a sandbox:

```sh
sand git status my-sandbox
```

Compare your host working directory to a sandbox:

```sh
sand git diff my-sandbox
```

Include uncommitted sandbox changes:

```sh
sand git diff --include-uncommitted my-sandbox
```

## Re-enter a sandbox

Open another shell into a sandbox container:

```sh
sand shell my-sandbox
```

Launch VS Code connected to a sandbox:

```sh
sand vsc my-sandbox
```

## Stop or remove a sandbox

Stop the container without deleting its filesystem:

```sh
sand stop my-sandbox
```

Start it again later:

```sh
sand start my-sandbox
```

Remove the container and move its sandbox filesystem to sand's trash:

```sh
sand rm my-sandbox
```

## Move changes back to the host

Commit changes inside the sandbox, then pull them from the host checkout:

```sh
git pull sand/my-sandbox <branchname>
```

See [Git remotes between host and sandbox](GIT_REMOTES.md) for the detailed host/sandbox git model.
