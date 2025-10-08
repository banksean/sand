// gorunac works like "go run [...]", but instead of building and executing a binary on
// your MacOS host, it cross-compiles a linux binary and then executes the binary in an
// apple linux container.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	applecontainer "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
)

var (
	imageName   = flag.String("image", "ghcr.io/linuxcontainers/alpine:latest", "container image")
	logLevelStr = flag.String("loglevel", "error", "Set the logging level (debug, info, warn, error)")
)

func initSlog() {
	var level slog.Level
	switch *logLevelStr {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo // Default to info if invalid
	}

	// Create a new logger with a JSON handler writing to standard error
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
}

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
	initSlog()
	ctx := context.Background()
	slog.InfoContext(ctx, "main", "args", flag.Args())

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
	bin, err := compile(ctx, compileArgs...)
	if err != nil {
		slog.ErrorContext(ctx, "main compile", "error", err)
		os.Exit(1)
	}
	slog.InfoContext(ctx, "main", "bin", bin)

	status, err := applecontainer.System.Status(ctx, nil)
	if err != nil {
		slog.ErrorContext(ctx, "main system status", "error", err)
		fmt.Fprintf(os.Stderr, "You may need to run `container system start` and re-try this command\n")
		os.Exit(1)
	}

	slog.InfoContext(ctx, "main container system", "status", status)

	err = run(ctx, bin, runArgs...)
	if err != nil {
		slog.ErrorContext(ctx, "main run", "error", err)

		os.Exit(1)
	}
}

func run(ctx context.Context, bin string, args ...string) error {
	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "run getting current working dir", "error", err)
		return err
	}
	wait, err := applecontainer.Containers.Run(ctx, &options.RunContainer{
		ProcessOptions: options.ProcessOptions{
			Interactive: true,
		},
		ManagementOptions: options.ManagementOptions{
			Remove: true,
			Volume: cwd + ":/gorunac/dev",
		},
	}, *imageName, "/gorunac/dev/bin/linux/"+bin, os.Environ(), os.Stdin, os.Stdout, os.Stderr, args...)
	if err != nil {
		slog.ErrorContext(ctx, "getting running command in container", "error", err)
		return err
	}

	return wait()
}

func compile(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", append([]string{"build", "-o", "./bin/linux/"}, args...)...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "GOOS=linux")
	cmd.Env = append(cmd.Env, "GOARCH=arm64")

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "compile go build", "error", err, "out", string(out))
		return "", err
	}

	out, err = exec.Command("go", append([]string{"list", "-f", "{{.Target}}"}, args...)...).CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "compile go list", "error", err, "out", string(out))
		return "", err
	}
	binPath := strings.TrimSpace(string(out))
	_, bin := filepath.Split(binPath)

	return bin, nil
}
