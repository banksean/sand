package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/cli/agentlaunch"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/goombaio/namegenerator"
)

// OneshotCmd creates a sandbox (or reuses an existing one) and runs an AI agent
// non-interactively with the given prompt, streaming output to stdout.
type OneshotCmd struct {
	SandboxCreationFlags
	Agent       string `short:"a" required:"" placeholder:"<claude|codex|gemini|opencode>" help:"coding agent to use"`
	Username    string `help:"name of default user to create (defaults to $USER)"`
	Uid         string `help:"id of default user to create (defaults to $UID)"`
	SandboxName string `short:"n" placeholder:"<name>" help:"name of the sandbox to use (generated if omitted)"`
	Stop        bool   `help:"stop the container when the command completes"`
	Prompt      string `arg:"" help:"prompt to pass to the agent"`
}

func (c *OneshotCmd) Run(cctx *CLIContext) error {
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
		c.ImageName = agentlaunch.DefaultImage(c.Agent, DefaultImageName)
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

	agentCmd, err := agentlaunch.BuildOneShotExec(c.Agent)
	if err != nil {
		return err
	}

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if sbox == nil || err != nil {
		slog.InfoContext(ctx, "OneshotCmd: creating sandbox", "id", c.SandboxName)
		fmt.Printf("creating new sandbox...\n")
		sbox, err = mc.CreateSandbox(ctx, daemon.CreateSandboxOpts{
			ID:             c.SandboxName,
			CloneFromDir:   c.CloneFromDir,
			ImageName:      c.ImageName,
			EnvFile:        c.EnvFile,
			Agent:          c.Agent,
			AllowedDomains: allowedDomains,
			Volumes:        c.Volume,
			SharedCaches:   cctx.SharedCaches,
			CPUs:           c.CPU,
			Memory:         c.Memory,
			Username:       c.Username,
			Uid:            c.Uid,
		}, os.Stdout)
		if err != nil {
			return fmt.Errorf("creating sandbox: %w", err)
		}
	}
	fmt.Printf("executing in sanbox: %s\n", sbox.ID)

	containerSvc := hostops.NewAppleContainerOps()
	hostname := types.GetContainerHostname(sbox.Container)
	env := map[string]string{
		"HOSTNAME":            hostname,
		"LANG":                os.Getenv("LANG"),
		"TERM":                os.Getenv("TERM"),
		"SAND_ONESHOT_PROMPT": c.Prompt,
	}
	wait, err := containerSvc.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
				EnvFile:     sbox.EnvFile,
				Env:         env,
				User:        c.Username,
				UID:         c.Uid,
			},
		}, sbox.ContainerID, "/bin/sh", os.Environ(),
		os.Stdin, os.Stdout, os.Stderr,
		"-c", agentCmd)
	if err != nil {
		return fmt.Errorf("starting agent in sandbox %s: %w", sbox.ID, err)
	}
	if err := wait(); err != nil {
		slog.ErrorContext(ctx, "OneshotCmd: agent wait", "sandbox", sbox.ID, "error", err)
	}

	if c.Stop {
		slog.InfoContext(ctx, "OneshotCmd: stopping sandbox container", "id", sbox.ID)
		if err := mc.StopSandbox(ctx, sbox.ID); err != nil {
			slog.ErrorContext(ctx, "OneshotCmd: StopContainer", "error", err)
		}
		fmt.Printf("stopped sandbox: %s\n", sbox.ID)
	}
	if c.Rm {
		slog.InfoContext(ctx, "OneshotCmd: removing sandbox", "id", sbox.ID)
		if err := mc.RemoveSandbox(ctx, sbox.ID); err != nil {
			slog.ErrorContext(ctx, "OneshotCmd: RemoveSandbox", "error", err)
		}
		fmt.Printf("removed sandbox: %s\n", sbox.ID)
	}

	return nil
}
