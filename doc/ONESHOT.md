# Non-Interactive Oneshot Runs

`sand oneshot` runs an agent non-interactively with a single prompt and streams its output to stdout. It is useful for scripting and CI-style workflows.

Create a sandbox, run an agent prompt, then remove the sandbox:

```sh
$ sand oneshot --agent claude --rm "Summarize the open TODOs in this repo"
creating new sandbox...
executing in sandbox: small-pond
[...]
```

This creates a new sandbox, runs Claude with that prompt, writes Claude's summary to stdout, then stops and removes the container and moves the sandbox data to trash.

Create a sandbox and keep it after the agent finishes:

```sh
$ sand oneshot --agent claude "Add unit tests for the auth package and commit"
creating new sandbox...
executing in sandbox: holy-waterfall
[...]
```

This leaves the sandbox running. Adding `--stop` stops the sandbox container but keeps its filesystem. Either way, committed changes are available from the host through the git remote `sand/holy-waterfall`.

The sandbox is created fresh unless you pass `-n` to reuse an active name. Deleted names can be reused; each sandbox also has a stable ID shown by `sand ls`.

See the [command reference](../cmd/sand/HELP.md#sand-oneshot) for all `sand oneshot` flags.
