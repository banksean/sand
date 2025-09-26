package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	applecontainer "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
)

// gorunac works like "go run [...]", but instead of building and executing a binary on
// your host OS, it cross-compiles a linux binary and then executes the binary in an
// apple container.

var (
	verbose   = flag.Bool("verbose", false, "verbose output")
	imageName = flag.String("image", "ghcr.io/linuxcontainers/alpine:latest", "container image")
)

func main() {
	flag.Parse()
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "This command cross-compiles a linux binary from go source on your Mac, and then excutes it inside a linux container")
		fmt.Fprintf(os.Stderr, "Usage: %s <...stuff that you would normally put after `go run`>\n", os.Args[0])
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

	compileArgs := []string{}
	runArgs := []string{}
	inRunArgs := false
	for _, arg := range flag.Args() {
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
	bin, err := compile(compileArgs...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compile error: %v\n", err)
		os.Exit(1)
	}
	if *verbose {
		fmt.Fprintf(os.Stderr, "output binary name: %s\n", bin)
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

	err = run(bin, runArgs...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run error: %v\n", err)
		os.Exit(1)
	}
}

func run(bin string, args ...string) error {
	cwd, err := os.Getwd()
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "getting current working dir: %s\n", err)
		}
		return err
	}
	cmd := exec.Command("container", append([]string{"run", "-it", "--rm", "--volume",
		cwd + ":/gorunac/dev", *imageName, "/gorunac/dev/bin/linux/" + bin}, args...)...)
	cmd.Env = os.Environ()
	if *verbose {
		fmt.Fprintf(os.Stderr, "container run command: %+v\n", cmd)
	}

	// Connect subprocess stdio to parent process stdio
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func compile(args ...string) (string, error) {
	cmd := exec.Command("go", append([]string{"build", "-o", "./bin/linux/"}, args...)...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GOOS=linux")
	cmd.Env = append(cmd.Env, "GOARCH=arm64")

	out, err := cmd.CombinedOutput()
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "go build error: %s\n", string(out))
		}
		return "", err
	}

	out, err = exec.Command("go", append([]string{"list", "-f", "{{.Target}}"}, args...)...).CombinedOutput()
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "go list error: %s\n", string(out))
		}
		return "", err
	}
	binPath := strings.TrimSpace(string(out))
	_, bin := filepath.Split(binPath)

	return bin, nil
}
