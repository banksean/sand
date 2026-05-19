package cli

import (
	"context"
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
	Sync     SyncCmd     `cmd:"" help:"pull committed sandbox changes into the host worktree"`
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

type SyncCmd struct {
	SandboxName   string `arg:"" completion-predictor:"sandbox-name" help:"name of the sandbox"`
	HostBranch    string `arg:"" optional:"" placeholder:"<host branch name>" help:"host branch to create or update (default: sandbox name)"`
	SandboxBranch string `placeholder:"<branch name>" help:"sandbox branch to pull from (default: host branch)"`
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

func (c *SyncCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	if err := runtimedeps.Verify(ctx, cctx.AppBaseDir, runtimedeps.GitDir); err != nil {
		return err
	}
	if err := validateSyncSandboxName(c.SandboxName); err != nil {
		return err
	}

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "name", c.SandboxName)
		return fmt.Errorf("could not find sandbox named %s: %w", c.SandboxName, err)
	}
	if sbox == nil {
		return fmt.Errorf("could not find sandbox named %s", c.SandboxName)
	}

	hostBranch := c.HostBranch
	if hostBranch == "" {
		hostBranch = c.SandboxName
	}
	sandboxBranch := c.SandboxBranch
	if sandboxBranch == "" {
		sandboxBranch = hostBranch
	}
	if err := validateSyncBranch(ctx, hostBranch, "host branch"); err != nil {
		return err
	}
	if err := validateSyncBranch(ctx, sandboxBranch, "sandbox branch"); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not get current working directory: %w", err)
	}
	hostRoot, err := gitCommandOutput(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if err := requireInsideSandboxOrigin(cwd, sbox.HostOriginDir); err != nil {
		return err
	}
	if err := requireSandboxOriginRoot(hostRoot, sbox.HostOriginDir); err != nil {
		return err
	}

	remoteName := "sand/" + c.SandboxName
	sandboxAppDir := filepath.Join(sbox.SandboxWorkDir, "app")
	if err := requireSandboxRemote(ctx, hostRoot, remoteName, sandboxAppDir); err != nil {
		return err
	}
	sandboxFetchPath, err := syncCanonicalPath(sandboxAppDir)
	if err != nil {
		return fmt.Errorf("resolve sandbox git path %s: %w", sandboxAppDir, err)
	}
	if !sandboxBranchExists(ctx, sandboxFetchPath, sandboxBranch) {
		return fmt.Errorf("sandbox branch %q was not found at %s/%s", sandboxBranch, remoteName, sandboxBranch)
	}

	remoteRef := "refs/remotes/" + remoteName + "/" + sandboxBranch
	sandboxBranchRef := "refs/heads/" + sandboxBranch
	if err := runGitTransport(ctx, hostRoot, "fetch", "--prune", sandboxFetchPath, "+"+sandboxBranchRef+":"+remoteRef); err != nil {
		return err
	}
	if !gitRefExists(ctx, hostRoot, remoteRef) {
		return fmt.Errorf("sandbox branch %q was not found at %s/%s", sandboxBranch, remoteName, sandboxBranch)
	}

	if gitRefExists(ctx, hostRoot, "refs/heads/"+hostBranch) {
		if err := requireBranchTracks(ctx, hostRoot, hostBranch, remoteName, sandboxBranch); err != nil {
			return err
		}
		if err := runGitWithHint(ctx, hostRoot, "switch to host branch", "git switch failed; commit or stash host changes, then retry", "switch", hostBranch); err != nil {
			return err
		}
	} else {
		remoteTrackingBranch := remoteName + "/" + sandboxBranch
		if err := runGitWithHint(ctx, hostRoot, "create host branch", "git switch failed; commit or stash host changes, then retry", "switch", "-c", hostBranch, "--track", remoteTrackingBranch); err != nil {
			return err
		}
	}

	if err := runGitTransportWithHint(ctx, hostRoot, "pull sandbox branch", "git pull failed; resolve the host worktree state, then retry", "pull", sandboxFetchPath, sandboxBranch); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "synced %s/%s into host branch %s\n", remoteName, sandboxBranch, hostBranch)
	return nil
}

func requireInsideSandboxOrigin(path, origin string) error {
	absPath, err := syncCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path %s: %w", path, err)
	}
	absOrigin, err := syncCanonicalPath(origin)
	if err != nil {
		return fmt.Errorf("resolve sandbox origin %s: %w", origin, err)
	}
	rel, err := filepath.Rel(absOrigin, absPath)
	if err != nil {
		return fmt.Errorf("compare current directory with sandbox origin: %w", err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil
	}
	return fmt.Errorf("current git repository %s is outside sandbox origin directory %s", absPath, absOrigin)
}

func requireSandboxOriginRoot(path, origin string) error {
	absPath, err := syncCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve git repository root %s: %w", path, err)
	}
	absOrigin, err := syncCanonicalPath(origin)
	if err != nil {
		return fmt.Errorf("resolve sandbox origin %s: %w", origin, err)
	}
	if absPath != absOrigin {
		return fmt.Errorf("current git repository root %s does not match sandbox origin directory %s", absPath, absOrigin)
	}
	return nil
}

func syncCanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return filepath.Clean(abs), nil
	}
	return filepath.Clean(resolved), nil
}

func requireSandboxRemote(ctx context.Context, dir, remoteName, sandboxAppDir string) error {
	remoteURL, err := gitCommandOutput(ctx, dir, "remote", "get-url", remoteName)
	if err != nil {
		return fmt.Errorf("sandbox remote %q is not configured in %s", remoteName, dir)
	}
	if sameFilesystemPath(dir, remoteURL, sandboxAppDir) {
		return nil
	}
	return fmt.Errorf("sandbox remote %q points to %s, want %s", remoteName, remoteURL, sandboxAppDir)
}

func sameFilesystemPath(baseDir, a, b string) bool {
	if !filepath.IsAbs(a) {
		a = filepath.Join(baseDir, a)
	}
	if !filepath.IsAbs(b) {
		b = filepath.Join(baseDir, b)
	}
	absA, errA := syncCanonicalPath(a)
	absB, errB := syncCanonicalPath(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}

func requireBranchTracks(ctx context.Context, dir, branch, remoteName, sandboxBranch string) error {
	upstream, err := gitCommandOutput(ctx, dir, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil || upstream != remoteName+"/"+sandboxBranch {
		return fmt.Errorf("host branch %q already exists but does not track %s/%s", branch, remoteName, sandboxBranch)
	}
	return nil
}

func gitRefExists(ctx context.Context, dir, ref string) bool {
	cmd := newHardenedGit(ctx).command(dir, "show-ref", "--verify", "--quiet", ref)
	return cmd.Run() == nil
}

func sandboxBranchExists(ctx context.Context, sandboxPath, branch string) bool {
	cmd := hardenedGitTransportCommand(ctx, "", "ls-remote", "--exit-code", "--heads", sandboxPath, branch)
	return cmd.Run() == nil
}

func gitCommandOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := newHardenedGit(ctx).command(dir, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w (output: %s)", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := newHardenedGit(ctx).command(dir, args...)
	slog.InfoContext(ctx, "GitCmd.SyncCmd", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		fmt.Fprint(os.Stdout, string(output))
	}
	if err != nil {
		return fmt.Errorf("git %s failed: %w (output: %s)", strings.Join(args, " "), err, output)
	}
	return nil
}

func runGitTransport(ctx context.Context, dir string, args ...string) error {
	cmd := hardenedGitTransportCommand(ctx, dir, args...)
	slog.InfoContext(ctx, "GitCmd.SyncCmd", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		fmt.Fprint(os.Stdout, string(output))
	}
	if err != nil {
		return fmt.Errorf("git %s failed: %w (output: %s)", strings.Join(args, " "), err, output)
	}
	return nil
}

func hardenedGitTransportCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	base := []string{
		"-c", "protocol.allow=never",
		"-c", "protocol.file.allow=always",
		"-c", "protocol.ext.allow=never",
	}
	cmd := newHardenedGit(ctx).command(dir, append(base, args...)...)
	cmd.Env = append(cmd.Env, "GIT_ALLOW_PROTOCOL=file")
	return cmd
}

func validateSyncSandboxName(name string) error {
	if name == "" {
		return fmt.Errorf("sandbox name is required")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid sandbox name %q: must not start with '-'", name)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("invalid sandbox name %q: must not contain '/'", name)
	}
	if !validGitRefComponent(name) {
		return fmt.Errorf("invalid sandbox name %q", name)
	}
	return nil
}

func validateSyncBranch(ctx context.Context, branch, label string) error {
	if branch == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid %s %q: must not start with '-'", label, branch)
	}
	if strings.Contains(branch, "@{") {
		return fmt.Errorf("invalid %s %q", label, branch)
	}
	cmd := newHardenedGit(ctx).command("", "check-ref-format", "--branch", branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("invalid %s %q (output: %s)", label, branch, output)
	}
	return nil
}

func validGitRefComponent(component string) bool {
	if component == "" || component == "." || component == ".." ||
		strings.HasPrefix(component, ".") ||
		strings.HasSuffix(component, ".") ||
		strings.HasSuffix(component, ".lock") ||
		strings.Contains(component, "..") ||
		strings.Contains(component, "@{") {
		return false
	}
	for _, r := range component {
		if r <= 0x20 || r == 0x7f {
			return false
		}
		switch r {
		case '~', '^', ':', '?', '*', '[', '\\':
			return false
		}
	}
	return true
}

func runGitWithHint(ctx context.Context, dir, action, hint string, args ...string) error {
	if err := runGit(ctx, dir, args...); err != nil {
		return fmt.Errorf("%s: %w\nHint: %s", action, err, hint)
	}
	return nil
}

func runGitTransportWithHint(ctx context.Context, dir, action, hint string, args ...string) error {
	if err := runGitTransport(ctx, dir, args...); err != nil {
		return fmt.Errorf("%s: %w\nHint: %s", action, err, hint)
	}
	return nil
}
