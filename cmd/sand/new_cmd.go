package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/banksean/sand"
	"github.com/banksean/sand/sshimmer"
	"github.com/goombaio/namegenerator"
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

	if err := verifyPrerequisites(ctx, GitDir); err != nil {
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
		seed := time.Now().UTC().UnixNano()
		nameGenerator := namegenerator.NewNameGenerator(seed)
		c.ID = nameGenerator.Generate()
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
