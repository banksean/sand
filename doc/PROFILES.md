# Sand Profiles

## Summary

A `Profile` describes what host-side material a sandbox may receive. Profiles are a user-facing policy, separate from agent definitions and separate from resolved runtime permissions.

The design split is:

- `AgentRequirements`: what an agent requires in order to launch, such as auth env groups.
- `Profile`: what the user permits sand to expose to a sandbox.
- Requirements/profile resolver: intersects agent requirements with profile permissions and returns the minimum launch material.

Profiles should not persist secret values. They may reference env files or env variable names, but secret contents should be resolved at launch time rather than specified explicitly in config files.

## Schema

Example YAML shape (as it would appear at the top level in a .sand.yaml file):

```yaml
profiles:
  default:
    dotfiles:
      mode: minimal
      files:
        - source: ~/.gitconfig
          target: ~/.gitconfig
        - source: ~/.zshrc.sand
          target: ~/.zshrc
    env:
      files:
        - path: .env
          scope: auth
      vars:
        - name: OPENAI_API_KEY
          scope: auth
        - name: ANTHROPIC_API_KEY
          scope: auth
    ssh:
      agentForwarding: opt-in
    git:
      config: sanitized
```

## Env And Requirements

- `auth`: may satisfy agent launch requirements, but is not passed to plain shell, exec, or git commands.
- `project`: available to plain shell, exec, and git commands only when `--project-env` is set.
- `shell`: reserved for explicit shell exposure.
- `all`: available in every supported context.

Agent auth resolution intersects the selected profile with the agent's declared requirements and passes only the minimum required variables to the agent process. Plain shells and plain exec commands do not receive auth-scoped environment by default.

1. Agent declares required auth groups through `AgentRequirements`.
2. Profile declares which env files/vars are allowed for `auth`.
3. Resolver reads only allowed sources.
4. Resolver returns only the minimum env vars satisfying one required auth group.
5. Shell/exec do not receive auth env unless the profile explicitly allows that scope.

## Dotfile Policy

Dotfiles are not copied unless allowed by the selected profile. `dotfiles.mode: none` copies nothing; `allowlist` and `minimal` copy only entries listed under `files`. Relative `source` paths are resolved from the project directory. Symlinks are rejected unless `allowSymlink: true`, and symlink targets outside `$HOME` are rejected unless `allowOutsideHome: true`.

- Do not copy dotfiles by default.
- Prefer a sand-managed minimal profile.
- Allow opt-in dotfiles through an allowlist.
- Reject absolute symlink targets outside `$HOME` unless `AllowOutsideHome` is true.
- Treat `.gitconfig` specially: prefer `sanitized` over raw copy.

This reduces accidental exposure of host credentials, shell hooks, credential helpers, signing config, and host-specific command execution.

## Git Policy

Git config policy controls `~/.gitconfig` separately from general dotfiles:

- `none`: do not write a git config.
- `sanitized`: write a filtered copy that removes credential helpers, include directives, executable aliases, and host command hooks.
- `copy`: copy `~/.gitconfig` as a normal dotfile.
