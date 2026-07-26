package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/user"

	"github.com/banksean/sand/internal/cli/agentlaunch"
	"github.com/banksean/sand/internal/daemon"
)

type ShellCmd struct {
	ShellFlags
	ProjectEnvFlag
	SSHAgent bool `help:"enable ssh-agent forwarding for the container"`
	SandboxNameFlag
	NoArchive bool `help:"use ephemeral state for the sandbox's declared agent in this shell"`
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
	if sbox.Container == nil || sbox.Container.Status.State != "running" {
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
	if c.NoArchive && (c.Tmux || c.Atch) {
		return fmt.Errorf("--no-archive cannot reconnect to persistent tmux or atch sessions")
	}
	if c.Tmux {
		shell = "/usr/bin/tmux"
		args = []string{"new-session", "-A"}
	} else if c.Atch {
		shell = "/usr/local/bin/atch"
		args = []string{sbox.Name, c.Shell}
	}

	projectEnv, err := plainCommandProjectEnv(sbox, c.ProjectEnv)
	if err != nil {
		return err
	}
	defer projectEnv.Cleanup()
	env := projectEnv.Env
	if c.NoArchive && sbox.AgentType != "" && sbox.AgentType != "default" {
		env = mergeEnv(env, agentlaunch.EphemeralSessionEnv(sbox.AgentType, sbox.ID))
	}
	launchID := ""
	if !c.NoArchive && sbox.SessionArchiveEnabled && sbox.AgentType != "default" {
		launchID, err = mc.BeginAgentSessionLaunch(ctx, sbox.Name)
		if err != nil {
			return fmt.Errorf("begin agent session archive: %w", err)
		}
	}
	runErr := runShell(ctx, sbox, shell, args, false, projectEnv.EnvFile, env)
	if launchID != "" {
		archiveCtx := context.WithoutCancel(ctx)
		if !c.Tmux && !c.Atch {
			if err := mc.EndAgentSessionLaunch(archiveCtx, launchID); err != nil {
				return err
			}
		}
		if err := mc.SyncAgentSessions(archiveCtx, sbox.Name); err != nil {
			return err
		}
	}
	return runErr
}
