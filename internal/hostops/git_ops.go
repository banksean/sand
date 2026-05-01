package hostops

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
)

type GitOps interface {
	AddRemote(ctx context.Context, dir, name, url string) error
	RemoveRemote(ctx context.Context, dir, name string) error
	Fetch(ctx context.Context, dir, remote string) error
	TopLevel(ctx context.Context, dir string) string
	// RemoteURL returns the URL of the named remote (e.g. "origin"), or "" if not found.
	RemoteURL(ctx context.Context, dir, name string) string
	// LocalBranchExists reports whether refs/heads/branch exists in dir.
	LocalBranchExists(ctx context.Context, dir, branch string) bool
	// Branch returns the current branch name, or "" if detached/unavailable.
	Branch(ctx context.Context, dir string) string
	// Commit returns the current HEAD commit hash, or "" if unavailable.
	Commit(ctx context.Context, dir string) string
	// IsDirty returns true if the working tree has uncommitted changes.
	IsDirty(ctx context.Context, dir string) bool
	// CommitDivergence returns head's ahead/behind counts relative to base.
	CommitDivergence(ctx context.Context, dir, base, head string) (ahead, behind int, ok bool)
}

type defaultGitOps struct{}

func NewDefaultGitOps() GitOps {
	return &defaultGitOps{}
}

func (g *defaultGitOps) AddRemote(ctx context.Context, dir, name, url string) error {
	cmd := exec.CommandContext(ctx, "git", "remote", "add", name, url)
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.AddRemote", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.AddRemote", "error", err, "output", string(output))
		return fmt.Errorf("git remote add failed: %w (output: %s)", err, output)
	}
	return nil
}

func (g *defaultGitOps) RemoveRemote(ctx context.Context, dir, name string) error {
	cmd := exec.CommandContext(ctx, "git", "remote", "remove", name)
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.RemoveRemote", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.RemoveRemote", "error", err, "output", string(output))
		return fmt.Errorf("git remote remove failed: %w (output: %s)", err, output)
	}
	return nil
}

func (g *defaultGitOps) Fetch(ctx context.Context, dir, remote string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", remote)
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.Fetch", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.Fetch", "error", err, "output", string(output))
		return fmt.Errorf("git fetch failed: %w (output: %s)", err, output)
	}
	return nil
}

func (g *defaultGitOps) TopLevel(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.TopLevel", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.TopLevel", "error", err, "output", string(output))
		return ""
	}

	return strings.TrimSpace(string(output))
}

func (g *defaultGitOps) RemoteURL(ctx context.Context, dir, name string) string {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", name)
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.RemoteURL", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.RemoteURL", "error", err, "output", string(output))
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (g *defaultGitOps) LocalBranchExists(ctx context.Context, dir, branch string) bool {
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.LocalBranchExists", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	if err := cmd.Run(); err != nil {
		slog.InfoContext(ctx, "GitOps.LocalBranchExists", "error", err)
		return false
	}
	return true
}

func (g *defaultGitOps) Branch(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.Branch", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.Branch", "error", err, "output", string(output))
		return ""
	}
	branch := strings.TrimSpace(string(output))
	if branch == "HEAD" {
		// detached HEAD state
		return ""
	}
	return branch
}

func (g *defaultGitOps) Commit(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.Commit", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.Commit", "error", err, "output", string(output))
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (g *defaultGitOps) IsDirty(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.IsDirty", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.IsDirty", "error", err, "output", string(output))
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

func (g *defaultGitOps) CommitDivergence(ctx context.Context, dir, base, head string) (ahead, behind int, ok bool) {
	if base == "" || head == "" {
		return 0, 0, false
	}
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", base+"..."+head)
	cmd.Dir = dir
	slog.InfoContext(ctx, "GitOps.CommitDivergence", "cmd", strings.Join(cmd.Args, " "), "dir", dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "GitOps.CommitDivergence", "error", err, "output", string(output))
		return 0, 0, false
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		slog.InfoContext(ctx, "GitOps.CommitDivergence unexpected output", "output", string(output))
		return 0, 0, false
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		slog.InfoContext(ctx, "GitOps.CommitDivergence parse behind", "error", err, "output", string(output))
		return 0, 0, false
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		slog.InfoContext(ctx, "GitOps.CommitDivergence parse ahead", "error", err, "output", string(output))
		return 0, 0, false
	}
	return ahead, behind, true
}
