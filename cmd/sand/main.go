package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
	"github.com/banksean/apple-container/sandbox"
)

type Context struct {
	LogFile   string
	LogLevel  string
	CloneRoot string
}

type ShellCmd struct {
	ImageName     string `default:"sandbox" type:"image-name" help:"name of container image to use"`
	DockerFileDir string `help:"location of directory with docker file from which to build the image locally. Uses an embedded dockerfile if unset."`
	Shell         string `default:"/bin/zsh" help:"shell command to exec in the container"`
	Rm            bool   `help:"remove the sandbox after the shell terminates"`
	ID            string `arg optional help:"ID of the sandbox to create, or re-attach to"`
}

func (sc *ShellCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if sc.DockerFileDir == "" {
		slog.InfoContext(ctx, "main: unpacking embedded container build files")
		// TODO: name this dir using a content hash of defaultContainer.
		sc.DockerFileDir = "/tmp/sandbox-container-build"
		os.RemoveAll(sc.DockerFileDir)
		if err := os.MkdirAll(sc.DockerFileDir, 0755); err != nil {
			panic(err)
		}
		if err := os.CopyFS(sc.DockerFileDir, defaultContainer); err != nil {
			panic(err)
		}
		slog.InfoContext(ctx, "main: done unpacking embedded dockerfile")
		sc.DockerFileDir = filepath.Join(sc.DockerFileDir, "defaultcontainer")
	}
	sber := sandbox.NewSandBoxer(
		cctx.CloneRoot,
		sc.ImageName,
		sc.DockerFileDir,
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

	slog.InfoContext(ctx, "main: sbox.startContainer")
	if err := sbox.StartContainer(ctx); err != nil {
		slog.ErrorContext(ctx, "sbox.startContainer", "error", err)
		os.Exit(1)
	}

	slog.InfoContext(ctx, "main: sbox.shell starting")
	if err := sbox.ShellExec(ctx, sc.Shell, os.Stdin, os.Stdout, os.Stderr); err != nil {
		slog.ErrorContext(ctx, "sbox.shell", "error", err)
	}

	slog.InfoContext(ctx, "sbox.shell finished, cleaning up...")
	if err := sber.Cleanup(ctx, sbox); err != nil {
		slog.ErrorContext(ctx, "sber.Cleanup", "error", err)
	}

	slog.InfoContext(ctx, "Cleanup complete. Exiting.")
	return nil
}

type CLI struct {
	LogFile   string `default:"/tmp/sand/log" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel  string `default:"info" help:"the logging level (debug, info, warn, error)"`
	CloneRoot string `default:"/tmp/sand/boxen" help:"root dir to store sandbox clones of working directories"`

	Shell ShellCmd `cmd:"" help:"create or revive a sandbox and shell into its container"`
	Rm    struct {
		Paths []string `arg:"" name:"path" help:"Paths to remove." type:"path"`
	} `cmd:"" help:"remove sandbox"`

	Ls struct {
	} `cmd:"" help:"list sandboxes"`
}

func (c *CLI) initSlog() {
	var level slog.Level
	switch c.LogLevel {
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
	if c.LogFile == "" {
		f, err = os.CreateTemp("/tmp", c.LogFile)
		if err != nil {
			panic(err)
		}
	} else {
		logDir := filepath.Dir(c.LogFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			panic(err)
		}
		f, err = os.OpenFile(c.LogFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			panic(err)
		}
	}
	fmt.Printf("To view logs:\n\ttail -f %s\n\n", f.Name())
	logger := slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	slog.Info("slog initialized")
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli)
	cli.initSlog()
	err := ctx.Run(&Context{
		LogFile:   cli.LogFile,
		LogLevel:  cli.LogLevel,
		CloneRoot: cli.CloneRoot,
	})
	ctx.FatalIfErrorf(err)
}
