# Bazel Remote Cache PoC

This PoC shows multiple sandboxes using one shared Bazel remote build cache.
It covers remote caching only, not remote execution.

## What It Proves

- A single `bazel-remote` service container can be reachable by all sandbox containers at `http://sand-bazel-cache.dev.local:8080`.
- Sandboxes created with `caches.bazel: true` get user and root `.bazelrc` files pointing Bazel at that cache.
- The second sandbox can reuse outputs uploaded by the first sandbox when the Bazel targets are reproducible.

## Start The Cache

```sh
scripts/bazel-remote-cache-poc.sh start
scripts/bazel-remote-cache-poc.sh status
```

The script stores cache data under:

```text
~/Library/Application Support/Sand/caches/bazel-remote
```

Set `CONTAINER_DNS_DOMAIN` if your Apple container DNS domain is not `dev.local`.

## Enable Bazel Cache For Sandboxes

Use a project or user `.sand.yaml`:

```yaml
caches:
  bazel: true
```

Or pass the global CLI flag:

```sh
sand --caches-bazel exec -d /path/to/bazel/workspace bazel-cache-a bazel build //...
```

The sandbox first-start hook writes this managed block to `/root/.bazelrc` and `/home/<user>/.bazelrc`:

```bazelrc
# sand bazel remote cache start
build --remote_cache=http://sand-bazel-cache.dev.local:8080
build --experimental_guard_against_concurrent_changes
# sand bazel remote cache end
```

The project checkout's `.bazelrc` is not modified.

## Run The Demo

```sh
scripts/bazel-remote-cache-poc.sh demo /path/to/bazel/workspace //...
```

The demo:

1. Starts the shared cache.
2. Builds the target in sandbox `bazel-cache-a`.
3. Prints cache `/status`.
4. Builds the same target in sandbox `bazel-cache-b`.
5. Prints cache `/status` again.

The first build should populate the cache. The second build should show remote cache reuse in Bazel output or reduced local action execution.

## Cleanup

```sh
scripts/bazel-remote-cache-poc.sh stop
sand rm bazel-cache-a bazel-cache-b
```

Remove the persistent cache data manually if needed:

```sh
rm -rf "$HOME/Library/Application Support/Sand/caches/bazel-remote"
```
