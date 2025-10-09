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
	Diff   DiffCmd   `cmd:"" help:"diff current working directory with sandbox clone"`
	Status StatusCmd `cmd:"" help:"show git status of sandbox working tree"`
	Log    LogCmd    `cmd:"" help:"show git log of sandbox working tree"`
}

type StatusCmd struct {
	SandboxID string `arg:"" help:"ID of the sandbox to get status from"`
}

type LogCmd struct {
	SandboxID string `arg:"" help:"ID of the sandbox to get log from"`
}

type DiffCmd struct {
	Branch             string `short:"b" default:"" placeholder:"<branch>" help:"branch to diff against (default: sandbox ID)"`
	IncludeUncommitted bool   `short:"u" default:"false" help:"include uncommitted changes from sandbox working tree"`
	SandboxID          string `arg:"" help:"ID of the sandbox to diff against"`
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
	remoteName := sand.ClonedWorkDirRemotePrefix + c.SandboxID

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

	// If including uncommitted changes, create a temporary commit in the sandbox
	var tempCommitCreated bool
	if c.IncludeUncommitted {
		if err := c.createTempCommit(ctx, sbox.SandboxWorkDir); err != nil {
			return fmt.Errorf("failed to create temporary commit: %w", err)
		}
		tempCommitCreated = true
		defer func() {
			if err := c.cleanupTempCommit(ctx, sbox.SandboxWorkDir); err != nil {
				slog.ErrorContext(ctx, "failed to cleanup temporary commit", "error", err)
			}
		}()

		// Fetch again to get the temporary commit
		gitFetch := exec.CommandContext(ctx, "git", "fetch", remoteName)
		slog.InfoContext(ctx, "GitCmd.DiffCmd", "gitFetch (with temp commit)", strings.Join(gitFetch.Args, " "))
		gitFetch.Dir = cwd
		gitFetch.Stdout = os.Stdout
		gitFetch.Stderr = os.Stderr
		if err := gitFetch.Run(); err != nil {
			return fmt.Errorf("git fetch failed: %w", err)
		}
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

	if tempCommitCreated {
		fmt.Fprintf(os.Stderr, "\nNote: Diff includes uncommitted changes from sandbox working tree\n")
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

// createTempCommit creates a temporary commit in the sandbox working directory
// that includes all uncommitted changes (staged and unstaged).
func (c *DiffCmd) createTempCommit(ctx context.Context, sandboxWorkDir string) error {
	sandboxAppDir := filepath.Join(sandboxWorkDir, "app")
	slog.InfoContext(ctx, "createTempCommit", "dir", sandboxAppDir)

	// Add all changes to the index
	gitAdd := exec.CommandContext(ctx, "git", "add", "-A")
	gitAdd.Dir = sandboxAppDir
	slog.InfoContext(ctx, "createTempCommit gitAdd", "cmd", strings.Join(gitAdd.Args, " "))
	if output, err := gitAdd.CombinedOutput(); err != nil {
		slog.ErrorContext(ctx, "git add failed", "error", err, "output", string(output))
		return fmt.Errorf("git add failed: %w", err)
	}

	// Create a temporary commit
	gitCommit := exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", "[TEMPORARY] sand git diff --include-uncommitted")
	gitCommit.Dir = sandboxAppDir
	slog.InfoContext(ctx, "createTempCommit gitCommit", "cmd", strings.Join(gitCommit.Args, " "))
	if output, err := gitCommit.CombinedOutput(); err != nil {
		slog.ErrorContext(ctx, "git commit failed", "error", err, "output", string(output))
		return fmt.Errorf("git commit failed: %w", err)
	}

	return nil
}

// cleanupTempCommit removes the temporary commit created by createTempCommit.
func (c *DiffCmd) cleanupTempCommit(ctx context.Context, sandboxWorkDir string) error {
	sandboxAppDir := filepath.Join(sandboxWorkDir, "app")
	slog.InfoContext(ctx, "cleanupTempCommit", "dir", sandboxAppDir)

	// Reset to the previous commit, keeping working tree changes
	gitReset := exec.CommandContext(ctx, "git", "reset", "--soft", "HEAD~1")
	gitReset.Dir = sandboxAppDir
	slog.InfoContext(ctx, "cleanupTempCommit gitReset", "cmd", strings.Join(gitReset.Args, " "))
	if output, err := gitReset.CombinedOutput(); err != nil {
		slog.ErrorContext(ctx, "git reset failed", "error", err, "output", string(output))
		return fmt.Errorf("git reset failed: %w", err)
	}

	// Unstage all changes to restore the original state
	gitResetFiles := exec.CommandContext(ctx, "git", "reset", "HEAD")
	gitResetFiles.Dir = sandboxAppDir
	slog.InfoContext(ctx, "cleanupTempCommit gitResetFiles", "cmd", strings.Join(gitResetFiles.Args, " "))
	if output, err := gitResetFiles.CombinedOutput(); err != nil {
		slog.ErrorContext(ctx, "git reset HEAD failed", "error", err, "output", string(output))
		return fmt.Errorf("git reset HEAD failed: %w", err)
	}

	return nil
}

func (c *StatusCmd) Run(cctx *Context) error {
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

	// Construct the remote name for the sandbox clone
	remoteName := sand.ClonedWorkDirRemotePrefix + c.SandboxID

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current working directory: %w", err)
	}

	// First, fetch from the sandbox remote
	gitFetch := exec.CommandContext(ctx, "git", "fetch", remoteName)
	slog.InfoContext(ctx, "GitCmd.StatusCmd", "gitFetch", strings.Join(gitFetch.Args, " "))
	gitFetch.Dir = cwd
	gitFetch.Stdout = os.Stdout
	gitFetch.Stderr = os.Stderr
	if err := gitFetch.Run(); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	// Run git status in the sandbox working directory
	sandboxAppDir := filepath.Join(sbox.SandboxWorkDir, "app")
	gitStatus := exec.CommandContext(ctx, "git", "status")
	slog.InfoContext(ctx, "GitCmd.StatusCmd", "gitStatus", strings.Join(gitStatus.Args, " "), "dir", sandboxAppDir)
	gitStatus.Dir = sandboxAppDir
	gitStatus.Stdout = os.Stdout
	gitStatus.Stderr = os.Stderr
	if err := gitStatus.Run(); err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}

	// Print information about the sandbox
	slog.InfoContext(ctx, "StatusCmd completed",
		"sandbox_id", c.SandboxID,
		"sandbox_workdir", sbox.SandboxWorkDir,
		"host_origin_dir", sbox.HostOriginDir,
	)

	return nil
}

func (c *LogCmd) Run(cctx *Context) error {
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

	// Construct the remote name for the sandbox clone
	remoteName := sand.ClonedWorkDirRemotePrefix + c.SandboxID

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current working directory: %w", err)
	}

	// First, fetch from the sandbox remote
	gitFetch := exec.CommandContext(ctx, "git", "fetch", remoteName)
	slog.InfoContext(ctx, "GitCmd.LogCmd", "gitFetch", strings.Join(gitFetch.Args, " "))
	gitFetch.Dir = cwd
	gitFetch.Stdout = os.Stdout
	gitFetch.Stderr = os.Stderr
	if err := gitFetch.Run(); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	// Run git log in the sandbox working directory
	sandboxAppDir := filepath.Join(sbox.SandboxWorkDir, "app")
	gitLog := exec.CommandContext(ctx, "git", "log")
	slog.InfoContext(ctx, "GitCmd.LogCmd", "gitLog", strings.Join(gitLog.Args, " "), "dir", sandboxAppDir)
	gitLog.Dir = sandboxAppDir
	gitLog.Stdout = os.Stdout
	gitLog.Stderr = os.Stderr
	if err := gitLog.Run(); err != nil {
		return fmt.Errorf("git log failed: %w", err)
	}

	// Print information about the sandbox
	slog.InfoContext(ctx, "LogCmd completed",
		"sandbox_id", c.SandboxID,
		"sandbox_workdir", sbox.SandboxWorkDir,
		"host_origin_dir", sbox.HostOriginDir,
	)

	return nil
}
