[![Go Reference](https://pkg.go.dev/badge/github.com/banksean/sand.svg)](https://pkg.go.dev/github.com/banksean/sand) 
[![Main Commit Queue](https://github.com/banksean/sand/actions/workflows/queue-main.yml/badge.svg)](https://github.com/banksean/sand/actions/workflows/queue-main.yml)

# TL;DR

```sh
% go install github.com/banksean/sand/cmd/sand
% sand new your-new-sandbox-name
```

You are now root, in a Linux container, with a COW clone of your current working directory mounted in the container at `/app`.

For more information about `sand`'s subcommands and other options, see [cmd/sand/HELP.md](./cmd/sand/HELP.md)

## Requirements
- Only works on Apple hardware (of course).
- Install [`sand`](https://github.com/apple/container) first, since these helper functions just shell out to it. 

# Other stuff
## `gorunac` and `gotestac` commands
```
go install github.com/banksean/sand/
cmd/gorunac

go install github.com/banksean/sand/
cmd/gotestac
```

The `gorunac` command is similar to `go run`, but it runs your go command inside a lightweight linux container, using [apple/container](https://github.com/apple/container).

- cross-compiles a linux binary from go source on your Mac
  - build on your host OS, using the Go toolchain already installed on your Mac
  - static binary means you do not need to have a go toolchain or anything else pre-installed in your container in order to run the binary
- writes binary output to `./bin/linux/{binary}` on your host OS
- creates a new container instance using a specified image (the `--image` flag value is `ghcr.io/linuxcontainers/alpine:latest` by default, but most linux OCI images should work)
- mounts a volume at `/gorunac/dev` in the container, pointed at the current working directory on your MacOS host machine
- executes `/gorunac/dev/bin/linux/{binary}` in the container, passing any extra flags that appear after `--` to `{binary}`
- stdin/stdout/stderr are routed to/from the host OS's gorunac process to the container's `{binary}` process

Similar to `gorunac`, `gotestac` will run unit tests in a linux container after cross-compiling the test binaries first on your MacOS host.  
