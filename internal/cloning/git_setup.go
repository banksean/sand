package cloning

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/banksean/sand/internal/hostops"
)

const (
	// OriginalWorkDirRemoteName is the name of the git remote pointing to the original host workspace
	OriginalWorkDirRemoteName = "origin"
	ContainerSideGitOrigin    = "/run/git-origin-ro"
	// ClonedWorkDirGitRemotePrefix is the prefix for git remotes pointing to cloned workspaces
	ClonedWorkDirGitRemotePrefix = "sand/"
)

// GitSetup handles git-specific operations for workspace cloning.
// It sets up bidirectional git remotes between the host workspace and the sandbox clone.
type GitSetup struct {
	gitOps hostops.GitOps
}

// NewGitSetup creates a new GitSetup instance.
func NewGitSetup(gitOps hostops.GitOps) *GitSetup {
	return &GitSetup{gitOps: gitOps}
}

// SetupGitRemotes configures bidirectional git remotes between the host and clone directories.
// This allows easy synchronization of changes between the original workspace and the sandbox.
//
// Returns nil if hostDir is not a git repository (no error).
func (g *GitSetup) SetupGitRemotes(ctx context.Context, sandboxID, hostDir, cloneDir string) error {
	slog.InfoContext(ctx, "GitSetup.SetupGitRemotes", "hostDir", hostDir, "cloneDir", cloneDir)
	// Check if hostDir is part of a git repository
	gitTopLevel := g.gitOps.TopLevel(ctx, hostDir)
	if gitTopLevel == "" {
		// Not a git repo, nothing to do
		return nil
	}

	// Add remote in host pointing to cloned workdir
	remoteName := ClonedWorkDirGitRemotePrefix + sandboxID
	if err := g.gitOps.AddRemote(ctx, gitTopLevel, remoteName, cloneDir); err != nil {
		return fmt.Errorf("failed to add git remote (cloned work dir) %s for sandbox %s: %w",
			remoteName, sandboxID, err)
	}

	// Fetch from original workdir into clone
	if err := g.gitOps.Fetch(ctx, cloneDir, OriginalWorkDirRemoteName); err != nil {
		return fmt.Errorf("failed to fetch git remote %s for sandbox %s: %w",
			OriginalWorkDirRemoteName, sandboxID, err)
	}

	// Fetch from clone into original workdir
	if err := g.gitOps.Fetch(ctx, gitTopLevel, remoteName); err != nil {
		return fmt.Errorf("failed to fetch git remote %s for sandbox %s: %w",
			remoteName, sandboxID, err)
	}

	return nil
}

// GetGitTopLevel returns the top-level directory of the git repository containing the given directory.
// Returns empty string if the directory is not part of a git repository.
func (g *GitSetup) GetGitTopLevel(ctx context.Context, dir string) string {
	slog.InfoContext(ctx, "GitSetup.GetGitTopLevel", "dir", dir)

	return g.gitOps.TopLevel(ctx, dir)
}
