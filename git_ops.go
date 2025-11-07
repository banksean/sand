package sand

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

type GitOps interface {
	AddRemote(ctx context.Context, dir, name, url string) error
	RemoveRemote(ctx context.Context, dir, name string) error
	Fetch(ctx context.Context, dir, remote string) error
	TopLevel(ctx context.Context, dir string) string
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
