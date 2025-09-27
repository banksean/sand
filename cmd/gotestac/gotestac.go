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
	"os"
	"os/exec"
	"strings"

	applecontainer "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
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

func runTests(ctx context.Context, args ...string) error {
	cwd, err := os.Getwd()
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "getting current working dir: %s\n", err)
		}
		return err
	}
	fileSystem := os.DirFS(cwd + "/testbin/linux")

	err = fs.WalkDir(fileSystem, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if d.IsDir() {
			return nil
		}
		fmt.Printf("Running %s...\n", path)
		wait, err := applecontainer.Containers.Run(ctx, options.RunContainer{
			ManagementOptions: options.ManagementOptions{
				Remove: true,
				Volume: cwd + ":/gorunac/dev",
			},
		}, *imageName, "/gorunac/dev/testbin/linux/"+path, os.Environ(), os.Stdin, os.Stdout, os.Stderr, args...)

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
		return nil
	})

	return err
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
