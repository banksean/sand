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

Example:

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

Note that only a *subset* of this material is passed to the sandbox environment based on what the agent launch (or other command) explicitly requires. This follows the [Principle of Least Privilege](https://en.wikipedia.org/wiki/Principle_of_least_privilege). 

See [PROFILES.md](PROFILES.md) for more information about what profiles may contain and how `sand` uses them.

## Shared caches

For shared runtime caches across sandboxes, including the Go module cache and Go build cache under the shared mise cache mount, add this to `~/.sand.yaml` or a project `.sand.yaml`:

```yaml
caches:
  mise: true
  apk: true
```

`mise: true` makes new sandboxes mount a sand-managed host cache directory for mise and for Go's `GOMODCACHE` and `GOCACHE`, so repeated `mise install`, `go mod download`, `go test`, and `go build` work can be reused across containers.

`apk: true` allows `apk add ...` commands to re-use `apk` packages that have already been downloaded by other sandbox instances. 

## Network filtering config

If you plan to use `--allowed-domains-file`, install the custom init image and BPFFS-enabled kernel first:

```sh
sand install-ebpf-support
```

See [Network filtering](NETWORK_FILTERING.md) for details.
