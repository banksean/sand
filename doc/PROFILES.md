# Sand Profiles

## Summary

Add a `Profile` concept to describe what host-side material a sandbox may receive. Profiles should be user-facing policy, separate from agent definitions and separate from resolved runtime capabilities.

The design split is:

- `AgentCapabilities`: what an agent requires to launch, such as auth env groups.
- `Profile`: what the user permits sand to expose to a sandbox.
- Capability/profile resolver: intersects agent requirements with profile permissions and returns the minimum launch material.

Profiles should not persist secret values. They may reference env files or env variable names, but secret contents should be resolved at launch time.

## Proposed Schema

Introduce a profile type similar to:

```go
type Profile struct {
	Name     string
	Dotfiles DotfilePolicy
	Env      EnvPolicy
	SSH      SSHPolicy
	Git      GitPolicy
	Network  NetworkPolicy
}

type DotfilePolicy struct {
	Mode  DotfileMode // none, minimal, allowlist
	Files []DotfileRule
}

type DotfileRule struct {
	Source           string // e.g. ~/.gitconfig
	Target           string // e.g. ~/.gitconfig
	AllowSymlink     bool
	AllowOutsideHome bool
}

type EnvPolicy struct {
	Files []EnvFileRef
	Vars  []EnvVarRule
}

type EnvFileRef struct {
	Path  string
	Scope EnvScope // auth, project, shell, all
}

type EnvVarRule struct {
	Name  string
	Scope EnvScope
}

type SSHPolicy struct {
	AgentForwarding SSHAgentMode // off, opt-in, on
}

type GitPolicy struct {
	Config GitConfigPolicy // none, sanitized, copy
}

type NetworkPolicy struct {
	AllowedDomainsFile string
}
```

Example YAML shape:

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

## Env And Capabilities

Profiles should encapsulate `.env` references and exposure policy, not `.env` contents.

Current `EnvFile` behavior is overloaded: agent auth resolution reads it, while plain shell/exec only receive it when `--project-env` is set. Profiles should make this explicit with env scopes:

- `auth`: daemon may read the source to satisfy agent auth capabilities; only resolved required vars are passed to the agent process.
- `project`: eligible for `--project-env` shell/exec injection.
- `shell`: passed to plain shells by default, if explicitly configured.
- `all`: allowed in all supported contexts.

Resolution flow:

1. Agent declares required auth groups through `AgentCapabilities`.
2. Profile declares which env files/vars are allowed for `auth`.
3. Resolver reads only allowed sources.
4. Resolver returns only the minimum env vars satisfying one required auth group.
5. Shell/exec do not receive auth env unless the profile explicitly allows that scope.

## Dotfile Policy

Default behavior should avoid copying arbitrary host shell state.

Recommended defaults:

- Do not copy `.zshrc` by default.
- Prefer a sand-managed minimal profile.
- Allow opt-in dotfiles through an allowlist.
- Reject absolute symlink targets outside `$HOME` unless `AllowOutsideHome` is true.
- Treat `.gitconfig` specially: prefer `sanitized` over raw copy.

This reduces accidental exposure of host credentials, shell hooks, credential helpers, signing config, and host-specific command execution.

## Implementation Notes

Add profile loading to the existing configuration merge path, then thread the selected profile into sandbox creation. Workspace preparation should use `Profile.Dotfiles` instead of the current hard-coded dotfile list.

Capability resolution should accept profile policy as an input while keeping `AgentCapabilities` agent-owned and declarative.

Persist only profile name and non-secret policy references in sandbox metadata. Do not persist resolved secret values.

## Test Plan

Cover these scenarios:

- Default profile does not copy `.zshrc`.
- Allowlisted dotfiles are copied to requested targets.
- Absolute symlink targets outside `$HOME` are rejected by default.
- Agent auth resolves from profile-approved env files.
- Agent auth does not resolve from env files scoped only as `project`.
- Plain shell/exec do not receive auth env by default.
- `--project-env` continues to pass project-scoped env only.
- Sanitized git config excludes credential helpers and executable aliases.
