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
