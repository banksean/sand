package cli

import (
	"fmt"
	"log/slog"
	"os/user"
)

type ShellCmd struct {
	ShellFlags
	SandboxNameFlag
}

func (c *ShellCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "id", c.SandboxName)
		return fmt.Errorf("error while trying to find sandbox with ID %s: %w", c.SandboxName, err)
	}
	if sbox == nil {
		return fmt.Errorf("could not find sandbox with ID %s", c.SandboxName)
	}

	// Legacy sandboxes may not have a stored username; fall back to the current user.
	if sbox.Username == "" {
		userInfo, err := user.Current()
		if err != nil {
			return err
		}
		sbox.Username = userInfo.Username
		sbox.Uid = userInfo.Uid
	}

	slog.InfoContext(ctx, "main: sbox.shell starting")

	// sbox.Container is populated by GetSandbox; its Status is fresh enough to
	// decide whether to start the container without a redundant Inspect call.
	if sbox.Container == nil || sbox.Container.Status != "running" {
		if err := mc.StartSandbox(ctx, sbox.ID); err != nil {
			return fmt.Errorf("could not start container for %s: %w", sbox.ID, err)
		}
	}

	shell := c.Shell
	var args []string
	if c.Tmux {
		shell = "/usr/bin/tmux"
		args = []string{"new-session", "-A"}
	}

	return runShell(ctx, sbox, shell, args)
}
