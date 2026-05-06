package cli

import (
	"fmt"
	"log/slog"
	"os/user"

	"github.com/banksean/sand/internal/daemon"
)

type ShellCmd struct {
	ShellFlags
	ProjectEnvFlag
	SSHAgent bool `help:"enable ssh-agent forwarding for the container"`
	SandboxNameFlag
}

func (c *ShellCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "name", c.SandboxName)
		return fmt.Errorf("error while trying to find sandbox named %s: %w", c.SandboxName, err)
	}
	if sbox == nil {
		return fmt.Errorf("could not find sandbox named %s", c.SandboxName)
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
		if err := mc.StartSandbox(ctx, daemon.StartSandboxOpts{
			Name:     sbox.Name,
			SSHAgent: c.SSHAgent,
		}); err != nil {
			return fmt.Errorf("could not start container for %s: %w", sbox.Name, err)
		}
		sbox, err = mc.GetSandbox(ctx, c.SandboxName)
		if err != nil {
			return fmt.Errorf("error while refreshing sandbox %s after start: %w", c.SandboxName, err)
		}
	}

	if c.SSHAgent && sbox.Container != nil && !sbox.Container.Configuration.SSH {
		fmt.Printf("warning: %s is already running without ssh-agent forwarding; stop it and run `sand shell %s --ssh-agent` again to recreate it with ssh-agent enabled\n", sbox.Name, sbox.Name)
	}

	shell := c.Shell
	var args []string
	if c.Tmux && c.Atch {
		return fmt.Errorf("--tmux and --atch cannot be used together")
	}
	if c.Tmux {
		shell = "/usr/bin/tmux"
		args = []string{"new-session", "-A"}
	} else if c.Atch {
		shell = "/usr/local/bin/atch"
		args = []string{sbox.Name, c.Shell}
	}

	return runShell(ctx, sbox, shell, args, false, plainCommandEnvFile(sbox, c.ProjectEnv), nil)
}
