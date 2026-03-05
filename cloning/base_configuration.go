package cloning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/sandtypes"
)

// BaseContainerConfiguration implements the default container configuration.
// It sets up standard mounts and container startup hooks for SSH and dotfiles.
type BaseContainerConfiguration struct{}

// NewBaseContainerConfiguration creates a new base container configuration instance.
func NewBaseContainerConfiguration() *BaseContainerConfiguration {
	return &BaseContainerConfiguration{}
}

func (c *BaseContainerConfiguration) GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec {
	return []sandtypes.MountSpec{
		{
			Source:   artifacts.PathRegistry.SSHKeysDir(),
			Target:   "/sshkeys",
			ReadOnly: true,
		},
		{
			Source:   artifacts.PathRegistry.DotfilesDir(),
			Target:   "/dotfiles",
			ReadOnly: true,
		},
		{
			Source: artifacts.PathRegistry.WorkDir(),
			Target: "/app",
		},
	}
}

func (c *BaseContainerConfiguration) GetStartupHooks(artifacts CloneArtifacts) []sandtypes.ContainerStartupHook {
	return []sandtypes.ContainerStartupHook{
		c.defaultContainerHook(),
		c.githubSSHContainerHook(),
	}
}

// defaultContainerHook sets up dotfiles and SSH in the container.
func (c *BaseContainerConfiguration) defaultContainerHook() sandtypes.ContainerStartupHook {
	return sandtypes.NewContainerStartupHook("default container bootstrap", func(ctx context.Context, ctr *types.Container, exec sandtypes.StartupHookFunc) error {
		var errs []error

		// Copy dotfiles to /root
		cpOut, err := exec(ctx, "cp", "-r", "/dotfiles/.", "/root/.")
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook copying dotfiles", "error", err, "cpOut", cpOut)
			errs = append(errs, fmt.Errorf("copy dotfiles: %w", err))
		}

		// Copy SSH keys to /etc/ssh
		sshkeysOut, err := exec(ctx, "cp", "-r", "/sshkeys/.", "/etc/ssh/.")
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook copying host keys", "error", err, "sshkeysOut", sshkeysOut)
			errs = append(errs, fmt.Errorf("copy host keys: %w", err))
		}

		// Set SSH key permissions
		sshkeysChmodOut, err := exec(ctx, "chmod", "600",
			"/etc/ssh/ssh_host_key",
			"/etc/ssh/ssh_host_key.pub",
			"/etc/ssh/ssh_host_key.pub-cert",
			"/etc/ssh/user_ca.pub")
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook setting host key permissions", "error", err, "sshkeysChmodOut", sshkeysChmodOut)
			errs = append(errs, fmt.Errorf("chmod host keys: %w", err))
		}

		// Start sshd
		sshdOut, err := exec(ctx, "/usr/sbin/sshd", "-f", "/etc/ssh/sshd_config")
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook starting sshd", "error", err, "sshdOut", sshdOut)
			errs = append(errs, fmt.Errorf("start sshd: %w", err))
		}

		if len(errs) > 0 {
			return errors.Join(errs...)
		}

		slog.InfoContext(ctx, "defaultContainerHook completed", "hook", "default container bootstrap")
		return nil
	})
}

// githubSSHContainerHook verifies that SSH authentication to GitHub works (via ssh-agent calling back out to the host OS).
func (c *BaseContainerConfiguration) githubSSHContainerHook() sandtypes.ContainerStartupHook {
	return sandtypes.NewContainerStartupHook("git ssh auth check", func(ctx context.Context, ctr *types.Container, exec sandtypes.StartupHookFunc) error {
		var errs []error

		sshOut, err := exec(ctx, "/usr/bin/ssh", "-T", "git@github.com")
		if err != nil && strings.Contains(sshOut, "git@github.com: Permission denied (publickey)") {
			slog.ErrorContext(ctx, "githubSSHContainerHook checking for ssh auth", "error", err, "sshOut", sshOut)
			errs = append(errs, fmt.Errorf("you may need to run `ssh-add --apple-use-keychain ~/.ssh/<github ssh key>` for ssh-agent to work with your git remote: %w", err))
		}

		return errors.Join(errs...)
	})
}
