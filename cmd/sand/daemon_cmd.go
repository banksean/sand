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
	Action string `arg:"" optional:"" default:"status" enum:"start,stop,restart,status" help:"Action to perform: start, stop, restart, or status (default). Shows daemon status if omitted."`
}

// Run handles all daemon command variants
func (c *DaemonCmd) Run(cctx *Context) error {
	ctx := context.Background()
	mux := sand.NewMuxServer(cctx.AppBaseDir, cctx.sber)

	switch c.Action {
	case "start":
		return c.startDaemon(ctx, mux)
	case "stop":
		return c.stopDaemon(ctx, mux)
	case "restart":
		return c.restartDaemon(ctx, mux, cctx)
	case "status":
		fallthrough
	default:
		// Check status
		return c.checkStatus(ctx, mux)
	}
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
	return mux.ServeUnix(ctx)
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
