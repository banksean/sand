//go:build linux

package cli

import (
	"fmt"
	"log/slog"
	"os"
)

type VscCmd struct{}

func (c *VscCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.MuxClient

	slog.InfoContext(ctx, "VscCmd", "run", *c)
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("could not get hostname: %w", err)
	}
	if err := mc.VSC(ctx, hostname); err != nil {
		slog.ErrorContext(ctx, "VscCmd.Run cmd", "error", err)
	}

	return nil
}
