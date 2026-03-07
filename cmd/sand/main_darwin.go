//go:build darwin

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/banksean/sand/cli"
	"github.com/banksean/sand/mux"
	kongcompletion "github.com/jotaen/kong-completion"
)

type Outie struct {
	LogFile    string                    `default:"/tmp/sand/outie/log" placeholder:"<log-file-path>" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel   string                    `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	AppBaseDir string                    `default:"" placeholder:"<app-base-dir>" help:"root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'"`
	Completion kongcompletion.Completion `cmd:"" help:"Outputs shell code for initialising tab completions"`

	New     cli.NewCmd     `cmd:"" help:"create a new sandbox and shell into its container"`
	Shell   cli.ShellCmd   `cmd:"" help:"shell into a sandbox container (and start the container, if necessary)"`
	Exec    cli.ExecCmd    `cmd:"" help:"execute a single command in a sanbox"`
	Ls      cli.LsCmd      `cmd:"" help:"list sandboxes"`
	Rm      cli.RmCmd      `cmd:"" help:"remove sandbox container and its clone directory"`
	Stop    cli.StopCmd    `cmd:"" help:"stop sandbox container"`
	Git     cli.GitCmd     `cmd:"" help:"git operations with sandboxes"`
	Doc     DocCmd         `cmd:"" help:"print complete command help formatted as markdown"`
	Version cli.VersionCmd `cmd:"" help:"print version infomation about this command"`
	Vsc     cli.VscCmd     `cmd:"" help:"launch a vscode remote window connected to the sandbox's container"`
}

func (c *Outie) initSlog() {
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
		f, err = os.CreateTemp("/tmp/sand/outie", "log")
		if err != nil {
			panic(err)
		}
		c.LogFile = f.Name()
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
	slog.Info("outie slog initialized")
}

const description = `Manage lightweight linux container sandboxes on MacOS.

Requires apple container CLI: https://github.com/apple/container/releases/tag/` + cli.AppleContainerVersion

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
	var app Outie

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Catch control-C so if you break out of "sand new" because it's taking too long
	// to download a container image (which runs in exec.CommandContext subprocess),
	// we also kill any subprocesses that were started with ctx.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	appBaseDir, err := appHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to get application home directory: %v\n", err.Error())
		os.Exit(1)
	}

	predictorMC, err := mux.NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sandmux client, error: %v\n", err)
		os.Exit(1)
	}
	namePredictor := cli.NewSandboxNamePredictor(predictorMC)

	kongApp := kong.Must(&app)
	kongcompletion.Register(kongApp, kongcompletion.WithPredictor("sandbox-name", namePredictor))
	kongCtx := kong.Parse(&app,
		kong.Configuration(kong.JSON, ".sand.json", "~/.sand.json"),
		kong.Description(description))

	app.initSlog()

	if err := cli.VerifyPrerequisites(ctx, cli.MacOS, cli.MacOSVersion, cli.ContainerCommand); err != nil {
		fmt.Fprintf(os.Stderr, "Prerequisite check(s) failed: %s\r\n", err)
		os.Exit(1)
	}

	slog.Info("main", "appBaseDir", appBaseDir)

	if err := mux.EnsureDaemon(ctx, appBaseDir); err != nil {
		fmt.Fprintf(os.Stderr, "daemon not running, and failed to start it. error: %v\n", err)
		os.Exit(1)
	}

	if app.AppBaseDir == "" {
		app.AppBaseDir = appBaseDir
	}

	mc, err := mux.NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sandmux client, error: %v\n", err)
		os.Exit(1)
	}
	err = kongCtx.Run(&cli.CLIContext{
		MuxClient:  mc,
		Context:    ctx,
		AppBaseDir: appBaseDir,
		LogFile:    app.LogFile,
		LogLevel:   app.LogLevel,
		CloneRoot:  app.AppBaseDir,
	})
	kongCtx.FatalIfErrorf(err)
}
