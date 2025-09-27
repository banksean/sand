[![Go Reference](https://pkg.go.dev/badge/github.com/banksean/apple-container.svg)](https://pkg.go.dev/github.com/banksean/apple-container)

# A Go package interface to apple's container commands

This Go package contains 
- Commands `gorunac` and `gotestac`
  - Quickly run and test go code inside lightweight linux containers, using apple's new container library. 
  - Similar to `go run` and `go test`, they build executables from source code located on your MacOS host
  - However, `gorunac` and `gotestac` cross-compile to linux, then mount those output files inside the linux containers and execute them there instead of the MacOS host. No container-side go toolchain is required.
  - `gorunac` pipes stdio between host and container process.
- Some helper functions and types for use with apple's new container library. `gorunac` and `gotestac` were written as demonstrations of how these packages can be used.

## Requirements
- Only works on Apple hardware (of course).
- Install [`apple-container`](https://github.com/apple/container) first, since these helper functions just shell out to it. 

## `gorunac` and `gotestac` commands

The `gorunac` command is similar to `go run`, but it runs your go command inside a lightweight linux container, using [apple/container](https://github.com/apple/container).

- cross-compiles a linux binary from go source on your Mac
  - build on your host OS, using the Go toolchain already installed on your Mac
  - static binary means you do not need to have a go toolchain or anything else pre-installed in your container in order to run the binary
- writes binary output to `./bin/linux/{binary}` on your host OS
- creates a new container instance using a specified image (the `--image` flag value is `ghcr.io/linuxcontainers/alpine:latest` by default, but most linux OCI images should work)
- mounts a volume at `/gorunac/dev` in the container, pointed at the current working directory on your MacOS host machine
- executes `/gorunac/dev/bin/linux/{binary}` in the container, passing any extra flags that appear after `--` to `{binary}`
- stdin/stdout/stderr are routed to/from the host OS's gorunac process to the container's `{binary}` process

Similar to `gorunac`, `gotestac` will run unit tests in a linux container after cross-compiling the test binaries first on your MacOS host.  Note: `gotestac` is a fairly naive test runner (more of a proof-of-concept). It starts a new container instance for each test binary invocation, and it runs all of them in series.

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

## `gotestac` examples

```sh
# First, run all the tests on the MacOS host and see that they pass:

>go test ./... -count 1
?       sketch.dev/browser      [no test files]
ok      sketch.dev/claudetool   0.692s
ok      sketch.dev/claudetool/bashkit   0.524s
[...]
ok      sketch.dev/loop 3.420s
ok      sketch.dev/loop/server  4.272s
ok      sketch.dev/loop/server/gzhandler        2.752s
?       sketch.dev/mcp  [no test files]
ok      sketch.dev/skabandclient        3.512s

# Now run the same tests in linux containers using gotestac, and see that some tests fail in that environment but not on the MacOS host:

>go install github.com/banksean/apple-container/cmd/gotestac
>gotestac ./...
Running ant.test...
PASS                             
Running bashkit.test...          
PASS                             
Running browse.test...           
2025/09/27 20:47:37 Browser closed
2025/09/27 20:47:37 Browser closed
2025/09/27 20:47:37 Browser closed
--- FAIL: TestBrowserInitialization (0.00s)
    browse_test.go:121: Failed to initialize browser: failed to start browser (please apt get chromium or equivalent): exec: "google-chrome": executable file not found in $PATH
2025/09/27 20:47:37 Browser closed
--- FAIL: TestNavigateTool (0.00s)
    browse_test.go:177: Error running navigate tool: failed to start browser (please apt get chromium or equivalent): exec: "google-chrome": executable file 
...
Running skabandclient.test...    
PASS                             
Running sketch.test...           
PASS
```
