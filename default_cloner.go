package sand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	originalWorkdDirRemoteName   = "origin-host-workdir"
	ClonedWorkDirGitRemotePrefix = "sand/"
)

// DefaultWorkspaceCloner reproduces the current cloning behaviour.
type DefaultWorkspaceCloner struct {
	appRoot   string
	cloneRoot string
	messenger UserMessenger
	gitOps    GitOps
	fileOps   FileOps
}

// NewDefaultWorkspaceCloner constructs the default provisioner used by Boxer.
func NewDefaultWorkspaceCloner(appRoot string, terminalWriter io.Writer) WorkspaceCloner {
	return &DefaultWorkspaceCloner{
		appRoot:   appRoot,
		cloneRoot: filepath.Join(appRoot, "clones"),
		messenger: NewTerminalMessenger(terminalWriter),
		gitOps:    NewDefaultGitOps(),
		fileOps:   NewDefaultFileOps(),
	}
}

func (p *DefaultWorkspaceCloner) Prepare(ctx context.Context, req CloneRequest) (*CloneResult, error) {
	slog.InfoContext(ctx, "DefaultWorkspaceCloner.Prepare", "req", req)
	if err := p.fileOps.MkdirAll(filepath.Join(p.cloneRoot, req.ID), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create clone directory for sandbox %s: %w", req.ID, err)
	}

	if err := p.cloneWorkDir(ctx, req.ID, req.HostWorkDir); err != nil {
		return nil, fmt.Errorf("failed to clone workdir for sandbox %s: %w", req.ID, err)
	}

	if err := p.fileOps.MkdirAll(filepath.Join(p.cloneRoot, req.ID, "dotfiles"), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create dotfiles directory for sandbox %s: %w", req.ID, err)
	}

	if err := p.cloneDotfiles(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("failed to clone dotfiles for sandbox %s: %w", req.ID, err)
	}

	sandboxWorkDir := filepath.Join(p.cloneRoot, req.ID)

	mounts := p.mountPlanFor(sandboxWorkDir)
	hooks := []ContainerStartupHook{
		p.defaultContainerHook(),
		p.githubSSHContainerHook(),
	}

	return &CloneResult{
		SandboxWorkDir: sandboxWorkDir,
		Mounts:         mounts,
		ContainerHooks: hooks,
	}, nil
}

// Hydrate populates runtime-only fields on a Box that was loaded from persistent storage.
func (p *DefaultWorkspaceCloner) Hydrate(ctx context.Context, box *Box) error {
	slog.InfoContext(ctx, "DefaultWorkspaceCloner.Hydrate", "req", box)

	if box == nil {
		return fmt.Errorf("nil box provided to hydrate")
	}
	sandboxWorkDir := box.SandboxWorkDir
	if sandboxWorkDir == "" {
		return fmt.Errorf("sandbox %s missing workdir", box.ID)
	}
	box.Mounts = p.mountPlanFor(sandboxWorkDir)
	box.ContainerHooks = append(box.ContainerHooks, p.defaultContainerHook())
	return nil
}

func (p *DefaultWorkspaceCloner) mountPlanFor(sandboxWorkDir string) []MountSpec {
	return []MountSpec{
		{
			Source:   filepath.Join(sandboxWorkDir, "sshkeys"),
			Target:   "/sshkeys",
			ReadOnly: true,
		},
		{
			Source:   filepath.Join(sandboxWorkDir, "dotfiles"),
			Target:   "/dotfiles",
			ReadOnly: true,
		},
		{
			Source: filepath.Join(sandboxWorkDir, "app"),
			Target: "/app",
		},
	}
}

func (p *DefaultWorkspaceCloner) cloneWorkDir(ctx context.Context, id, hostWorkDir string) error {
	p.messenger.Message(ctx, "Cloning "+hostWorkDir)
	gitTopLevel := p.gitOps.TopLevel(ctx, hostWorkDir)
	if gitTopLevel != "" {
		hostWorkDir = gitTopLevel
	}
	if err := p.fileOps.MkdirAll(filepath.Join(p.cloneRoot, id), 0o750); err != nil {
		return fmt.Errorf("failed to create clone root for sandbox %s: %w", id, err)
	}
	hostCloneDir := filepath.Join(p.cloneRoot, id, "app")
	if err := p.fileOps.Copy(ctx, hostWorkDir, hostCloneDir); err != nil {
		return fmt.Errorf("failed to copy workdir %s to %s for sandbox %s: %w", hostWorkDir, hostCloneDir, id, err)
	}

	// Exit early and skip the git remote stuff if the working directory isn't part of a git repo.
	if gitTopLevel == "" {
		return nil
	}

	if err := p.gitOps.AddRemote(ctx, hostCloneDir, originalWorkdDirRemoteName, gitTopLevel); err != nil {
		return fmt.Errorf("failed to add git remote (original work dir) %s for sandbox %s: %w", originalWorkdDirRemoteName, id, err)
	}

	if err := p.gitOps.AddRemote(ctx, gitTopLevel, ClonedWorkDirGitRemotePrefix+id, hostCloneDir); err != nil {
		return fmt.Errorf("failed to add git remote (cloned work dir) %s for sandbox %s: %w", ClonedWorkDirGitRemotePrefix+id, id, err)
	}

	if err := p.gitOps.Fetch(ctx, hostCloneDir, originalWorkdDirRemoteName); err != nil {
		return fmt.Errorf("failed to fetch git remote %s for sandbox %s: %w", originalWorkdDirRemoteName, id, err)
	}

	if err := p.gitOps.Fetch(ctx, gitTopLevel, ClonedWorkDirGitRemotePrefix+id); err != nil {
		return fmt.Errorf("failed to fetch git remote %s for sandbox %s: %w", ClonedWorkDirGitRemotePrefix+id, id, err)
	}

	return nil
}

// cloneSSHIdentityKeys sets up /etc/ssh so that users can ssh into the container without TOFU or a password.
// TODO: move this out of DefaultWorkspaceCloner since it's not workspace-specific.
func (p *DefaultWorkspaceCloner) cloneSSHIdentityKeys(ctx context.Context, id string) error {
	userCAPub := filepath.Join(os.Getenv("HOME"), ".config", "sand", "user_ca.pub")
	hostKey := filepath.Join(os.Getenv("HOME"), ".config", "sand", "ssh_host_key")
	hostKeyPub := filepath.Join(os.Getenv("HOME"), ".config", "sand", "ssh_host_key.pub")
	hostKeyCert := filepath.Join(os.Getenv("HOME"), ".config", "sand", "ssh_host_key.pub-cert")

	cloneHostKeyDir := filepath.Join(p.cloneRoot, id, "sshkeys")
	if err := p.fileOps.MkdirAll(cloneHostKeyDir, 0o750); err != nil {
		return fmt.Errorf("failed to create sshkeys directory for sandbox %s: %w", id, err)
	}

	if err := p.fileOps.Copy(ctx, hostKey, filepath.Join(cloneHostKeyDir, "ssh_host_key")); err != nil {
		return fmt.Errorf("failed to copy host key for sandbox %s: %w", id, err)
	}

	if err := p.fileOps.Copy(ctx, hostKeyPub, filepath.Join(cloneHostKeyDir, "ssh_host_key.pub")); err != nil {
		return fmt.Errorf("failed to copy host pub key for sandbox %s: %w", id, err)
	}

	if err := p.fileOps.Copy(ctx, hostKeyCert, filepath.Join(cloneHostKeyDir, "ssh_host_key.pub-cert")); err != nil {
		return fmt.Errorf("failed to copy host pub key for sandbox %s: %w", id, err)
	}

	if err := p.fileOps.Copy(ctx, userCAPub, filepath.Join(cloneHostKeyDir, "user_ca.pub")); err != nil {
		return fmt.Errorf("failed to copy user pub key for sandbox %s: %w", id, err)
	}
	return nil
}

func (p *DefaultWorkspaceCloner) cloneDotfiles(ctx context.Context, id string) error {
	p.messenger.Message(ctx, "Cloning dotfiles...")
	dotfiles := []string{
		".gitconfig",
		".p10k.zsh",
		".zshrc",
		".omp.json",
	}
	for _, dotfile := range dotfiles {
		clone := filepath.Join(p.cloneRoot, id, "dotfiles", dotfile)
		original := filepath.Join(os.Getenv("HOME"), dotfile)
		fi, err := p.fileOps.Lstat(original)
		if errors.Is(err, os.ErrNotExist) {
			p.messenger.Message(ctx, "skipping "+original)
			f, err := p.fileOps.Create(clone)
			if err != nil {
				return fmt.Errorf("failed to create empty dotfile %s for sandbox %s: %w", dotfile, id, err)
			}
			f.Close()
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			destination, err := p.fileOps.Readlink(original)
			if err != nil {
				slog.ErrorContext(ctx, "Boxer.cloneDotfiles error reading symbolic link", "original", original, "error", err)
				continue
			}
			if !filepath.IsAbs(destination) {
				destination = filepath.Join(os.Getenv("HOME"), destination)
			}
			_, err = p.fileOps.Lstat(destination)
			if errors.Is(err, os.ErrNotExist) {
				slog.ErrorContext(ctx, "Boxer.cloneDotfiles symbolic link points to nonexistent file",
					"sandbox", id, "dotfile", dotfile, "original", original, "destination", destination, "error", err)
				f, err := p.fileOps.Create(clone)
				if err != nil {
					return fmt.Errorf("failed to create empty dotfile %s for sandbox %s: %w", dotfile, id, err)
				}
				f.Close()
				continue
			}
			slog.InfoContext(ctx, "Boxer.cloneDotfiles resolved symbolic link",
				"original", original, "destination", destination)
			original = destination
		}
		cloneDir := filepath.Dir(clone)
		if err := p.fileOps.MkdirAll(cloneDir, 0o750); err != nil {
			slog.ErrorContext(ctx, "cloneDotfiles couldn't make clone dir", "sandbox", id, "dotfile", dotfile, "cloneDir", cloneDir, "error", err)
			return fmt.Errorf("failed to create dotfile directory %s for sandbox %s: %w", cloneDir, id, err)
		}
		if err := p.fileOps.Copy(ctx, original, clone); err != nil {
			return fmt.Errorf("failed to copy dotfile %s for sandbox %s: %w", dotfile, id, err)
		}
		p.messenger.Message(ctx, "cloned "+original)
	}

	return nil
}

// Make sure `ssh git@github.com` can authenticate with ssh-agent.
// TODO: make this work with other remotes that aren't hosted on github.com.
func (p *DefaultWorkspaceCloner) githubSSHContainerHook() ContainerStartupHook {
	return NewContainerStartupHook("git ssh auth check", func(ctx context.Context, b *Box) error {
		var errs []error

		sshOut, err := b.Exec(ctx, "/usr/bin/ssh", "-T", "git@github.com")
		if err != nil && strings.Contains(sshOut, "git@github.com: Permission denied (publickey)") {
			slog.ErrorContext(ctx, "DefaultContainerHook checking for ssh auth", "error", err, "sshOut", sshOut)
			errs = append(errs, fmt.Errorf("you may need to run `ssh-add --apple-use-keychain ~/.ssh/<github ssh key>` for ssh-agent to work with your git remote: %w", err))
		}
		return errors.Join(errs...)
	})
}

func (p *DefaultWorkspaceCloner) defaultContainerHook() ContainerStartupHook {
	return NewContainerStartupHook("default container bootstrap", func(ctx context.Context, b *Box) error {
		var errs []error

		cpOut, err := b.Exec(ctx, "cp", "-r", "/dotfiles/.", "/root/.")
		if err != nil {
			slog.ErrorContext(ctx, "DefaultContainerHook copying dotfiles", "error", err, "cpOut", cpOut)
			errs = append(errs, fmt.Errorf("copy dotfiles: %w", err))
		}

		sshkeysOut, err := b.Exec(ctx, "cp", "-r", "/sshkeys/.", "/etc/ssh/.")
		if err != nil {
			slog.ErrorContext(ctx, "DefaultContainerHook copying host keys", "error", err, "sshkeysOut", sshkeysOut)
			errs = append(errs, fmt.Errorf("copy host keys: %w", err))
		}

		sshkeysChmodOut, err := b.Exec(ctx, "chmod", "600",
			"/etc/ssh/ssh_host_key",
			"/etc/ssh/ssh_host_key.pub",
			"/etc/ssh/ssh_host_key.pub-cert",
			"/etc/ssh/user_ca.pub")
		if err != nil {
			slog.ErrorContext(ctx, "DefaultContainerHook setting host key permissions", "error", err, "sshkeysChmodOut", sshkeysChmodOut)
			errs = append(errs, fmt.Errorf("chmod host keys: %w", err))
		}

		sshdOut, err := b.Exec(ctx, "/usr/sbin/sshd", "-f", "/etc/ssh/sshd_config")
		if err != nil {
			slog.ErrorContext(ctx, "DefaultContainerHook starting sshd", "error", err, "sshdOut", sshdOut)
			errs = append(errs, fmt.Errorf("start sshd: %w", err))
		}

		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		slog.InfoContext(ctx, "DefaultContainerHook completed", "hook", "default container bootstrap")
		return nil
	})
}
