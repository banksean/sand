package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/banksean/apple-container/sand"
)

type GitCmd struct {
	Diff DiffCmd `cmd:"" help:"diff current working directory with sandbox clone"`
}

type DiffCmd struct {
	Branch    string `short:"b" default:"" placeholder:"<branch>" help:"branch to diff against (default: sandbox ID)"`
	SandboxID string `arg:"" help:"ID of the sandbox to diff against"`
}

func (c *DiffCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get the sandbox to verify it exists and get its metadata
	mux := sand.NewMuxServer(cctx.AppBaseDir, cctx.sber)
	mc, err := mux.NewClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "NewClient", "error", err)
		return err
	}

	sbox, err := mc.GetSandbox(ctx, c.SandboxID)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "id", c.SandboxID)
		return fmt.Errorf("could not find sandbox with ID %s: %w", c.SandboxID, err)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current working directory: %w", err)
	}

	// Construct the remote name for the sandbox clone
	remoteName := fmt.Sprintf("sandbox-clone-%s", c.SandboxID)

	// First, fetch from the sandbox remote
	gitFetch := exec.CommandContext(ctx, "git", "fetch", remoteName)
	slog.InfoContext(ctx, "GitCmd.DiffCmd", "gitFetch", strings.Join(gitFetch.Args, " "))
	gitFetch.Dir = cwd
	gitFetch.Stdout = os.Stdout
	gitFetch.Stderr = os.Stderr
	if err := gitFetch.Run(); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	if c.Branch == "" {
		c.Branch = c.SandboxID
	}

	// Now diff against the specified branch from the remote
	remoteBranch := fmt.Sprintf("%s/%s", remoteName, c.Branch)
	gitDiff := exec.CommandContext(ctx, "git", "diff", remoteBranch)
	slog.InfoContext(ctx, "GitCmd.DiffCmd", "gitDiff", strings.Join(gitDiff.Args, " "))
	gitDiff.Dir = cwd
	gitDiff.Stdout = os.Stdout
	gitDiff.Stderr = os.Stderr
	if err := gitDiff.Run(); err != nil {
		return fmt.Errorf("git diff failed: %w", err)
	}

	// Print information about the sandbox
	slog.InfoContext(ctx, "DiffCmd completed",
		"sandbox_id", c.SandboxID,
		"sandbox_workdir", sbox.SandboxWorkDir,
		"host_origin_dir", sbox.HostOriginDir,
		"cwd", cwd,
	)

	// Verify we're in the right directory
	if absOrigin, err := filepath.Abs(sbox.HostOriginDir); err == nil {
		if absCwd, err := filepath.Abs(cwd); err == nil {
			if absOrigin != absCwd {
				fmt.Fprintf(os.Stderr, "\nWarning: Current directory (%s) differs from sandbox origin directory (%s)\n",
					absCwd, absOrigin)
			}
		}
	}

	return nil
}
