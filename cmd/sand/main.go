package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/banksean/apple-container/sand"
)

type Context struct {
	AppBaseDir string
	LogFile    string
	LogLevel   string
	CloneRoot  string
	sber       *sand.SandBoxer
}

type CLI struct {
	LogFile   string `default:"/tmp/sand/log" placeholder:"<log-file-path>" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel  string `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	CloneRoot string `default:"" placeholder:"<clone-root-dir>" help:"root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand/boxen'"`

	New     NewCmd     `cmd:"" help:"create a new sandbox and shell into its container"`
	Shell   ShellCmd   `cmd:"" help:"shell into a sandbox container (and start the container, if necessary)"`
	Exec    ExecCmd    `cmd:"" help:"execute a single command in a sanbox"`
	Ls      LsCmd      `cmd:"" help:"list sandboxes"`
	Rm      RmCmd      `cmd:"" help:"remove sandbox container and its clone directory"`
	Stop    StopCmd    `cmd:"" help:"stop sandbox container"`
	Doc     DocCmd     `cmd:"" help:"print complete command help formatted as markdown"`
	Daemon  DaemonCmd  `cmd:"" help:"start or stop the sandmux daemon"`
	Version VersionCmd `cmd:"" help:"print version infomation about this command"`
}

func (c *CLI) initSlog(cctx *kong.Context) {
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
	if strings.HasPrefix(cctx.Command(), "daemon") {
		c.LogFile = c.LogFile + "daemon"
	}
	if c.LogFile == "" {
		f, err = os.CreateTemp("/tmp", "sand-log")
		if err != nil {
			panic(err)
		}
	} else {
		logDir := filepath.Dir(c.LogFile)
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			panic(err)
		}
		f, err = os.OpenFile(c.LogFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
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

func appHomeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("error getting home directory: %w", err)
	}

	// Construct the path to the application support directory
	appSupportDir := filepath.Join(homeDir, "Library", "Application Support", "Sand")

	// Create the directory if it doesn't exist
	err = os.MkdirAll(appSupportDir, 0o755) // 0755 grants read/write/execute for owner, read/execute for group/others
	if err != nil {
		return "", fmt.Errorf("error creating application support directory: %w", err)
	}

	return appSupportDir, nil
}

func main() {
	var cli CLI

	ctx := kong.Parse(&cli,
		kong.Configuration(kong.JSON, ".sand.json", "~/.sand.json"),
		kong.Description(description))
	cli.initSlog(ctx)

	if err := verifyPrerequisites(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Prerequisites check failed: %v\n", err.Error())
		os.Exit(1)
	}

	appBaseDir, err := appHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to get application home directory: %v\n", err.Error())
		os.Exit(1)
	}
	slog.Info("main", "appBaseDir", appBaseDir)

	// Don't try to ensure the daemon is running if we're trying to start or stop it.
	if !strings.HasPrefix(ctx.Command(), "daemon") && ctx.Command() != "doc" {
		if err := sand.EnsureDaemon(appBaseDir); err != nil {
			fmt.Fprintf(os.Stderr, "daemon not running, and failed to start it. error: %v\n", err)
			os.Exit(1)
		}
	}

	if cli.CloneRoot == "" {
		cli.CloneRoot = filepath.Join(appBaseDir, "boxen")
	}
	sber := sand.NewSandBoxer(cli.CloneRoot, os.Stderr)
	err = ctx.Run(&Context{
		AppBaseDir: appBaseDir,
		LogFile:    cli.LogFile,
		LogLevel:   cli.LogLevel,
		CloneRoot:  cli.CloneRoot,
		sber:       sber,
	})
	ctx.FatalIfErrorf(err)
}
