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
	"github.com/banksean/sand/box"
	"github.com/banksean/sand/mux"
	"github.com/banksean/sand/sshimmer"
	"github.com/goombaio/namegenerator"
)

type NewCmd struct {
	ImageName    string `short:"i" default:"ghcr.io/banksean/sand/default:latest" placeholder:"<container-image-name>" help:"name of container image to use"`
	Shell        string `short:"s" default:"/bin/zsh" placeholder:"<shell-command>" help:"shell command to exec in the container"`
	Cloner       string `short:"c" default:"default" placeholder:"<claude|default|opencode>" help:"name of workspace cloner to use"`
	CloneFromDir string `short:"d" placeholder:"<project-dir>" help:"directory to clone into the sandbox. Defaults to current working directory, if unset."`
	EnvFile      string `short:"e" placholder:"<file-path>" help:"path to env file to use when creating a new shell"`
	Branch       bool   `short:"b" help:"create a new git branch inside the sandbox _container_ (not on your host workdir)"`
	Rm           bool   `help:"remove the sandbox after the shell terminates"`
	ID           string `arg:"" optional:"" help:"ID of the sandbox to create, or re-attach to"`
}

var (
	defaultImageForCloner = map[string]string{
		"claude":   "ghcr.io/banksean/sand/default:latest",
		"opencode": "ghcr.io/banksean/sand/opencode:latest",
		"codex":    "ghcr.io/banksean/sand/codex:latest",
	}
)

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
	if c.ID == "" {
		seed := time.Now().UTC().UnixNano()
		nameGenerator := namegenerator.NewNameGenerator(seed)
		c.ID = nameGenerator.Generate()
	}

	if c.EnvFile != "" && !filepath.IsAbs(c.EnvFile) {
		c.EnvFile = filepath.Join(c.CloneFromDir, c.EnvFile)
	}

	if c.ImageName == "" && c.Cloner != "" {
		img, ok := defaultImageForCloner[c.Cloner]
		if ok {
			c.ImageName = img
		}
	}

	// Try to get existing sandbox.
	// TODO: Consider returning an error here, rather than trying to "do the right thing" despite what the user asked for.
	sbox, err := mc.GetSandbox(ctx, c.ID)
	if sbox == nil || err != nil {
		// Sandbox doesn't exist, create it via daemon
		slog.InfoContext(ctx, "Creating new sandbox via daemon", "id", c.ID)
		sbox, err = mc.CreateSandbox(ctx, mux.CreateSandboxOpts{
			ID:           c.ID,
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
		sbox.ImageName = box.DefaultImageName
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
	fmt.Printf("container hostname: %s\n", hostname)

	slog.InfoContext(ctx, "main: sbox.new starting")

	if c.Branch {
		// Create and check out a git branch inside the container, named after the sandbox id
		out, err := sbox.Exec(ctx, "git", "checkout", "-b", sbox.ID)
		if err != nil {
			slog.ErrorContext(ctx, "sbox.new git checkout", "error", err, "out", out)
		}
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
	containerSvc := box.NewAppleContainerOps()
	wait, err := containerSvc.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
				Env:         env,
				EnvFile:     sbox.EnvFile,
			},
		}, sbox.ContainerID, c.Shell, os.Environ(), os.Stdin, os.Stdout, os.Stderr)
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
