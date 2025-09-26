# A Go package interface to apple's container commands

This is a Go package containing some helper functions and types for use with apple's new container library.

## Requirements
- Only works on Apple hardware (of course).
- Install [`apple-container`](https://github.com/apple/container) first, since these helper functions just shell out to it. 

## `gorunac` command

This command is similar to `go run`, but it runs your go command inside a lightweight linux container, using [apple/container](https://github.com/apple/container).

- cross-compiles a linux binary from go source on your Mac
  - build on your host OS, using the Go toolchain already installed on your Mac
  - static binary means you do not need to have a go toolchain or anything else pre-installed in your container in order to run the binary
- writes binary output to `./bin/linux/{binary}` on your host OS
- creates a new container instance using a specified image (`ghcr.io/linuxcontainers/alpine:latest` by default)
- mounts a volume at `/gorunac/dev` in the container, pointed at the current working directory on your MacOS host machine
- executes `/gorunac/dev/bin/linux/{binary}` in the container, passing any extra flags that appear after `--` to `{binary}`
- stdin/stdout/stderr are routed to/from the host OS's gorunac process to the container's `{binary}` process

## `gorunac` examples

First, run the example hello world on your MacOS host:

```sh
> go run ./examples/hello foo bar
Hello World!
Operating System: darwin
args: [foo bar]
Hostname: mac.lan
```
Note that it prints out some information about the environment where it is executing.

Next, install `gorunac`:

```sh
❯ go install github.com/banksean/apple-container/cmd/gorunac
❯ gorunac
Usage: gorunac <...stuff that you would normally put after `go run`>
  -image string
        container image (default "ghcr.io/linuxcontainers/alpine:latest")
  -verbose
        verbose output
```

Now run that same hello world example, but this time use `gorunac` instead of `go run`:

```sh
❯ gorunac ./examples/hello -- foo bar
Hello World!                     
Operating System: linux
args: [foo bar]
Hostname: 5df8b394-f231-429f-be31-060e1d94f418
```

Note that when you run from source using `gorunac`, it builds the binary entirely on your MacOS host, so the only file that it needs to expose to the container is the resulting statically linked binary.

