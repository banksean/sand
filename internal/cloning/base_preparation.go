package cloning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
)

// BaseWorkspacePreparation implements the default workspace preparation behavior.
// It clones the host workspace directory, sets up dotfiles, and configures git remotes.
type BaseWorkspacePreparation struct {
	cloneRoot string
	messenger hostops.UserMessenger
	gitSetup  *GitSetup
	gitMirror *GitMirror
	fileOps   hostops.FileOps
}

// NewBaseWorkspacePreparation creates a new base workspace preparation instance.
func NewBaseWorkspacePreparation(cloneRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) *BaseWorkspacePreparation {
	return &BaseWorkspacePreparation{
		cloneRoot: cloneRoot,
		messenger: messenger,
		gitSetup:  NewGitSetup(gitOps),
		gitMirror: NewGitMirror(DefaultGitMirrorRoot(cloneRoot), gitOps, fileOps),
		fileOps:   fileOps,
	}
}

func (p *BaseWorkspacePreparation) Prepare(ctx context.Context, req CloneRequest) (*CloneArtifacts, error) {
	slog.InfoContext(ctx, "BaseWorkspacePreparation.Prepare", "req", req)

	sandboxRoot := filepath.Join(p.cloneRoot, req.ID)
	pathRegistry := NewStandardPathRegistry(sandboxRoot)

	// Create sandbox root directory
	if err := p.fileOps.MkdirAll(sandboxRoot, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create clone directory for sandbox %s: %w", req.ID, err)
	}

	// Clone workspace directory
	hostWorkDir, hostGitMirrorDir, err := p.cloneWorkDir(ctx, req.ID, req.Name, req.HostWorkDir, pathRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to clone workdir for sandbox %s: %w", req.ID, err)
	}

	// Create dotfiles directory
	if err := p.fileOps.MkdirAll(pathRegistry.DotfilesDir(), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create dotfiles directory for sandbox %s: %w", req.ID, err)
	}

	// Clone dotfiles
	if err := p.cloneDotfiles(ctx, req, pathRegistry); err != nil {
		return nil, fmt.Errorf("failed to clone dotfiles for sandbox %s: %w", req.ID, err)
	}

	return &CloneArtifacts{
		HostWorkDir:       hostWorkDir,
		HostGitMirrorDir:  hostGitMirrorDir,
		SandboxWorkDir:    sandboxRoot,
		PathRegistry:      pathRegistry,
		Username:          req.Username,
		Uid:               req.Uid,
		SharedCacheMounts: req.SharedCacheMounts,
	}, nil
}

func (p *BaseWorkspacePreparation) cloneWorkDir(ctx context.Context, id, name, hostWorkDir string, pathRegistry PathRegistry) (string, string, error) {
	p.messenger.Message(ctx, "Cloning "+hostWorkDir)

	// Check if hostWorkDir is part of a git repository
	gitTopLevel := p.gitSetup.GetGitTopLevel(ctx, hostWorkDir)
	slog.InfoContext(ctx, "BaseWorkspacePreparation.cloneWorkDir", "gitTopLevel", gitTopLevel, "hostWorkDir", hostWorkDir)
	var hostGitMirrorDir string
	if gitTopLevel != "" {
		// Clone from git top level instead
		hostWorkDir = gitTopLevel
	}

	// Copy files from host to sandbox
	hostCloneDir := pathRegistry.WorkDir()
	slog.InfoContext(ctx, "BaseWorkspacePreparation.cloneWorkDir", "hostCloneDir", hostCloneDir)

	workDirVol, err := p.fileOps.Volume(hostWorkDir)
	if err != nil {
		return "", "", fmt.Errorf("failed to get volume info for work dir %s: %v", hostWorkDir, err)
	}

	cloneDirVol, err := p.fileOps.Volume(p.cloneRoot)
	if err != nil {
		return "", "", fmt.Errorf("failed to get volume info for clone root dir %s: %v", p.cloneRoot, err)
	}

	if workDirVol.DeviceID != cloneDirVol.DeviceID {
		return "", "", fmt.Errorf("can't clone dirs across volumes: workdir volume %s vs clone dir volume %s", workDirVol.MountPoint, cloneDirVol.MountPoint)
	}

	if gitTopLevel != "" {
		var err error
		hostGitMirrorDir, err = p.gitMirror.EnsureUpdated(ctx, gitTopLevel)
		if err != nil {
			return "", "", err
		}
	}

	if err := p.fileOps.Copy(ctx, hostWorkDir, hostCloneDir); err != nil {
		return "", "", fmt.Errorf("failed to copy workdir %s to %s for sandbox %s: %w", hostWorkDir, hostCloneDir, id, err)
	}

	// Set up git remotes if this is a git repository
	if gitTopLevel != "" {
		if err := p.gitSetup.SetupGitRemotes(ctx, id, name, gitTopLevel, hostCloneDir, hostGitMirrorDir); err != nil {
			return "", "", err
		}
	}

	return hostWorkDir, hostGitMirrorDir, nil
}

func (p *BaseWorkspacePreparation) cloneDotfiles(ctx context.Context, req CloneRequest, pathRegistry PathRegistry) error {
	p.messenger.Message(ctx, "Cloning dotfiles...")

	for _, rule := range dotfileRules(req.Profile.Dotfiles) {
		source, target, err := normalizeDotfileRule(req.HostWorkDir, rule)
		if err != nil {
			return err
		}
		if err := p.cloneDotfileRule(ctx, req.ID, pathRegistry, source, target, rule); err != nil {
			return err
		}
	}

	return nil
}

func dotfileRules(policy sandtypes.DotfilePolicy) []sandtypes.DotfileRule {
	switch policy.Mode {
	case sandtypes.DotfileModeNone:
		return nil
	case sandtypes.DotfileModeAllowlist, sandtypes.DotfileModeMinimal, "":
		return policy.Files
	default:
		return policy.Files
	}
}

func normalizeDotfileRule(hostWorkDir string, rule sandtypes.DotfileRule) (string, string, error) {
	source := expandHome(rule.Source)
	if source == "" {
		return "", "", fmt.Errorf("dotfile source is required")
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(hostWorkDir, source)
	}

	target := rule.Target
	if target == "" {
		target = filepath.Base(source)
	}
	target = strings.TrimPrefix(target, "~/")
	if filepath.IsAbs(target) {
		home := os.Getenv("HOME")
		rel, err := filepath.Rel(home, target)
		if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
			return "", "", fmt.Errorf("dotfile target %q must be inside $HOME", rule.Target)
		}
		target = rel
	}
	target = filepath.Clean(target)
	if target == "." || strings.HasPrefix(target, "..") {
		return "", "", fmt.Errorf("dotfile target %q must be relative to $HOME", rule.Target)
	}

	return source, target, nil
}

func expandHome(path string) string {
	if path == "~" {
		return os.Getenv("HOME")
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(os.Getenv("HOME"), strings.TrimPrefix(path, "~/"))
	}
	return path
}

func (p *BaseWorkspacePreparation) cloneDotfileRule(ctx context.Context, id string, pathRegistry PathRegistry, source, target string, rule sandtypes.DotfileRule) error {
	original, err := p.resolveDotfileSource(ctx, source, rule)
	if err != nil {
		return fmt.Errorf("dotfile %s for sandbox %s: %w", source, id, err)
	}

	clone := filepath.Join(pathRegistry.DotfilesDir(), target)
	cloneDir := filepath.Dir(clone)
	if err := p.fileOps.MkdirAll(cloneDir, 0o750); err != nil {
		slog.ErrorContext(ctx, "cloneDotfiles couldn't make clone dir", "sandbox", id, "target", target, "cloneDir", cloneDir, "error", err)
		return fmt.Errorf("failed to create dotfile directory %s for sandbox %s: %w", cloneDir, id, err)
	}

	if err := p.fileOps.Copy(ctx, original, clone); err != nil {
		return fmt.Errorf("failed to copy dotfile %s for sandbox %s: %w", target, id, err)
	}

	p.messenger.Message(ctx, "cloned "+original)
	return nil
}

func (p *BaseWorkspacePreparation) resolveDotfileSource(ctx context.Context, source string, rule sandtypes.DotfileRule) (string, error) {
	fi, err := p.fileOps.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("source does not exist")
	}
	if err != nil {
		return "", err
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		return source, nil
	}
	if !rule.AllowSymlink {
		return "", fmt.Errorf("source is a symlink")
	}

	destination, err := p.fileOps.Readlink(source)
	if err != nil {
		slog.ErrorContext(ctx, "cloneDotfiles error reading symbolic link", "source", source, "error", err)
		return "", err
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(filepath.Dir(source), destination)
	}
	destination = filepath.Clean(destination)
	if !rule.AllowOutsideHome && !pathInsideHome(destination) {
		return "", fmt.Errorf("symlink target %q is outside $HOME", destination)
	}
	if _, err := p.fileOps.Lstat(destination); err != nil {
		return "", err
	}

	slog.InfoContext(ctx, "cloneDotfiles resolved symbolic link", "source", source, "destination", destination)
	return destination, nil
}

func pathInsideHome(path string) bool {
	home := os.Getenv("HOME")
	rel, err := filepath.Rel(home, path)
	return err == nil && !strings.HasPrefix(rel, "..")
}
