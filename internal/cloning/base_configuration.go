package cloning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/sandtypes"
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
		c.defaultContainerHook(artifacts.Username, artifacts.Uid),
	}
}

// defaultContainerHook sets up dotfiles and SSH in the container.
func (c *BaseContainerConfiguration) defaultContainerHook(username, uid string) sandtypes.ContainerStartupHook {
	return sandtypes.NewContainerStartupHook("default container bootstrap", func(ctx context.Context, ctr *types.Container, exec sandtypes.StartupHookFunc) error {
		var errs []error

		// We create a group and a user with the same name and uid as the the host user.
		// This avoids potential permissions issues with volumes mounted from host.
		agOut, err := exec(ctx, "addgroup", "-g", uid, username)
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook adding group for user", "error", err, "agOut", agOut, "username", username)
			errs = append(errs, fmt.Errorf("addgroup: %w", err))
		}

		// Create the user if they don't exist
		// Since we're on Alpine, uses busybox's `adduser` instead of the usual `useradd`.
		// -D for no password
		uaOut, err := exec(ctx, "adduser", "-u", uid, "-D", "-G", username, "-s", "/bin/zsh", username)
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook creating user", "error", err, "uaOut", uaOut, "username", username)
			errs = append(errs, fmt.Errorf("useradd: %w", err))
		}

		// Unlock the account: adduser -D sets the shadow password to "!" which
		// OpenSSH treats as a locked account and rejects before trying key/cert auth.
		puOut, err := exec(ctx, "passwd", "-u", username)
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook unlocking account", "error", err, "puOut", puOut, "username", username)
			errs = append(errs, fmt.Errorf("passwd -u: %w", err))
		}

		agwOut, err := exec(ctx, "addgroup", username, "wheel")
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook adding user to wheel", "error", err, "agwOut", agwOut, "username", username)
			errs = append(errs, fmt.Errorf("addgroup: %w", err))
		}

		// Copy dotfiles to the user's home directory
		cpOut, err := exec(ctx, "cp", "-r", "/dotfiles/.", "/home/"+username+"/.")
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook copying dotfiles", "error", err, "cpOut", cpOut, "username", username)
			errs = append(errs, fmt.Errorf("copy dotfiles: %w", err))
		}

		// Copy config and known_hosts from /root/.ssh to make sure github host keys are already known for the user.
		dotsshOut, err := exec(ctx, "cp", "-r", "/root/.ssh", "/home/"+username+"/.ssh")
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook copying /root/.ssh to ~/.ssh", "error", err, "dotsshOut", dotsshOut, "username", username)
			errs = append(errs, fmt.Errorf("copy /root/.ssh: %w", err))
		}

		// Fix ownership
		cOut, err := exec(ctx, "chown", "-R", username+":"+username,
			"/home/"+username)
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook chown homedir", "error", err, "cOut", cOut, "username", username)
			errs = append(errs, fmt.Errorf("chown: %w", err))
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
