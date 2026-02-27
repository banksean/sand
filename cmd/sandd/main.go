package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/banksean/sand"
)

type Context struct {
	AppBaseDir string
	LogFile    string
	LogLevel   string
	CloneRoot  string
	Context    context.Context
	sber       *sand.Boxer
}
type DaemonCmd struct {
	Action string `arg:"" optional:"" default:"status" enum:"start,stop,restart,status,version" help:"Action to perform: start, stop, restart, or status (default). Shows daemon status if omitted."`
}

// Run handles all daemon command variants
func (c *DaemonCmd) Run(cctx *Context) error {
	ctx := cctx.Context
	mux := sand.NewMuxServer(cctx.AppBaseDir, cctx.sber)

	switch c.Action {
	case "start":
		return c.startDaemon(ctx, mux)
	case "stop":
		return c.stopDaemon(ctx, mux)
	case "restart":
		return c.restartDaemon(ctx, mux, cctx)
	case "version":
		return c.version(ctx, mux)
	case "status":
		fallthrough
	default:
		// Check status
		return c.checkStatus(ctx, mux)
	}
}

func (c *DaemonCmd) version(ctx context.Context, mux *sand.Mux) error {
	mc, err := mux.NewClient(ctx)
	if err != nil {
		fmt.Println("Daemon is not running")
		return nil
	}

	versionInfo, err := mc.Version(ctx)
	if err != nil {
		fmt.Printf("Could not get version info from daemon: %v\n", err)
		return nil
	}

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

func (c *DaemonCmd) checkStatus(ctx context.Context, mux *sand.Mux) error {
	mc, err := mux.NewClient(ctx)
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

func (c *DaemonCmd) startDaemon(ctx context.Context, mux *sand.Mux) error {
	// Check if daemon is already running
	mc, err := mux.NewClient(ctx)
	if err == nil {
		if err := mc.Ping(ctx); err == nil {
			fmt.Println("Daemon is already running")
			return nil
		}
	}

	// Start the daemon
	return mux.ServeUnixSocket(ctx)
}

func (c *DaemonCmd) stopDaemon(ctx context.Context, mux *sand.Mux) error {
	mc, err := mux.NewClient(ctx)
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

func (c *DaemonCmd) restartDaemon(ctx context.Context, mux *sand.Mux, cctx *Context) error {
	// First, attempt to stop the daemon if it's running
	mc, err := mux.NewClient(ctx)
	if err == nil {
		// Daemon is running, try to stop it
		if err := mc.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to stop daemon: %w", err)
		}
		fmt.Println("Daemon stopped")
	}

	// Build the command to start the daemon
	cmd := exec.CommandContext(ctx, os.Args[0], "daemon", "start", "--log-file", cctx.LogFile, "--clone-root", cctx.CloneRoot)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	// Detach from parent process
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait for daemon to be ready
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		conn, err := net.DialTimeout("unix", mux.SocketPath, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			fmt.Println("Daemon restarted successfully")
			return nil
		}
	}

	return fmt.Errorf("daemon failed to start")
}

type CLI struct {
	LogFile    string `default:"/tmp/sand/log.daemon" placeholder:"<log-file-path>" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel   string `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	AppBaseDir string `default:"" placeholder:"<app-base-dir>" help:"root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'"`

	Daemon DaemonCmd `cmd:"" help:"start or stop the sandmux daemon"`
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
	var cli CLI

	kongCtx := kong.Parse(&cli,
		kong.Configuration(kong.JSON, ".sand.json", "~/.sand.json"),
		kong.Description(description))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Catch control-C so if you break out of "sand new" because it's taking too long
	// to download a container image (which runs in exec.CommandContext subprocess),
	// we also kill any subprocesses that were started with ctx.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cli.initSlog(kongCtx)

	appBaseDir, err := appHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to get application home directory: %v\n", err.Error())
		os.Exit(1)
	}
	slog.Info("main", "appBaseDir", appBaseDir)

	// Don't try to ensure the daemon is running if we're trying to start or stop it.
	if !strings.HasPrefix(kongCtx.Command(), "daemon") && kongCtx.Command() != "doc" {
		if err := sand.EnsureDaemon(ctx, appBaseDir, cli.LogFile); err != nil {
			fmt.Fprintf(os.Stderr, "daemon not running, and failed to start it. error: %v\n", err)
			os.Exit(1)
		}
	}

	if cli.AppBaseDir == "" {
		cli.AppBaseDir = appBaseDir
	}

	var sber *sand.Boxer
	if kongCtx.Command() != "doc" {
		sber, err = sand.NewBoxer(cli.AppBaseDir, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create Boxer: %v\n", err)
			os.Exit(1)
		}
		if err := sber.Sync(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "failed to sync Boxer db with current environment state: %v\n", err)
			os.Exit(1)
		}
		defer sber.Close()
	}

	err = kongCtx.Run(&Context{
		Context:    ctx,
		AppBaseDir: appBaseDir,
		LogFile:    cli.LogFile,
		LogLevel:   cli.LogLevel,
		CloneRoot:  cli.AppBaseDir,
		sber:       sber,
	})
	kongCtx.FatalIfErrorf(err)
}
