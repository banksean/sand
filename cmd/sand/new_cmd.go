package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/banksean/sand"
	"github.com/google/uuid"
)

type NewCmd struct {
	ImageName    string `short:"i" default:"ghcr.io/banksean/sand/default:latest" placeholder:"<container-image-name>" help:"name of container image to use"`
	Shell        string `short:"s" default:"/bin/zsh" placeholder:"<shell-command>" help:"shell command to exec in the container"`
	Cloner       string `default:"default" placeholder:"<claude|default|opencode>" help:"name of workspace cloner to use"`
	CloneFromDir string `short:"c" placeholder:"<project-dir>" help:"directory to clone into the sandbox. Defaults to current working directory, if unset."`
	EnvFile      string `short:"e" placholder:"<file-path>" help:"path to env file to use when creating a new shell"`
	Branch       bool   `short:"b" help:"create a new git branch inside the sandbox _container_ (not on your host workdir)"`
	Rm           bool   `help:"remove the sandbox after the shell terminates"`
	ID           string `arg:"" optional:"" help:"ID of the sandbox to create, or re-attach to"`
}

func (c *NewCmd) Run(cctx *Context) error {
	ctx := cctx.Context

	slog.InfoContext(ctx, "NewCmd.Run")

	if err := verifyPrerequisites(ctx, "git-dir", "git-ssh-checkout"); err != nil {
		return err
	}

	if err := cctx.sber.EnsureImage(ctx, c.ImageName); err != nil {
		slog.ErrorContext(ctx, "sber.Init", "error", err)
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

	// Generate ID if not provided
	if c.ID == "" {
		c.ID = uuid.NewString()
	}

	// Use MuxClient to check if sandbox exists or create it
	mux := sand.NewMuxServer(cctx.AppBaseDir, cctx.sber)
	mc, err := mux.NewClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "NewClient", "error", err)
		return err
	}

	if c.EnvFile != "" && !filepath.IsAbs(c.EnvFile) {
		c.EnvFile = filepath.Join(c.CloneFromDir, c.EnvFile)
	}

	// Try to get existing sandbox
	sbox, err := mc.GetSandbox(ctx, c.ID)
	if sbox == nil || err != nil {
		// Sandbox doesn't exist, create it via daemon
		slog.InfoContext(ctx, "Creating new sandbox via daemon", "id", c.ID)
		sbox, err = mc.CreateSandbox(ctx, sand.CreateSandboxOpts{
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
		sbox.ImageName = sand.DefaultImageName
	}

	// At this point the sandbox and container exist and are running (created by daemon)
	// Now attach to the shell directly (not through daemon)
	ctr, err := sbox.GetContainer(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "sbox.GetContainer", "error", err)
		return err
	}

	if ctr == nil {
		if err := sbox.CreateContainer(ctx); err != nil {
			return err
		}
		ctr, err = sbox.GetContainer(ctx)
		if err != nil {
			return err
		}
	}
	hostname := getContainerHostname(ctr)
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

	// Shell attachment is direct, not through daemon
	if err := sbox.Shell(ctx, env, c.Shell, os.Stdin, os.Stdout, os.Stderr); err != nil {
		slog.ErrorContext(ctx, "sbox.new", "error", err)
	}

	if c.Rm {
		slog.InfoContext(ctx, "sbox.new finished, cleaning up...")
		// Use daemon for cleanup
		if err := mc.RemoveSandbox(ctx, sbox.ID); err != nil {
			slog.ErrorContext(ctx, "RemoveSandbox", "error", err)
		}
		slog.InfoContext(ctx, "Cleanup complete. Exiting.")
	}
	return nil
}
