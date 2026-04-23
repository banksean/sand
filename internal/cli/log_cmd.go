package cli

import (
	"log/slog"
	"os"
)

type SandboxLogCmd struct {
	SandboxNameFlag
	// TODO: add -f for following a la slogtail.
}

func (c *SandboxLogCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	if err := cctx.Daemon.LogSandbox(ctx, c.SandboxName, os.Stdout); err != nil {
		slog.ErrorContext(ctx, "LogSandbox", "error", err, "sandbox_id", c.SandboxName)
		return err
	}
	return nil
}
