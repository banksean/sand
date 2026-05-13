# Configuration

`sand config ls` shows the effective configuration, merged from built-in defaults, your user-level `~/.sand.yaml`, and the project-level `./.sand.yaml`. Values include a comment indicating which source set them unless they came from hard-coded flag defaults.

Example:

```sh
$ sand config ls
app-base-dir: /Users/seanmccullough/Library/Application Support/Sand
dry-run: false
log-file: /tmp/sand/outie/log
log-level: info
timeout: 0s
exec:
  cpu: 2
  env-file: .env
  memory: 1024
git:
  diff:
    include-uncommitted: true # /Users/seanmccullough/.sand.yaml
new:
  cpu: 2
  env-file: .env
  memory: 1024
  shell: /bin/zsh
  tmux: true # /Users/seanmccullough/.sand.yaml
oneshot:
  agent: claude # ./.sand.yaml
  cpu: 2
  env-file: .env
  memory: 1024
  stop: true # ./.sand.yaml
shell:
  shell: /bin/zsh
  tmux: true # /Users/seanmccullough/.sand.yaml
```

Commit a `.sand.yaml` at the root of a project to share default flag values with your team, such as image name, allowed-domains file, CPU limits, and memory limits.

`sand` exits with an error if a user or project `.sand.yaml` contains keys that do not match known flags. This prevents typos from being silently ignored.

## Profiles

Profiles describe which host-side material a sandbox may receive. Select one at sandbox creation with `--profile <name>`; if omitted, `sand` uses `default`.

Profile policy is read from user and project `.sand.yaml` files. The project file overrides profile entries from the user file by profile name.

```yaml
profiles:
  default:
    dotfiles:
      mode: allowlist
      files:
        - source: ~/.zshrc.sand
          target: ~/.zshrc
    env:
      files:
        - path: .env
          scope: auth
      vars:
        - name: OPENAI_API_KEY
          scope: auth
    ssh:
      agentForwarding: opt-in
    git:
      config: sanitized
```

Dotfiles are not copied unless allowed by the selected profile. `dotfiles.mode: none` copies nothing; `allowlist` and `minimal` copy only entries listed under `files`. Relative `source` paths are resolved from the project directory. Symlinks are rejected unless `allowSymlink: true`, and symlink targets outside `$HOME` are rejected unless `allowOutsideHome: true`.

Environment policy uses scopes:

- `auth`: may satisfy agent launch requirements, but is not passed to plain shell, exec, or git commands.
- `project`: available to plain shell, exec, and git commands only when `--project-env` is set.
- `shell`: reserved for explicit shell exposure.
- `all`: available in every supported context.

Agent auth resolution intersects the selected profile with the agent's declared requirements and passes only the minimum required variables to the agent process. Plain shells and plain exec commands do not receive auth-scoped environment by default.

Git config policy controls `~/.gitconfig` separately from general dotfiles:

- `none`: do not write a git config.
- `sanitized`: write a filtered copy that removes credential helpers, include directives, executable aliases, and host command hooks.
- `copy`: copy `~/.gitconfig` as a normal dotfile.

## Shared caches

For shared runtime caches across sandboxes, including the Go module cache and Go build cache under the shared mise cache mount, add this to `~/.sand.yaml` or a project `.sand.yaml`:

```yaml
caches:
  mise: true
```

That makes new sandboxes mount a sand-managed host cache directory for mise and for Go's `GOMODCACHE` and `GOCACHE`, so repeated `mise install`, `go mod download`, `go test`, and `go build` work can be reused across containers.

Older `caches.go.*` settings are still accepted as compatibility aliases for the same shared mise-backed cache.

## Network filtering config

If you plan to use `--allowed-domains-file`, install the custom init image and BPFFS-enabled kernel first:

```sh
sand install-ebpf-support
```

See [Network filtering](NETWORK_FILTERING.md) for details.
