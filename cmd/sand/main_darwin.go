//go:build darwin

package main

import (
	"context"
	"encoding/json"
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
	kongyaml "github.com/alecthomas/kong-yaml"
	"github.com/banksean/sand/internal/cli"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/banksean/sand/internal/version"
	kongcompletion "github.com/jotaen/kong-completion"
)

type Outie struct {
	LogFile    string                    `default:"/tmp/sand/outie/log" placeholder:"<log-file-path>" help:"location of log file (leave empty for a random tmp/ path)"`
	LogLevel   string                    `default:"info" placeholder:"<debug|info|warn|error>" help:"the logging level (debug, info, warn, error)"`
	AppBaseDir string                    `default:"" placeholder:"<app-base-dir>" help:"root dir to store sandbox clones of working directories. Leave unset to use '~/Library/Application Support/Sand'"`
	Timeout    time.Duration             `default:"0s" help:"if set to anything other than 0s, overrides the default timeout for an operation"`
	Completion kongcompletion.Completion `cmd:"" help:"Outputs shell code for initialising tab completions"`
	Version    cli.VersionFlag           `name:"version" help:"Print version and exit."`
	DryRun     bool                      `default:"false" help:"just print out the operations instead of executing them"`
	Caches     cli.CacheFlags            `embed:"" prefix:"caches-"`

	New                cli.NewCmd                `cmd:"" help:"create a new sandbox and shell into its container"`
	Oneshot            cli.OneshotCmd            `cmd:"" help:"run an AI agent non-interactively with a prompt"`
	Shell              cli.ShellCmd              `cmd:"" help:"shell into a sandbox container (and start the container, if necessary)"`
	Exec               cli.ExecCmd               `cmd:"" help:"execute a single command in a sanbox"`
	Ls                 cli.LsCmd                 `cmd:"" help:"list sandboxes"`
	Log                cli.SandboxLogCmd         `cmd:"" help:"print sandbox lifecycle and daemon events"`
	Rm                 cli.RmCmd                 `cmd:"" help:"remove sandbox container and its clone directory"`
	Stop               cli.StopCmd               `cmd:"" help:"stop sandbox container"`
	Start              cli.StartCmd              `cmd:"" help:"start sandbox container"`
	Git                cli.GitCmd                `cmd:"" help:"git operations with sandboxes"`
	Doc                DocCmd                    `cmd:"" help:"print complete command help formatted as markdown"`
	BuildInfo          cli.BuildInfoCmd          `cmd:"" help:"print version infomation about this command"`
	Vsc                cli.VscCmd                `cmd:"" help:"launch a vscode remote window connected to the sandbox's container"`
	InstallEBPFSupport cli.InstallEBPFSupportCmd `cmd:"" help:"install the BPFFS-enabled kernel build"`
	ExportFS           cli.ExportCmd             `cmd:"" help:"export a container's filesystem"`
	Stats              cli.StatsCmd              `cmd:"" help:"list container stats for sandboxes"`
	Config             cli.ConfigCmd             `cmd:"" help:"list, get, or set default values for flags"`
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

Requires apple container CLI: https://github.com/apple/container/releases/tag/` + runtimedeps.AppleContainerVersion

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

// ensureDaemon attempts to verify that the sandd daemon is running, and if not,
// starts a new instance of it.
func ensureDaemon(ctx context.Context, appBaseDir string) error {
	socketPath := filepath.Join(appBaseDir, daemon.DefaultSocketFile)
	slog.Info("EnsureDaemon", "socketPath", socketPath)

	// Try to connect to existing daemon
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err == nil {
		conn.Close()
		// Daemon is running, check if version matches
		if err := checkDaemonVersion(ctx, appBaseDir); err != nil {
			slog.Info("EnsureDaemon", "versionMismatch", err.Error())
			// Version mismatch, shut down old daemon
			if err := shutdownDaemon(appBaseDir); err != nil {
				slog.Warn("EnsureDaemon", "shutdownError", err.Error())
				// Continue to try starting new daemon anyway
			}
			// Fall through to start new daemon
		} else {
			return nil // Daemon running with correct version
		}
	}

	// Start daemon in background
	cmd := exec.Command("sandd", "start", "--app-base-dir", appBaseDir)
	slog.Info("EnsureDaemon", "cmd", strings.Join(cmd.Args, " "))
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.Dir = appBaseDir

	// Detach from parent process
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Wait for daemon to be ready
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
	}

	return fmt.Errorf("daemon failed to start")
}

func checkDaemonVersion(ctx context.Context, appBaseDir string) error {
	client, err := daemon.NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	daemonVersion, err := client.Version(ctx)
	if err != nil {
		return fmt.Errorf("failed to get daemon version: %w", err)
	}

	cliVersion := version.Get()
	if !cliVersion.Equal(daemonVersion) {
		return fmt.Errorf("version mismatch: CLI=%s, Daemon=%s", cliVersion.GitCommit, daemonVersion.GitCommit)
	}

	return nil
}

func shutdownDaemon(appBaseDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := daemon.NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	return client.Shutdown(ctx)
}

func main() {
	var app Outie
	slog.SetLogLoggerLevel(slog.LevelError)

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

	predictorMC, err := daemon.NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sandd client, error: %v\n", err)
		os.Exit(1)
	}
	namePredictor := cli.NewSandboxNamePredictor(predictorMC)

	kongApp := kong.Must(&app)
	kongcompletion.Register(kongApp, kongcompletion.WithPredictor("sandbox-name", namePredictor))
	kongConfigPaths := []string{"~/.sand.yaml"}
	if p := cli.FindProjectConfig(); p != "" {
		kongConfigPaths = append(kongConfigPaths, p)
	}
	kongCtx := kong.Parse(&app,
		kong.UsageOnError(),
		kong.Configuration(kongyaml.Loader, kongConfigPaths...),
		kong.Description(description),
	)

	if app.DryRun {
		appJSON, err := json.MarshalIndent(app, "", "  ")
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s\n", string(appJSON))
		return
	}

	app.initSlog()

	if err := runtimedeps.Verify(ctx,
		appBaseDir,
		runtimedeps.MacOS,
		runtimedeps.MacOSVersion,
		runtimedeps.ContainerCommand,
		runtimedeps.ContainerSystemDNSDomain); err != nil {
		fmt.Fprintf(os.Stderr, "Prerequisite check(s) failed: %s\r\n", err)
		os.Exit(1)
	}

	slog.Info("main", "appBaseDir", appBaseDir)

	if err := ensureDaemon(ctx, appBaseDir); err != nil {
		fmt.Fprintf(os.Stderr, "daemon not running, and failed to start it. error: %v\n", err)
		os.Exit(1)
	}

	if app.AppBaseDir == "" {
		app.AppBaseDir = appBaseDir
	}

	mc, err := daemon.NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sandd client, error: %v\n", err)
		os.Exit(1)
	}
	if app.Timeout != 0 {
		ctx, cancel = context.WithTimeout(ctx, app.Timeout)
		defer cancel()
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
