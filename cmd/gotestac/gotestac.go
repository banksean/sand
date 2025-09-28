// gotestac works like "go test [...]", but instead of building and executing tests on
// your MacOS host, it cross-compiles linux test binaries and then executes the compiled tests in an
// apple linux container.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	applecontainer "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
	"github.com/banksean/apple-container/pool"
)

var (
	verbose   = flag.Bool("verbose", false, "verbose output")
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
	verboseVal := flag.Lookup("verbose").Value.(flag.Getter).Get().(bool)
	verbose = &verboseVal
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
	if *verbose {
		fmt.Fprintf(os.Stderr, "args: %v\n", flag.Args())
	}
	ctx := context.Background()

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
	err := compile(compileArgs...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compile error: %v\n", err)
		os.Exit(1)
	}

	status, err := applecontainer.System.Status(ctx, options.SystemStatus{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "container system status error: %v\n", err)
		fmt.Fprintf(os.Stderr, "You may need to run `container system start` and re-try this command\n")
		os.Exit(1)
	}
	if *verbose {
		fmt.Fprintf(os.Stderr, "container system status: %s\n", status)
	}

	err = runTests(ctx, runArgs...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run error: %v\n", err)
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
		if *verbose {
			fmt.Fprintf(os.Stderr, "creating container %s: %s\n", id, err)
		}
		return "", err
	}
	log.Printf("initContainerPool.New: %s", id)
	out, err := applecontainer.Containers.Start(ctx, options.StartContainer{}, id)
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "starting container %s: %s\n%s\n", id, err, out)
		}
		return "", err
	}
	if *verbose {
		fmt.Fprintf(os.Stderr, "container id %s started. Output:\n%s\n", id, out)
	}

	timeout := 5 * time.Second
	ctxLogs, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	logReader, logWait, err := applecontainer.Containers.Logs(ctxLogs, options.ContainerLogs{Boot: true}, id)
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "getting container logs %s: %s\n", id, err)
		}
		return "", err
	}
	defer logWait()

	if *verbose {
		fmt.Printf("Scanning container %s logs, with a %v timeout...\n", id, timeout)
		go func() {
			logScanner := bufio.NewScanner(logReader)
			for logScanner.Scan() {
				fmt.Printf("Log line: %s\n", logScanner.Text())
			}
			if logScanner.Err() != nil {
				fmt.Printf("logScanner error: %v\n", err)
			}
		}()
	}
	if err := logWait(); err != nil {
		if ctxLogs.Err() == context.DeadlineExceeded {
			fmt.Printf("%v timeout expired\n", timeout)
		} else {
			fmt.Printf("wait error: %v\n", err)
			return "", err
		}
	}

	status, err := applecontainer.Containers.Inspect(ctx, id)
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "inspecting container %s: %s\n", id, err)
		}
		return "", err
	}
	if *verbose {
		fmt.Printf("Container status:\n%+v\n", status)
	}
	return id, nil

}

func initContainerPool(ctx context.Context, cwd string) (*pool.ContainerPool, error) {
	log.Printf("initContainerPool %s", cwd)
	ret, err := pool.NewConnectionPool(ctx, 4, func(ctx context.Context) (*pool.PooledContainer, error) {
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
		if err != nil {
			fmt.Printf("container stop error: %v\n%s\n", err, out)
		}
	})
	return ret, err
}

func runTests(ctx context.Context, args ...string) error {
	cwd, err := os.Getwd()
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "getting current working dir: %s\n", err)
		}
		return err
	}
	fileSystem := os.DirFS(cwd + "/testbin/linux")
	containerPool, err := initContainerPool(ctx, cwd)
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "initializing container pool: %s\n", err)
		}
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
		fmt.Printf("Executing %s in container %s...\n", path, id)
		wait, err := applecontainer.Containers.Exec(ctx,
			options.ExecContainer{}, id,
			"/gorunac/dev/testbin/linux/"+path,
			os.Environ(), os.Stdin, os.Stdout, os.Stderr, args...)

		if err != nil {
			if *verbose {
				fmt.Fprintf(os.Stderr, "getting running command in container: %s\n", err)
			}
			fmt.Fprintf(os.Stderr, "error running %s: %v\n", path, err)
		}
		err = wait()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error waiting for %s to complete: %v\n", path, err)
		}
		containerPool.Release(pooledCtr)
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "fs.WalkDir: %v", err)
	}
	ctx, done := context.WithTimeout(ctx, time.Second*20)
	defer done()
	return containerPool.Shutdown(ctx)
}

func compile(args ...string) error {
	cmd := exec.Command("go", append([]string{"test", "-c", "-o", "./testbin/linux/"}, args...)...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GOOS=linux")
	cmd.Env = append(cmd.Env, "GOARCH=arm64")
	if *verbose {
		fmt.Fprintf(os.Stderr, "compile cmd: %+v\n", cmd)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "go test -c error: %s\n", string(out))
		}
		return err
	}

	return nil
}
