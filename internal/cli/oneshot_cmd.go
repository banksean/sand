package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/goombaio/namegenerator"
)

// RunCmd creates a sandbox (or reuses an existing one) and runs an AI agent
// non-interactively with the given prompt, streaming output to stdout.
type RunCmd struct {
	SandboxCreationFlags
	Agent       string `short:"a" default:"claude" placeholder:"<claude|opencode>" help:"coding agent to use"`
	Username    string `help:"name of default user to create (defaults to $USER)"`
	Uid         string `help:"id of default user to create (defaults to $UID)"`
	SandboxName string `short:"n" placeholder:"<name>" help:"name of the sandbox to use (generated if omitted)"`
	Prompt      string `arg:"" help:"prompt to pass to the agent"`
}

func (c *RunCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	if err := runtimedeps.Verify(ctx, cctx.AppBaseDir, runtimedeps.GitDir); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if c.CloneFromDir == "" {
		c.CloneFromDir = cwd
	}

	userInfo, err := user.Current()
	if err != nil {
		return err
	}
	if c.Username == "" {
		c.Username = userInfo.Username
	}
	if c.Uid == "" {
		c.Uid = userInfo.Uid
	}

	if c.SandboxName == "" {
		seed := time.Now().UTC().UnixNano()
		c.SandboxName = namegenerator.NewNameGenerator(seed).Generate()
	}

	if c.EnvFile != "" && !filepath.IsAbs(c.EnvFile) {
		c.EnvFile = filepath.Join(c.CloneFromDir, c.EnvFile)
	}

	if c.ImageName == "" {
		if img, ok := defaultImageForAgent[c.Agent]; ok {
			c.ImageName = img
		} else {
			c.ImageName = DefaultImageName
		}
	}

	if err := mc.EnsureImage(ctx, c.ImageName, os.Stdout); err != nil {
		return fmt.Errorf("ensuring image %s: %w", c.ImageName, err)
	}

	var allowedDomains []string
	if c.AllowedDomainsFile != "" {
		if err := runtimedeps.Verify(ctx, cctx.AppBaseDir, runtimedeps.CustomInitImagePulled, runtimedeps.CustomKernelInstalled); err != nil {
			return err
		}
		domains, err := loadDomainsFile(c.AllowedDomainsFile)
		if err != nil {
			return fmt.Errorf("reading allowed-domains-file: %w", err)
		}
		allowedDomains = domains
	}

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if sbox == nil || err != nil {
		slog.InfoContext(ctx, "RunCmd: creating sandbox", "id", c.SandboxName)
		sbox, err = mc.CreateSandbox(ctx, daemon.CreateSandboxOpts{
			ID:             c.SandboxName,
			CloneFromDir:   c.CloneFromDir,
			ImageName:      c.ImageName,
			EnvFile:        c.EnvFile,
			Agent:          c.Agent,
			AllowedDomains: allowedDomains,
			Volumes:        c.Volume,
			CPUs:           c.CPU,
			Memory:         c.Memory,
			Username:       c.Username,
			Uid:            c.Uid,
		})
		if err != nil {
			return fmt.Errorf("creating sandbox: %w", err)
		}
	}

	var agentCmd string
	switch c.Agent {
	case "claude":
		agentCmd = `claude --permission-mode=bypassPermissions --print "$SAND_ONESHOT_PROMPT"`
	case "opencode":
		agentCmd = `opencode run "$SAND_ONESHOT_PROMPT"`
	default:
		return fmt.Errorf("one-shot mode not supported for agent %q", c.Agent)
	}

	containerSvc := hostops.NewAppleContainerOps()
	wait, err := containerSvc.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				WorkDir: "/app",
				EnvFile: sbox.EnvFile,
				Env:     map[string]string{"SAND_ONESHOT_PROMPT": c.Prompt},
				User:    c.Username,
				UID:     c.Uid,
			},
		}, sbox.ContainerID, "/bin/sh", os.Environ(),
		os.Stdin, os.Stdout, os.Stderr,
		"-c", agentCmd)
	if err != nil {
		return fmt.Errorf("starting agent in sandbox %s: %w", sbox.ID, err)
	}
	if err := wait(); err != nil {
		slog.ErrorContext(ctx, "RunCmd: agent wait", "sandbox", sbox.ID, "error", err)
	}

	if c.Rm {
		slog.InfoContext(ctx, "RunCmd: removing sandbox", "id", sbox.ID)
		if err := mc.RemoveSandbox(ctx, sbox.ID); err != nil {
			slog.ErrorContext(ctx, "RunCmd: RemoveSandbox", "error", err)
		}
	}

	return nil
}
