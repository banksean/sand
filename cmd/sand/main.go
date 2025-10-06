package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
	"github.com/banksean/apple-container/sand"
)

type Context struct {
	LogFile   string
	LogLevel  string
	CloneRoot string
	sber      *sand.SandBoxer
}

type CLI struct {
	LogFile   string `default:"/tmp/sand/log" placeholder:"<log-file-path>" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel  string `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	CloneRoot string `default:"/tmp/sand/boxen" placeholder:"<clone-root-dir>" help:"root dir to store sandbox clones of working directories"`

	New     NewCmd     `cmd:"" help:"create a new sandbox and shell into its container"`
	Shell   ShellCmd   `cmd:"" help:"shell into a sandbox container (and start the container, if necessary)"`
	Exec    ExecCmd    `cmd:"" help:"execute a single command in a sanbox"`
	Ls      LsCmd      `cmd:"" help:"list sandboxes"`
	Rm      RmCmd      `cmd:"" help:"remove sandbox container and its clone directory"`
	Stop    StopCmd    `cmd:"" help:"stop sandbox container"`
	Doc     DocCmd     `cmd:"" help:"print complete command help formatted as markdown"`
	Version VersionCmd `cmd:"" help:"print version infomation about this command"`
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

	logger := slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	slog.Info("slog initialized")
}

const description = `Manage lightweight linux container sandboxes on MacOS.

Requires apple container CLI: https://github.com/apple/container/releases/tag/` + appleContainerVersion

func main() {
	var cli CLI

	ctx := kong.Parse(&cli,
		kong.Description(description))
	cli.initSlog()

	if err := verifyPrerequisites(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Prerequisites check failed: %v\n", err.Error())
		fmt.Fprintf(os.Stderr, "You may need to install Apple's `container` command from the releases published at https://github.com/apple/container/releases/tag/"+appleContainerVersion)
		os.Exit(1)
	}

	sber := sand.NewSandBoxer(cli.CloneRoot, os.Stderr)
	err := ctx.Run(&Context{
		LogFile:   cli.LogFile,
		LogLevel:  cli.LogLevel,
		CloneRoot: cli.CloneRoot,
		sber:      sber,
	})
	ctx.FatalIfErrorf(err)
}
