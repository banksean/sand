//go:build linux

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
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/banksean/sand/internal/cli"
	"github.com/banksean/sand/internal/daemon"
	kongcompletion "github.com/jotaen/kong-completion"
)

// Innie is the container-side cli to work with the sandd instance running on the host.
// It shares a lot of the same subcommands with the host-side sand command, but they
// work slightly differently when invoked from inside a container.
// TODO:
// - sort out how `sand new` should work when you run it from inside a sandbox container. There are multiple ways to do this. Some options:
//.  - A: should it create a clone from the innie's sandbox's original parent, or
//   - B: should it create a new clone using the innie's sandbox's current state as its parent
//.  - if we want it to do the latter, do we know if apple's container system can clone the entire sandbox container (including FS changes, running processes etc)?

type Innie struct {
	LogFile    string                    `default:"/tmp/sand/innie/log" placeholder:"<log-file-path>" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel   string                    `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	AppBaseDir string                    `default:"" placeholder:"<app-base-dir>" help:"root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'"`
	Completion kongcompletion.Completion `cmd:"" help:"Outputs shell code for initialising tab completions"`
	Version    cli.VersionFlag           `name:"version" help:"Print version and exit."`
	Caches     cli.CacheFlags            `embed:"" prefix:"caches-"`

	New       cli.NewCmd        `cmd:"" help:"create a new sandbox and shell into its container"`
	Ls        cli.LsCmd         `cmd:"" help:"list sandboxes"`
	Log       cli.SandboxLogCmd `cmd:"" help:"print sandbox lifecycle and daemon events"`
	Rm        cli.RmCmd         `cmd:"" help:"remove sandbox container and its clone directory"`
	Stop      cli.StopCmd       `cmd:"" help:"stop sandbox container"`
	Git       cli.GitCmd        `cmd:"" help:"git operations with sandboxes"`
	BuildInfo cli.BuildInfoCmd  `cmd:"" help:"print version infomation about this command"`
	Vsc       cli.VscCmd        `cmd:"" help:"launch a vscode window on your host OS desktop, connected to this sandbox's container via ssh"`
}

func (c *Innie) initSlog() {
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
		f, err = os.CreateTemp("/tmp/sand/innie", "log")
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
	slog.Info("innie slog initialized")
}

const description = `The "innie" side of the "sand" command, communicates with the host OS to execute sandbox related commands.`

func main() {
	var app Innie
	slog.SetLogLoggerLevel(slog.LevelError)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Catch control-C so if you break out of "sand new" because it's taking too long
	// to download a container image (which runs in exec.CommandContext subprocess),
	// we also kill any subprocesses that were started with ctx.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// connect to the sandd process running on the host via unix domain socket.
	// The sandd.grpc.sock file in this directory should have been created by sandd
	// and attached to this container via --volume flag.
	appBaseDir := "/run/host-services"

	kongApp := kong.Must(&app)

	predictorMC, err := daemon.NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sandd client, error: %v\n", err)
		os.Exit(1)
	}
	namePredictor := cli.NewSandboxNamePredictor(predictorMC)

	kongcompletion.Register(kongApp, kongcompletion.WithPredictor("sandbox-name", namePredictor))
	kongConfigPaths := []string{"~/.sand.yaml"}
	if p := cli.FindProjectConfig(); p != "" {
		kongConfigPaths = append(kongConfigPaths, p)
	}
	kongCtx := kong.Parse(&app,
		kong.UsageOnError(),
		kong.Configuration(kongyaml.Loader, kongConfigPaths...),
		kong.Description(description))

	app.initSlog()

	slog.Info("main", "appBaseDir", appBaseDir)

	if app.AppBaseDir == "" {
		app.AppBaseDir = appBaseDir
	}

	mc, err := daemon.NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sandd client, error: %v\n", err)
		os.Exit(1)
	}
	err = kongCtx.Run(&cli.CLIContext{
		Daemon:       mc,
		Context:      ctx,
		AppBaseDir:   appBaseDir,
		LogFile:      app.LogFile,
		LogLevel:     app.LogLevel,
		CloneRoot:    app.AppBaseDir,
		SharedCaches: app.Caches.SharedCacheConfig(),
	})
	kongCtx.FatalIfErrorf(err)
}
