package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/runtimedeps"
)

type GitCmd struct {
	Diff     DiffCmd     `cmd:"" help:"diff current working directory with sandbox clone"`
	Status   StatusCmd   `cmd:"" help:"show git status of sandbox working tree"`
	Log      LogCmd      `cmd:"" help:"show git log of sandbox working tree"`
	SyncHost SyncHostCmd `cmd:"" name:"sync-host" help:"update the shared mirror for a sandbox's original host repo"`
}

type StatusCmd struct {
	SandboxNameFlag
}

type LogCmd struct {
	SandboxNameFlag
}

type SyncHostCmd struct {
	SandboxNameFlag
}

type DiffCmd struct {
	SandboxNameFlag
	Branch             string `short:"b" placeholder:"<branch name>" help:"remote branch to diff against (default: active git branch name in cwd)"`
	IncludeUncommitted bool   `short:"u" default:"false" help:"include uncommitted changes from sandbox working tree"`
}

func (c *DiffCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	if err := runtimedeps.Verify(ctx, cctx.AppBaseDir, runtimedeps.GitDir, runtimedeps.GitRemoteIsSSH); err != nil {
		return err
	}

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "name", c.SandboxName)
		return fmt.Errorf("could not find sandbox named %s: %w", c.SandboxName, err)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current working directory: %w", err)
	}

	if c.Branch == "" {
		// get the active branch name in cwd.
		cwdBranchCmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
		cwdBranchCmd.Dir = cwd
		out, err := cwdBranchCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git branch --show-current failed: %w", err)
		}
		c.Branch = strings.TrimSpace(string(out))
	}

	diffRoot, err := os.MkdirTemp("", "sand-diff-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(diffRoot)
	sandboxSnapshot := filepath.Join(diffRoot, "sandbox")
	hostSnapshot := filepath.Join(diffRoot, "host")
	for _, dir := range []string{sandboxSnapshot, hostSnapshot} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}

	sandboxAppDir := filepath.Join(sbox.SandboxWorkDir, "app")
	sandboxHadUncommittedChanges := false
	if c.IncludeUncommitted {
		var err error
		sandboxHadUncommittedChanges, err = sandboxHasUncommittedChanges(ctx, sandboxAppDir)
		if err != nil {
			return fmt.Errorf("check sandbox worktree changes: %w", err)
		}
		if err := sandboxWorktreeSnapshot(ctx, sandboxAppDir, sandboxSnapshot); err != nil {
			return fmt.Errorf("snapshot sandbox worktree: %w", err)
		}
	} else {
		cache := newGitInspectionCache(ctx, cctx.AppBaseDir, sbox)
		cacheDir, err := cache.ensureUpdated()
		if err != nil {
			return err
		}
		if err := cache.exportRef(cacheDir, cache.refForBranch(c.Branch), sandboxSnapshot); err != nil {
			return err
		}
	}

	if err := hostWorktreeSnapshot(ctx, cwd, hostSnapshot); err != nil {
		return fmt.Errorf("snapshot host worktree: %w", err)
	}

	gitDiff := newHardenedGit(ctx).command(diffRoot, "diff", "--no-index", "--no-ext-diff", "--src-prefix=sandbox/", "--dst-prefix=host/", "sandbox", "host")
	slog.InfoContext(ctx, "GitCmd.DiffCmd", "gitDiff", strings.Join(gitDiff.Args, " "))
	gitDiff.Stdout = os.Stdout
	gitDiff.Stderr = os.Stderr
	if err := gitDiff.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			return fmt.Errorf("git diff failed: %w", err)
		}
	}

	if sandboxHadUncommittedChanges {
		fmt.Fprintf(os.Stderr, "\nNote: Diff includes uncommitted changes from sandbox working tree\n")
	}

	// Print information about the sandbox
	slog.InfoContext(
		ctx, "DiffCmd completed",
		"sandbox_id", sbox.ID,
		"sandbox_name", sbox.Name,
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

func (c *StatusCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	if err := runtimedeps.Verify(ctx, cctx.AppBaseDir, runtimedeps.GitDir, runtimedeps.GitRemoteIsSSH); err != nil {
		return err
	}

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "name", c.SandboxName)
		return fmt.Errorf("could not find sandbox named %s: %w", c.SandboxName, err)
	}

	// Run git status in the sandbox working directory
	sandboxAppDir := filepath.Join(sbox.SandboxWorkDir, "app")
	gitStatus := newHardenedGit(ctx).command(sandboxAppDir, "status")
	slog.InfoContext(ctx, "GitCmd.StatusCmd", "gitStatus", strings.Join(gitStatus.Args, " "), "dir", sandboxAppDir)
	gitStatus.Stdout = os.Stdout
	gitStatus.Stderr = os.Stderr
	if err := gitStatus.Run(); err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}

	// Print information about the sandbox
	slog.InfoContext(
		ctx, "StatusCmd completed",
		"sandbox_id", sbox.ID,
		"sandbox_name", sbox.Name,
		"sandbox_workdir", sbox.SandboxWorkDir,
		"host_origin_dir", sbox.HostOriginDir,
	)

	return nil
}

func (c *LogCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	if err := runtimedeps.Verify(ctx, cctx.AppBaseDir, runtimedeps.GitDir, runtimedeps.GitRemoteIsSSH); err != nil {
		return err
	}

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "name", c.SandboxName)
		return fmt.Errorf("could not find sandbox named %s: %w", c.SandboxName, err)
	}

	cache := newGitInspectionCache(ctx, cctx.AppBaseDir, sbox)
	cacheDir, err := cache.ensureUpdated()
	if err != nil {
		return err
	}

	gitLog := newHardenedGit(ctx).command("", "--git-dir", cacheDir, "log", inspectionHeadRef)
	slog.InfoContext(ctx, "GitCmd.LogCmd", "gitLog", strings.Join(gitLog.Args, " "), "dir", cacheDir)
	gitLog.Stdout = os.Stdout
	gitLog.Stderr = os.Stderr
	if err := gitLog.Run(); err != nil {
		return fmt.Errorf("git log failed: %w", err)
	}

	// Print information about the sandbox
	slog.InfoContext(
		ctx, "LogCmd completed",
		"sandbox_id", sbox.ID,
		"sandbox_name", sbox.Name,
		"sandbox_workdir", sbox.SandboxWorkDir,
		"host_origin_dir", sbox.HostOriginDir,
	)

	return nil
}

func (c *SyncHostCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	mirrorPath, err := mc.SyncHostGitMirror(ctx, c.SandboxName)
	if err != nil {
		return fmt.Errorf("sync host git mirror for sandbox %s: %w", c.SandboxName, err)
	}
	fmt.Fprintf(os.Stdout, "updated host git mirror: %s\n", mirrorPath)
	return nil
}
