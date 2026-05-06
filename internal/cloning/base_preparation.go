package cloning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/banksean/sand/internal/hostops"
)

// BaseWorkspacePreparation implements the default workspace preparation behavior.
// It clones the host workspace directory, sets up dotfiles, and configures git remotes.
type BaseWorkspacePreparation struct {
	cloneRoot string
	messenger hostops.UserMessenger
	gitSetup  *GitSetup
	fileOps   hostops.FileOps
}

// NewBaseWorkspacePreparation creates a new base workspace preparation instance.
func NewBaseWorkspacePreparation(cloneRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) *BaseWorkspacePreparation {
	return &BaseWorkspacePreparation{
		cloneRoot: cloneRoot,
		messenger: messenger,
		gitSetup:  NewGitSetup(gitOps),
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
	if err := p.cloneWorkDir(ctx, req.ID, req.Name, req.HostWorkDir, pathRegistry); err != nil {
		return nil, fmt.Errorf("failed to clone workdir for sandbox %s: %w", req.ID, err)
	}

	// Create dotfiles directory
	if err := p.fileOps.MkdirAll(pathRegistry.DotfilesDir(), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create dotfiles directory for sandbox %s: %w", req.ID, err)
	}

	// Clone dotfiles
	if err := p.cloneDotfiles(ctx, req.ID, pathRegistry); err != nil {
		return nil, fmt.Errorf("failed to clone dotfiles for sandbox %s: %w", req.ID, err)
	}

	return &CloneArtifacts{
		HostWorkDir:       req.HostWorkDir,
		SandboxWorkDir:    sandboxRoot,
		PathRegistry:      pathRegistry,
		Username:          req.Username,
		Uid:               req.Uid,
		SharedCacheMounts: req.SharedCacheMounts,
	}, nil
}

func (p *BaseWorkspacePreparation) cloneWorkDir(ctx context.Context, id, name, hostWorkDir string, pathRegistry PathRegistry) error {
	p.messenger.Message(ctx, "Cloning "+hostWorkDir)

	// Check if hostWorkDir is part of a git repository
	gitTopLevel := p.gitSetup.GetGitTopLevel(ctx, hostWorkDir)
	slog.InfoContext(ctx, "BaseWorkspacePreparation.cloneWorkDir", "gitTopLevel", gitTopLevel, "hostWorkDir", hostWorkDir)
	if gitTopLevel != "" {
		// Clone from git top level instead
		hostWorkDir = gitTopLevel
	}

	// Copy files from host to sandbox
	hostCloneDir := pathRegistry.WorkDir()
	slog.InfoContext(ctx, "BaseWorkspacePreparation.cloneWorkDir", "hostCloneDir", hostCloneDir)

	workDirVol, err := p.fileOps.Volume(hostWorkDir)
	if err != nil {
		return fmt.Errorf("failed to get volume info for work dir %s: %v", hostWorkDir, err)
	}

	cloneDirVol, err := p.fileOps.Volume(p.cloneRoot)
	if err != nil {
		return fmt.Errorf("failed to get volume info for clone root dir %s: %v", p.cloneRoot, err)
	}

	if workDirVol.DeviceID != cloneDirVol.DeviceID {
		return fmt.Errorf("can't clone dirs across volumes: workdir volume %s vs clone dir volume %s", workDirVol.MountPoint, cloneDirVol.MountPoint)
	}

	if err := p.fileOps.Copy(ctx, hostWorkDir, hostCloneDir); err != nil {
		return fmt.Errorf("failed to copy workdir %s to %s for sandbox %s: %w", hostWorkDir, hostCloneDir, id, err)
	}

	// Set up git remotes if this is a git repository
	if gitTopLevel != "" {
		if err := p.gitSetup.SetupGitRemotes(ctx, id, name, gitTopLevel, hostCloneDir); err != nil {
			return err
		}
	}

	return nil
}

func (p *BaseWorkspacePreparation) cloneDotfiles(ctx context.Context, id string, pathRegistry PathRegistry) error {
	p.messenger.Message(ctx, "Cloning dotfiles...")

	// TODO: get this list from somewhere less hard-coded.
	// There are plenty of reasons one might not want to clone their dotfiles directly,
	// and instead use some written specifically for interactive sandbox shells.
	dotfiles := []string{
		".gitconfig",
		".p10k.zsh",
		".zshrc",
		".omp.json",
	}

	for _, dotfile := range dotfiles {
		clone := filepath.Join(pathRegistry.DotfilesDir(), dotfile)
		original := filepath.Join(os.Getenv("HOME"), dotfile)

		fi, err := p.fileOps.Lstat(original)
		if errors.Is(err, os.ErrNotExist) {
			p.messenger.Message(ctx, "skipping "+original)
			// Create empty file as placeholder
			f, err := os.Create(clone)
			if err != nil {
				return fmt.Errorf("failed to create empty dotfile %s for sandbox %s: %w", dotfile, id, err)
			}
			f.Close()
			continue
		}

		// Handle symbolic links
		if fi.Mode()&os.ModeSymlink != 0 {
			destination, err := p.fileOps.Readlink(original)
			if err != nil {
				slog.ErrorContext(ctx, "cloneDotfiles error reading symbolic link", "original", original, "error", err)
				continue
			}

			// Make destination absolute if it's relative
			if !filepath.IsAbs(destination) {
				destination = filepath.Join(os.Getenv("HOME"), destination)
			}

			// Check if symlink target exists
			_, err = p.fileOps.Lstat(destination)
			if errors.Is(err, os.ErrNotExist) {
				slog.ErrorContext(ctx, "cloneDotfiles symbolic link points to nonexistent file",
					"sandbox", id, "dotfile", dotfile, "original", original, "destination", destination, "error", err)
				// Create empty placeholder
				f, err := os.Create(clone)
				if err != nil {
					return fmt.Errorf("failed to create empty dotfile %s for sandbox %s: %w", dotfile, id, err)
				}
				f.Close()
				continue
			}

			slog.InfoContext(ctx, "cloneDotfiles resolved symbolic link",
				"original", original, "destination", destination)
			original = destination
		}

		// Ensure clone directory exists
		cloneDir := filepath.Dir(clone)
		if err := p.fileOps.MkdirAll(cloneDir, 0o750); err != nil {
			slog.ErrorContext(ctx, "cloneDotfiles couldn't make clone dir", "sandbox", id, "dotfile", dotfile, "cloneDir", cloneDir, "error", err)
			return fmt.Errorf("failed to create dotfile directory %s for sandbox %s: %w", cloneDir, id, err)
		}

		// Copy the dotfile
		if err := p.fileOps.Copy(ctx, original, clone); err != nil {
			return fmt.Errorf("failed to copy dotfile %s for sandbox %s: %w", dotfile, id, err)
		}

		p.messenger.Message(ctx, "cloned "+original)
	}

	return nil
}
