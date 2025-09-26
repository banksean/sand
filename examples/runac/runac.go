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

// runac works like "go run", but instead of building and executing a binary on
// your host OS, it cross-compiles a linux binary and the runs the binary in an
// apple container.

var (
	verbose   = flag.Bool("verbose", false, "verbose output")
	imageName = flag.String("image", "ghcr.io/linuxcontainers/alpine:latest", "container image")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	// TODO:
	//
	// - parse command line stuff so we can pass it to "go build" etc
	// - cross-compile the binary to linux
	bin, err := compile(flag.Args()...)
	if err != nil {
		fmt.Printf("compile error: %v\n", err)
		os.Exit(1)
	}
	if *verbose {
		fmt.Printf("output binary name: %s\n", bin)
	}

	// - create or re-use an existing container
	status, err := applecontainer.System.Status(ctx, options.SystemStatus{})
	if err != nil {
		fmt.Printf("container system status error: %v\n", err)
		fmt.Printf("You may need to run `container system start` and re-try this command\n")
		os.Exit(1)
	}
	if *verbose {
		fmt.Printf("container system status: %s\n", status)
	}

	err = run(bin)
	if err != nil {
		fmt.Printf("run error: %v\n", err)
		os.Exit(1)
	}
}

func run(bin string) error {
	cwd, err := os.Getwd()
	if err != nil {
		if *verbose {
			fmt.Printf("getting current working dir: %s\n", err)
		}
		return err
	}
	cmd := exec.Command("container", []string{"run", "-it", "--rm", "--volume", cwd + ":/runac/dev", *imageName, "/runac/dev/bin/linux/" + bin}...)
	cmd.Env = os.Environ()
	if *verbose {
		fmt.Printf("container run command: %+v\n", cmd)
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
			fmt.Printf("go build error: %s\n", string(out))
		}
		return "", err
	}

	out, err = exec.Command("go", append([]string{"list", "-f", "{{.Target}}"}, args...)...).CombinedOutput()
	if err != nil {
		if *verbose {
			fmt.Printf("go list error: %s\n", string(out))
		}
		return "", err
	}
	binPath := strings.TrimSpace(string(out))
	_, bin := filepath.Split(binPath)

	return bin, nil
}
