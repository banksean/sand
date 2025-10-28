package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/banksean/sand"
)

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
