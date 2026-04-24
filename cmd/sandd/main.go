//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/banksean/sand/internal/applecontainer"
	"github.com/banksean/sand/internal/cli"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/sandboxlog"
	"github.com/banksean/sand/internal/version"
	"gopkg.in/natefinch/lumberjack.v2"
)

// `sandd start` runs a long-lived process that manages sandboxes' lifecycles.
//
// When you run the `sand` CLI, it is actually making IPC calls to this daemon in order
// to do all of the actual work of managing sandboxes.
//
// At startup, it will:
// - acquire a lock file at $AppBaseDir/sandd.lock
// - open a unix domain socket at $AppBaseDir/sandd.sock to accept IPC from the sand cli on the host OS
// - start an http server listening at :4242 to accept IPC from the sand cli running inside containers

type App struct {
	AppBaseDir string
	// HTTPPort   string
	LogFile   string
	LogLevel  string
	CloneRoot string
	Context   context.Context
}

type DaemonCmd struct {
	LogFile    string          `default:"/tmp/sand/daemon/log" placeholder:"<log-file-path>" help:"location of log file"`
	LogLevel   string          `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	AppBaseDir string          `default:"" placeholder:"<app-base-dir>" help:"root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'"`
	Version    cli.VersionFlag `name:"version" help:"Print version and exit."`
	Action     string          `arg:"" optional:"" default:"status" enum:"start,stop,status,build-info" help:"Action to perform: start, stop, or status (default). Shows daemon status if omitted."`
}

// Run handles all daemon command variants
func (c *DaemonCmd) Run(cctx *App) error {
	ctx := cctx.Context

	localDomain, err := applecontainer.System.PropertyGet(ctx, "dns.domain")
	if err != nil {
		return fmt.Errorf("unable to get dns.domain from container system: %w", err)
	}
	slog.InfoContext(ctx, "DaemonCmd.Run", "localDomain", localDomain)
	server := daemon.NewDaemon(cctx.AppBaseDir, localDomain)
	server.LogFile = cctx.LogFile

	switch c.Action {
	case "start":
		return c.startDaemon(ctx, server)
	case "stop":
		return c.stopDaemon(ctx, server)
	case "build-info":
		return c.buildInfo(ctx, server)
	case "status":
		fallthrough
	default:
		// Check status
		return c.checkStatus(ctx, server)
	}
}

func (c *DaemonCmd) buildInfo(ctx context.Context, server *daemon.Daemon) error {
	runningErr := c.runningVersion(ctx, server)
	fmt.Println()
	cliErr := c.cliVersion(ctx)
	return errors.Join(runningErr, cliErr)
}

func (c *DaemonCmd) runningVersion(ctx context.Context, server *daemon.Daemon) error {
	mc, err := daemon.NewUnixSocketClient(ctx, c.AppBaseDir)
	if err != nil {
		fmt.Println("Daemon is not running")
		return nil
	}

	versionInfo, err := mc.Version(ctx)
	if err != nil {
		fmt.Printf("Could not get version info from daemon: %v\n", err)
		return nil
	}

	fmt.Printf("==== Currently running daemon ==== \n")
	fmt.Printf("Git Repository: %s\n", versionInfo.GitRepo)
	fmt.Printf("Git Branch: %s\n", versionInfo.GitBranch)
	fmt.Printf("Git Commit: %s\n", versionInfo.GitCommit)
	fmt.Printf("Build Time: %s\n", versionInfo.BuildTime)
	buildInfo := versionInfo.BuildInfo
	if buildInfo == nil {
		return nil
	}
	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" && versionInfo.GitCommit == "" {
			fmt.Printf("Git Commit: %s\n", setting.Value)
		}
		if setting.Key == "vcs.time" && versionInfo.BuildTime == "" {
			fmt.Printf("Commit Time: %s\n", setting.Value)
		}
		if setting.Key == "vcs.modified" {
			fmt.Printf("Modified: %s\n", setting.Value)
		}
	}
	return nil
}

func (c *DaemonCmd) cliVersion(ctx context.Context) error {
	fmt.Printf("==== Sandd cli binary ==== \n")

	versionInfo := version.Get()
	fmt.Printf("Git Repository: %s\n", versionInfo.GitRepo)
	fmt.Printf("Git Branch: %s\n", versionInfo.GitBranch)
	fmt.Printf("Git Commit: %s\n", versionInfo.GitCommit)
	fmt.Printf("Build Time: %s\n", versionInfo.BuildTime)
	buildInfo := versionInfo.BuildInfo
	if buildInfo == nil {
		return nil
	}
	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" && versionInfo.GitCommit == "" {
			fmt.Printf("Git Commit: %s\n", setting.Value)
		}
		if setting.Key == "vcs.time" && versionInfo.BuildTime == "" {
			fmt.Printf("Commit Time: %s\n", setting.Value)
		}
		if setting.Key == "vcs.modified" {
			fmt.Printf("Modified: %s\n", setting.Value)
		}
	}
	return nil
}

func (c *DaemonCmd) checkStatus(ctx context.Context, server *daemon.Daemon) error {
	mc, err := daemon.NewUnixSocketClient(ctx, c.AppBaseDir)
	if err != nil {
		fmt.Println("Daemon is not running")
		return nil
	}

	if err := mc.Ping(ctx); err != nil {
		fmt.Println("Daemon is not running")
		return nil
	}

	fmt.Println("Daemon is running")
	return nil
}

func (c *DaemonCmd) startDaemon(ctx context.Context, server *daemon.Daemon) error {
	// Check if daemon is already running
	mc, err := daemon.NewUnixSocketClient(ctx, c.AppBaseDir)
	if err == nil {
		if err := mc.Ping(ctx); err == nil {
			fmt.Println("Daemon is already running")
			return nil
		}
	}

	// Start the daemon
	return server.ServeUnixSocket(ctx)
}

func (c *DaemonCmd) stopDaemon(ctx context.Context, server *daemon.Daemon) error {
	mc, err := daemon.NewUnixSocketClient(ctx, c.AppBaseDir)
	if err != nil {
		fmt.Println("Daemon is not running")
		return nil
	}

	if err := mc.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	fmt.Println("Daemon stopped")
	return nil
}

func (c *DaemonCmd) initSlog() {
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

	if c.LogFile == "" {
		c.LogFile = "/tmp/sand/daemon/log"
	}
	logDir := filepath.Dir(c.LogFile)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		panic(err)
	}

	w := &lumberjack.Logger{
		Filename:   c.LogFile,
		MaxSize:    100, // MB
		MaxBackups: 5,
		MaxAge:     30, // days
		Compress:   true,
	}

	handlerOpts := &slog.HandlerOptions{Level: level}
	baseHandler := slog.NewJSONHandler(w, handlerOpts)
	handler, err := daemon.NewSandboxFanoutHandler(baseHandler, daemon.SandboxLogsDir(c.LogFile), handlerOpts)
	if err != nil {
		panic(err)
	}
	handler = sandboxlog.NewContextHandler(handler)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	slog.Info("daemon slog initialized")
}

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

const description = `Manage lightweight linux container sandboxes on MacOS.`

func main() {
	var cli DaemonCmd

	kongCtx := kong.Parse(&cli,
		kong.Configuration(kongyaml.Loader, "~/.sand.yaml", ".sand.yaml"),
		kong.Description(description))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Catch control-C so if you break out of "sand new" because it's taking too long
	// to download a container image (which runs in exec.CommandContext subprocess),
	// we also kill any subprocesses that were started with ctx.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cli.initSlog()
	versionInfo := version.Get()

	cwd, err := os.Getwd()
	slog.InfoContext(ctx, "sandd main", "version", versionInfo, "cwd", cwd, "error", err)

	appBaseDir, err := appHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to get application home directory: %v\n", err.Error())
		os.Exit(1)
	}
	slog.Info("main", "appBaseDir", appBaseDir)

	if cli.AppBaseDir == "" {
		cli.AppBaseDir = appBaseDir
	}

	err = kongCtx.Run(&App{
		Context:    ctx,
		AppBaseDir: appBaseDir,
		LogFile:    cli.LogFile,
		LogLevel:   cli.LogLevel,
		CloneRoot:  cli.AppBaseDir,
	})

	kongCtx.FatalIfErrorf(err)
}
