// gotestac works like "go test [...]", but instead of building and executing tests on
// your MacOS host, it cross-compiles linux test binaries and then executes the compiled tests in an
// apple linux container.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	applecontainer "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
	"github.com/banksean/apple-container/pool"
)

var (
	imageName = flag.String("image", "ghcr.io/linuxcontainers/alpine:latest", "container image")
)

func separateFlags() (knownArgs []string, unknownFlags []string) {
	// Get list of all declared flags
	declaredFlags := make(map[string]bool)
	flag.VisitAll(func(f *flag.Flag) {
		declaredFlags[f.Name] = true
	})

	// Process command line arguments
	knownArgs = append(knownArgs, os.Args[0]) // Keep program name

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]

		if !strings.HasPrefix(arg, "-") {
			// Not a flag, keep it
			knownArgs = append(knownArgs, arg)
			continue
		}

		// Extract flag name (handle both -flag and -flag=value)
		flagName := strings.TrimLeft(arg, "-")
		if idx := strings.Index(flagName, "="); idx != -1 {
			flagName = flagName[:idx]
		}

		if declaredFlags[flagName] {
			// Known flag - keep it and its value
			knownArgs = append(knownArgs, arg)
		} else {
			// Unknown flag - add to unknown list
			unknownFlags = append(unknownFlags, arg)
		}
	}

	return knownArgs, unknownFlags
}

func main() {
	// Separate known and unknown flags
	knownArgs, unknownFlags := separateFlags()

	// Temporarily modify os.Args to only include known flags
	originalArgs := os.Args

	// Restore original args
	os.Args = originalArgs

	os.Args = knownArgs

	// Now parse normally - this will only see declared flags
	flag.Parse()

	imageNameVal := flag.Lookup("image").Value.(flag.Getter).Get().(string)
	imageName = &imageNameVal

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "This command cross-compiles a linux binary from go source on your Mac, and then excutes it inside a linux container")
		fmt.Fprintf(os.Stderr, "Usage: %s <...stuff that you would normally put after `go test`>\n", os.Args[0])
		flag.PrintDefaults()
	}
	if len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(1)
	}
	ctx := context.Background()
	slog.InfoContext(ctx, "main", "args", flag.Args())

	compileArgs := flag.Args()
	runArgs := []string{}
	inRunArgs := false
	for _, arg := range unknownFlags {
		if arg == "--" {
			inRunArgs = true
			continue
		}
		if inRunArgs {
			runArgs = append(runArgs, arg)
		} else {
			compileArgs = append(compileArgs, arg)
		}
	}
	err := compileTests(ctx, compileArgs...)
	if err != nil {
		slog.ErrorContext(ctx, "main compile", "error", err)
		os.Exit(1)
	}

	status, err := applecontainer.System.Status(ctx, options.SystemStatus{})
	if err != nil {
		slog.ErrorContext(ctx, "main container system status", "error", err)
		fmt.Fprintf(os.Stderr, "container system status error: %v\n", err)
		fmt.Fprintf(os.Stderr, "You may need to run `container system start` and re-try this command\n")
		os.Exit(1)
	}
	slog.InfoContext(ctx, "main container system", "status", status)

	err = runTests(ctx, runArgs...)
	if err != nil {
		slog.ErrorContext(ctx, "main runTests", "error", err)
		os.Exit(1)
	}
}

func newTestContainer(ctx context.Context, cwd string) (string, error) {
	id, err := applecontainer.Containers.Create(ctx,
		options.CreateContainer{
			ManagementOptions: options.ManagementOptions{
				Detach:     true,
				Entrypoint: "sleep", // keep the container running
				Volume:     cwd + ":/gorunac/dev",
			},
		},
		*imageName, []string{"infinity"})
	if err != nil {
		slog.ErrorContext(ctx, "newTestContainer create container", "id", id, "error", err)
		return "", err
	}

	out, err := applecontainer.Containers.Start(ctx, options.StartContainer{}, id)
	if err != nil {
		slog.ErrorContext(ctx, "newTestContainer start container", "id", id, "out", out, "error", err)
		return "", err
	}
	slog.InfoContext(ctx, "newTestContainer started", "id", id, "out", out)

	status, err := applecontainer.Containers.Inspect(ctx, id)
	if err != nil {
		slog.ErrorContext(ctx, "newTestContainer inspect", "id", id, "error", err)
		return "", err
	}
	slog.InfoContext(ctx, "newTestContainer succeess", "id", id, "status", status)
	return id, nil
}

func initContainerPool(ctx context.Context, cwd string) (*pool.ContainerPool, error) {
	slog.InfoContext(ctx, "initContainerPool", "cwd", cwd)
	ret, err := pool.NewContainerPool(ctx, 4, func(ctx context.Context) (*pool.PooledContainer, error) {
		id, err := newTestContainer(ctx, cwd)
		if err != nil {
			return nil, err
		}
		pc := &pool.PooledContainer{
			ID: id,
		}
		return pc, nil
	}, func(ctx context.Context, pc *pool.PooledContainer) {
		out, err := applecontainer.Containers.Stop(ctx, options.StopContainer{}, pc.ID)
		slog.InfoContext(ctx, "container stop", "id", pc.ID, "error", err, "out", out)
	})
	return ret, err
}

func runTests(ctx context.Context, args ...string) error {
	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "runTests getting current working dir", "args", args, "error", err)
		return err
	}
	fileSystem := os.DirFS(cwd + "/testbin/linux")
	containerPool, err := initContainerPool(ctx, cwd)
	if err != nil {
		slog.ErrorContext(ctx, "runTests initializing container pool", "error", err)
		return err
	}
	err = fs.WalkDir(fileSystem, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if d.IsDir() {
			return nil
		}
		pooledCtr, err := containerPool.Acquire(ctx)
		if err != nil {
			return err
		}
		id := pooledCtr.ID
		slog.InfoContext(ctx, "runTests executing in container", "path", path, "id", id)
		wait, err := applecontainer.Containers.Exec(ctx,
			options.ExecContainer{}, id,
			"/gorunac/dev/testbin/linux/"+path,
			os.Environ(), os.Stdin, os.Stdout, os.Stderr, args...)

		if err != nil {
			slog.ErrorContext(ctx, "runTests container Exec", "id", id, "path", path, "error", err)
		}
		err = wait()
		if err != nil {
			slog.ErrorContext(ctx, "runTests waiting for Exec to complete", "id", id, "path", path, "error", err)
		}
		containerPool.Release(ctx, pooledCtr)
		return nil
	})

	if err != nil {
		slog.ErrorContext(ctx, "runTests fs.WalkDir", "error", err)
	}
	ctx, done := context.WithTimeout(ctx, time.Second*20)
	defer done()
	return containerPool.Shutdown(ctx)
}

func compileTests(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", append([]string{"test", "-c", "-o", "./testbin/linux/"}, args...)...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GOOS=linux")
	cmd.Env = append(cmd.Env, "GOARCH=arm64")
	slog.InfoContext(ctx, "compile", "cmd", cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "compile go test -c", "error", err, "out", string(out))
		return err
	}

	return nil
}
