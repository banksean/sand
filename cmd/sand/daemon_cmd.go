package main

import (
	"context"
	"fmt"

	"github.com/banksean/apple-container/sand"
)

type DaemonCmd struct {
	Action string `arg:"" optional:"" default:"status" enum:"start,stop,status" help:"Action to perform: start, stop, or status (default). Shows daemon status if omitted."`
}

// Run handles all daemon command variants
func (c *DaemonCmd) Run(cctx *Context) error {
	ctx := context.Background()
	mux := sand.NewMux(cctx.AppBaseDir, cctx.sber)

	switch c.Action {
	case "start":
		return c.startDaemon(ctx, mux)
	case "stop":
		return c.stopDaemon(ctx, mux)
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
