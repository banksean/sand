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

var _ ContainerConfiguration = &BaseContainerConfiguration{}

// NewBaseContainerConfiguration creates a new base container configuration instance.
func NewBaseContainerConfiguration() *BaseContainerConfiguration {
	return &BaseContainerConfiguration{}
}

func (c *BaseContainerConfiguration) GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec {
	mounts := []sandtypes.MountSpec{
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

	if artifacts.SharedCacheMounts.GoModuleCacheHostDir != "" {
		mounts = append(mounts, sandtypes.MountSpec{
			Source: artifacts.SharedCacheMounts.GoModuleCacheHostDir,
			Target: "/sand-go-pkgmod",
		})
	}
	if artifacts.SharedCacheMounts.GoBuildCacheHostDir != "" {
		mounts = append(mounts, sandtypes.MountSpec{
			Source: artifacts.SharedCacheMounts.GoBuildCacheHostDir,
			Target: "/sand-go-build",
		})
	}

	return mounts
}

func (c *BaseContainerConfiguration) GetStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	return nil
}

func (c *BaseContainerConfiguration) GetFirstStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	return []sandtypes.ContainerHook{
		c.defaultContainerHook(artifacts.Username, artifacts.Uid, artifacts.SharedCacheMounts),
	}
}

// defaultContainerHook sets up dotfiles and SSH in the container.
func (c *BaseContainerConfiguration) defaultContainerHook(username, uid string, sharedCaches sandtypes.SharedCacheMounts) sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("default container bootstrap", func(ctx context.Context, ctr *types.Container, exec sandtypes.HookFunc) error {
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

		// Create the parent directories before chown so the container user owns them,
		// but delay the symlink creation until after chown so we don't traverse into
		// shared host-mounted cache dirs.
		if sharedCaches.GoModuleCacheHostDir != "" {
			mkdirGoPkgOut, err := exec(ctx, "mkdir", "-p", "/home/"+username+"/go/pkg")
			if err != nil {
				slog.ErrorContext(ctx, "defaultContainerHook preparing go module cache parent", "error", err, "mkdirGoPkgOut", mkdirGoPkgOut, "username", username)
				errs = append(errs, fmt.Errorf("mkdir go module cache parent: %w", err))
			}
		}
		if sharedCaches.GoBuildCacheHostDir != "" {
			mkdirGoBuildOut, err := exec(ctx, "mkdir", "-p", "/home/"+username+"/.cache")
			if err != nil {
				slog.ErrorContext(ctx, "defaultContainerHook preparing go build cache parent", "error", err, "mkdirGoBuildOut", mkdirGoBuildOut, "username", username)
				errs = append(errs, fmt.Errorf("mkdir go build cache parent: %w", err))
			}
		}

		// Fix ownership
		cOut, err := exec(ctx, "chown", "-R", username+":"+username,
			"/home/"+username)
		if err != nil {
			slog.ErrorContext(ctx, "defaultContainerHook chown homedir", "error", err, "cOut", cOut, "username", username)
			errs = append(errs, fmt.Errorf("chown: %w", err))
		}

		if sharedCaches.GoModuleCacheHostDir != "" {
			lnGoPkgOut, err := exec(ctx, "ln", "-sfn", "/sand-go-pkgmod", "/home/"+username+"/go/pkg/mod")
			if err != nil {
				slog.ErrorContext(ctx, "defaultContainerHook linking go module cache", "error", err, "lnGoPkgOut", lnGoPkgOut, "username", username)
				errs = append(errs, fmt.Errorf("link go module cache: %w", err))
			}
		}
		if sharedCaches.GoBuildCacheHostDir != "" {
			lnGoBuildOut, err := exec(ctx, "ln", "-sfn", "/sand-go-build", "/home/"+username+"/.cache/go-build")
			if err != nil {
				slog.ErrorContext(ctx, "defaultContainerHook linking go build cache", "error", err, "lnGoBuildOut", lnGoBuildOut, "username", username)
				errs = append(errs, fmt.Errorf("link go build cache: %w", err))
			}
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
