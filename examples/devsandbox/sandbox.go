// command devsandbox manages containerized dev sandobx environments on MacOS
//
// On startup, devsandbox will:
// - if --attach=${id} is not set:
//   - choose a new unused ${id}
//   - create a new copy-on-write clone of the current working directory in ~/sandboxen/${id} on the MacOS host
//   - create a new container instance with name ${id} and ~/sandboxen/${id} mounted to /app in the container, using bind-mode
// - start container named ${id}
// - exec a shell in the container and connect this process's stdio to that shell in the container
//
// On exit, devsandbox will
// - stop the container named ${id}
// - if --rm is set, delete the container

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/banksean/apple-container/sandbox"
)

const (
	DefaultDockerFile = "./examples/devsandbox/Dockerfile"
)

var (
	attachTo         = flag.String("attach", "", "sandbox ID to re-connect to")
	sandboxCloneRoot = flag.String("sandboxen", "/tmp/sandboxen", "root dir to store sandbox data")
	imageName        = flag.String("image", sandbox.DefaultImageName, "name of container image to use")
	dockerFile       = flag.String("dockerfile", DefaultDockerFile, "location of docker file to build the image locally")
	shellCmd         = flag.String("shell", "/bin/zsh", "shell command to exec in the container")
	logLevelStr      = flag.String("loglevel", "error", "Set the logging level (debug, info, warn, error)")
	logFile          = flag.String("log", "", "location of log file (leave empty for a random tmp/ path)")
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
	var f *os.File
	var err error
	if *logFile == "" {
		f, err = os.CreateTemp("/tmp", *logFile)
		if err != nil {
			panic(err)
		}
	} else {
		f, err = os.OpenFile(*logFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			panic(err)
		}
	}
	fmt.Printf("tail -f %s\n", f.Name())
	logger := slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
}

func main() {
	flag.Parse()
	initSlog()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sber := sandbox.NewSandBoxer(
		*sandboxCloneRoot,
		*imageName,
		*dockerFile,
	)

	if err := sber.Init(ctx); err != nil {
		slog.ErrorContext(ctx, "sber.Init", "error", err)
		os.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		os.Exit(1)
	}

	var sbox *sandbox.SandBox
	if *attachTo == "" {
		sbox, err = sber.NewSandbox(ctx, cwd)
		if err != nil {
			slog.ErrorContext(ctx, "sber.NewSandbox", "error", err)
			os.Exit(1)
		}

		slog.InfoContext(ctx, "main: sbox.createContainer")
		if err := sbox.CreateContainer(ctx); err != nil {
			slog.ErrorContext(ctx, "sbox.createContainer", "error", err)
			os.Exit(1)
		}
	} else {
		sbox, err = sber.AttachSandbox(ctx, *attachTo)
		if err != nil {
			slog.ErrorContext(ctx, "sber.AttachSandbox", "error", err)
			os.Exit(1)
		}
	}

	slog.InfoContext(ctx, "main: sbox.startContainer")
	if err := sbox.StartContainer(ctx); err != nil {
		slog.ErrorContext(ctx, "sbox.startContainer", "error", err)
		os.Exit(1)
	}

	slog.InfoContext(ctx, "main: sbox.shell starting")
	if err := sbox.ShellExec(ctx, *shellCmd, os.Stdin, os.Stdout, os.Stderr); err != nil {
		slog.ErrorContext(ctx, "sbox.shell", "error", err)
	}

	slog.InfoContext(ctx, "sbox.shell finished, cleaning up...")
	if err := sber.Cleanup(ctx, sbox); err != nil {
		slog.ErrorContext(ctx, "sber.Cleanup", "error", err)
	}

	slog.InfoContext(ctx, "Cleanup complete. Exiting.")
}
