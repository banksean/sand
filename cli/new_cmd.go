package cli

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/hostops"
	"github.com/banksean/sand/mux"
	"github.com/banksean/sand/sshimmer"
	"github.com/goombaio/namegenerator"
)

type NewCmd struct {
	SandboxCreationFlags
	ShellFlags
	Cloner      string `short:"c" default:"default" placeholder:"<claude|default|opencode>" help:"name of workspace cloner to use"`
	Branch      bool   `short:"b" help:"create a new git branch inside the sandbox _container_ (not on your host workdir)"`
	Prompt      string `short:"p" placeholder:"<prompt>" help:"start the agent with this prompt in non-interactive (one-shot) mode and return immediately"`
	SandboxName string `arg:"" optional:"" help:"name of the sandbox to create"`
}

var defaultImageForCloner = map[string]string{
	"claude":   "ghcr.io/banksean/sand/claude:latest",
	"codex":    "ghcr.io/banksean/sand/codex:latest",
	"default":  "ghcr.io/banksean/sand/default:latest",
	"opencode": "ghcr.io/banksean/sand/opencode:latest",
}

func (c *NewCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.MuxClient

	slog.InfoContext(ctx, "NewCmd.Run")

	if err := VerifyPrerequisites(ctx, GitDir); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		return err
	}

	if c.CloneFromDir == "" {
		c.CloneFromDir = cwd
	}

	// Generate a new ID if one was not provided
	if c.SandboxName == "" {
		seed := time.Now().UTC().UnixNano()
		nameGenerator := namegenerator.NewNameGenerator(seed)
		c.SandboxName = nameGenerator.Generate()
	}

	if c.EnvFile != "" && !filepath.IsAbs(c.EnvFile) {
		c.EnvFile = filepath.Join(c.CloneFromDir, c.EnvFile)
	}

	// When a prompt is given, default to the claude cloner for workspace preparation.
	if c.Prompt != "" && c.Cloner == "default" {
		c.Cloner = "claude"
	}

	if c.ImageName == "" {
		if c.Cloner != "" {
			img, ok := defaultImageForCloner[c.Cloner]
			if ok {
				c.ImageName = img
			} else {
				c.ImageName = DefaultImageName
			}
		} else {
			c.ImageName = DefaultImageName
		}
	}

	// Try to get existing sandbox.
	// TODO: Consider returning an error here, rather than trying to "do the right thing" despite what the user asked for.
	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if sbox == nil || err != nil {
		// Sandbox doesn't exist, create it via daemon
		slog.InfoContext(ctx, "Creating new sandbox via daemon", "id", c.SandboxName)
		sbox, err = mc.CreateSandbox(ctx, mux.CreateSandboxOpts{
			ID:           c.SandboxName,
			CloneFromDir: c.CloneFromDir,
			ImageName:    c.ImageName,
			EnvFile:      c.EnvFile,
			Cloner:       c.Cloner,
		})
		if err != nil {
			slog.ErrorContext(ctx, "CreateSandbox", "error", err)
			return err
		}
	}

	if sbox.ImageName == "" {
		sbox.ImageName = DefaultImageName
	}

	// At this point the sandbox and container exist and are running (created by daemon)
	// Now attach to the shell directly (not through daemon)
	ctr := sbox.Container

	if ctr == nil {
		return fmt.Errorf("sandbox's container field is nil")
	}

	hostname := types.GetContainerHostname(ctr)
	env := map[string]string{
		"HOSTNAME": hostname,
	}

	slog.InfoContext(ctx, "main: sbox.new starting")

	if c.Branch {
		// Create and check out a git branch inside the container, named after the sandbox id
		containerSvc := hostops.NewAppleContainerOps()
		out, err := containerSvc.Exec(ctx,
			&options.ExecContainer{
				ProcessOptions: options.ProcessOptions{
					WorkDir: "/app",
					EnvFile: sbox.EnvFile,
				},
			}, sbox.ContainerID, "/bin/sh", os.Environ(),
			"git", "checkout", "-b", sbox.ID)
		if err != nil {
			slog.ErrorContext(ctx, "sbox.new git checkout", "error", err, "out", out)
		}
	}

	if c.Prompt != "" {
		// One-shot mode: run the agent inside the container, streaming output to stdio.
		// The prompt is passed via an env var to avoid shell quoting issues.
		containerSvc := hostops.NewAppleContainerOps()
		wait, err := containerSvc.ExecStream(ctx,
			&options.ExecContainer{
				ProcessOptions: options.ProcessOptions{
					WorkDir: "/app",
					EnvFile: sbox.EnvFile,
					Env:     map[string]string{"SAND_ONESHOT_PROMPT": c.Prompt},
				},
			}, sbox.ContainerID, "/bin/sh", os.Environ(),
			os.Stdin, os.Stdout, os.Stderr,
			"-c", `claude --permission-mode=auto --print "$SAND_ONESHOT_PROMPT"`)
		if err != nil {
			slog.ErrorContext(ctx, "NewCmd: start agent oneshot", "error", err)
			return fmt.Errorf("failed to start agent in sandbox %s: %w", sbox.ID, err)
		}
		if err := wait(); err != nil {
			slog.ErrorContext(ctx, "NewCmd: agent oneshot wait", "error", err)
		}
		return nil
	}

	updateSSHConfFunc, err := sshimmer.CheckSSHReachability(ctx, hostname)
	if err != nil {
		slog.ErrorContext(ctx, "sshimmer.CheckSSHReachability", "error", err)
	}
	if updateSSHConfFunc != nil {
		stdinReader := *bufio.NewReader(os.Stdin)
		fmt.Printf("\nTo enable you to use ssh to connect to local sand containers, we need to add one line to the top of your ssh config. Proceed [y/N]? ")
		text, err := stdinReader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("couldn't read from stdin: %w", err)
		}
		text = strings.TrimSpace(strings.ToLower(text))
		if text != "y" && text != "Y" {
			return fmt.Errorf("User declined to edit ssh config file")
		}
		if err := updateSSHConfFunc(); err != nil {
			return err
		}

	}

	// This will only work on the *host* OS, since it makes calls to apple's container service.
	// TODO: Sort out how "new" and "shell" should work when invoked inside a container.
	containerSvc := hostops.NewAppleContainerOps()
	ctrs, err := containerSvc.Inspect(ctx, sbox.ContainerID)
	if err != nil {
		return fmt.Errorf("could not inspect container for sandbox %s: %w", sbox.ContainerID, err)
	}
	if len(ctrs) == 0 {
		return fmt.Errorf("no container for sandbox %s", sbox.ContainerID)
	}
	var args []string
	switch c.Cloner {
	case "claude":
		args = []string{"-c", "claude --permission-mode=auto"}
	case "opencode":
		args = []string{"-c", "opencode --port 80 --hostname " + strings.TrimSuffix(ctrs[0].Networks[0].Hostname, ".")}
	}
	wait, err := containerSvc.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
				Env:         env,
				EnvFile:     sbox.EnvFile,
			},
		}, sbox.ContainerID, c.Shell, os.Environ(), os.Stdin, os.Stdout, os.Stderr, args...)
	if err != nil {
		slog.ErrorContext(ctx, "shell: containerService.ExecStream", "sandbox", sbox.ID, "error", err)
		return fmt.Errorf("failed to execute shell command for sandbox %s: %w", sbox.ID, err)
	}
	if err := wait(); err != nil {
		slog.ErrorContext(ctx, "sbox.new", "error", err)
	}

	if c.Rm {
		slog.InfoContext(ctx, "sbox.new finished, cleaning up...")
		if err := mc.RemoveSandbox(ctx, sbox.ID); err != nil {
			slog.ErrorContext(ctx, "RemoveSandbox", "error", err)
		}
		slog.InfoContext(ctx, "Cleanup complete. Exiting.")
	}
	return nil
}
