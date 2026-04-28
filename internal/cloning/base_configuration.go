package cloning

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/sandtypes"
)

// BaseContainerConfiguration implements the default container configuration.
// It sets up standard mounts and container startup hooks for SSH and dotfiles.
type BaseContainerConfiguration struct{}

var _ ContainerConfiguration = &BaseContainerConfiguration{}

const (
	miseCachePath    = "/opt/tool-cache/mise"
	goModCachePath   = miseCachePath + "/go/mod"
	goBuildCachePath = miseCachePath + "/go/build"
	apkCachePath     = "/var/cache/apk"
)

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

	if artifacts.SharedCacheMounts.MiseCacheHostDir != "" {
		mounts = append(mounts, sandtypes.MountSpec{
			Source: artifacts.SharedCacheMounts.MiseCacheHostDir,
			Target: miseCachePath,
		})
	}

	if artifacts.SharedCacheMounts.APKCacheHostDir != "" {
		mounts = append(mounts, sandtypes.MountSpec{
			Source: artifacts.SharedCacheMounts.APKCacheHostDir,
			Target: apkCachePath,
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

func (c *BaseContainerConfiguration) defaultContainerHook(username, uid string, sharedCaches sandtypes.SharedCacheMounts) sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("default container bootstrap", func(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer) error {
		osRelease, err := exec.Exec(ctx, "cat", "/etc/os-release")
		if err != nil {
			return fmt.Errorf("checking /etc/os-release: %w", err)
		}

		if strings.Contains(osRelease, "ID=alpine") {
			return c.defaultAlpineContainerHook(ctx, ctr, exec, username, uid, sharedCaches)
		}
		return c.defaultUbuntuContainerHook(ctx, ctr, exec, username, uid, sharedCaches)
	})
}

// defaultAlpineContainerHook sets up dotfiles and SSH in Alpine-based containers.
func (c *BaseContainerConfiguration) defaultAlpineContainerHook(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer, username, uid string, sharedCaches sandtypes.SharedCacheMounts) error {
	var errs []error
	// We create a group and a user with the same name and uid as the the host user.
	// This avoids potential permissions issues with volumes mounted from host.
	agOut, err := exec.Exec(ctx, "addgroup", "-g", uid, username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook adding group for user", "error", err, "agOut", agOut, "username", username)
		errs = append(errs, fmt.Errorf("addgroup: %w", err))
	}

	// Create the user if they don't exist
	// Since we're on Alpine, uses busybox's `adduser` instead of the usual `useradd`.
	// -D for no password
	uaOut, err := exec.Exec(ctx, "adduser", "-u", uid, "-D", "-G", username, "-s", "/bin/zsh", username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook creating user", "error", err, "uaOut", uaOut, "username", username)
		errs = append(errs, fmt.Errorf("useradd: %w", err))
	}

	// Unlock the account: adduser -D sets the shadow password to "!" which
	// OpenSSH treats as a locked account and rejects before trying key/cert auth.
	puOut, err := exec.Exec(ctx, "passwd", "-u", username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook unlocking account", "error", err, "puOut", puOut, "username", username)
		errs = append(errs, fmt.Errorf("passwd -u: %w", err))
	}

	agwOut, err := exec.Exec(ctx, "addgroup", username, "wheel")
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook adding user to wheel", "error", err, "agwOut", agwOut, "username", username)
		errs = append(errs, fmt.Errorf("addgroup: %w", err))
	}

	// Copy dotfiles to the user's home directory
	cpOut, err := exec.Exec(ctx, "cp", "-r", "/dotfiles/.", "/home/"+username+"/.")
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook copying dotfiles", "error", err, "cpOut", cpOut, "username", username)
		errs = append(errs, fmt.Errorf("copy dotfiles: %w", err))
	}

	// Copy config and known_hosts from /root/.ssh to make sure github host keys are already known for the user.
	dotsshOut, err := exec.Exec(ctx, "cp", "-r", "/root/.ssh", "/home/"+username+"/.ssh")
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook copying /root/.ssh to ~/.ssh", "error", err, "dotsshOut", dotsshOut, "username", username)
		errs = append(errs, fmt.Errorf("copy /root/.ssh: %w", err))
	}

	// Create the parent directories before chown so the container user owns them,
	// but delay the symlink creation until after chown so we don't traverse into
	// shared host-mounted cache dirs.
	if sharedCaches.MiseCacheHostDir != "" {
		mkdirGoPkgOut, err := exec.Exec(ctx, "mkdir", "-p", "/home/"+username+"/go/pkg")
		if err != nil {
			slog.ErrorContext(ctx, "defaultAlpineContainerHook preparing go module cache parent", "error", err, "mkdirGoPkgOut", mkdirGoPkgOut, "username", username)
			errs = append(errs, fmt.Errorf("mkdir go module cache parent: %w", err))
		}

		mkdirGoBuildOut, err := exec.Exec(ctx, "mkdir", "-p", "/home/"+username+"/.cache")
		if err != nil {
			slog.ErrorContext(ctx, "defaultAlpineContainerHook preparing go build cache parent", "error", err, "mkdirGoBuildOut", mkdirGoBuildOut, "username", username)
			errs = append(errs, fmt.Errorf("mkdir go build cache parent: %w", err))
		}
	}

	// Fix ownership
	cOut, err := exec.Exec(ctx, "chown", "-R", username+":"+username,
		"/home/"+username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook chown homedir", "error", err, "cOut", cOut, "username", username)
		errs = append(errs, fmt.Errorf("chown: %w", err))
	}

	if sharedCaches.APKCacheHostDir != "" {
		statOut, err := exec.Exec(ctx, "stat", "/etc/apk")
		if err != nil {
			slog.ErrorContext(ctx, "defaultAlpineContainerHook checking for /var/cache/apk", "error", err, "statOut", statOut)
		} else if out, err := exec.Exec(ctx, "ln", "-s", "/var/cache/apk", "/etc/apk/cache"); err != nil {
			slog.ErrorContext(ctx, "defaultAlpineContainerHook linking apk cache", "error", err, "out", out)
			errs = append(errs, fmt.Errorf("link apk cache: %w", err))
		}
	}

	// mise.sh exports GOMODCACHE/GOCACHE directly, and these symlinks keep
	// direct process execs aligned with the same mise-backed cache paths.
	if sharedCaches.MiseCacheHostDir != "" {
		lnGoPkgOut, err := exec.Exec(ctx, "ln", "-sfn", goModCachePath, "/home/"+username+"/go/pkg/mod")
		if err != nil {
			slog.ErrorContext(ctx, "defaultAlpineContainerHook linking go module cache", "error", err, "lnGoPkgOut", lnGoPkgOut, "username", username)
			errs = append(errs, fmt.Errorf("link go module cache: %w", err))
		}

		lnGoBuildOut, err := exec.Exec(ctx, "ln", "-sfn", goBuildCachePath, "/home/"+username+"/.cache/go-build")
		if err != nil {
			slog.ErrorContext(ctx, "defaultAlpineContainerHook linking go build cache", "error", err, "lnGoBuildOut", lnGoBuildOut, "username", username)
			errs = append(errs, fmt.Errorf("link go build cache: %w", err))
		}
	}

	// Copy SSH keys to /etc/ssh
	sshkeysOut, err := exec.Exec(ctx, "cp", "-r", "/sshkeys/.", "/etc/ssh/.")
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook copying host keys", "error", err, "sshkeysOut", sshkeysOut)
		errs = append(errs, fmt.Errorf("copy host keys: %w", err))
	}

	// Set SSH key permissions
	sshkeysChmodOut, err := exec.Exec(ctx, "chmod", "600",
		"/etc/ssh/ssh_host_key",
		"/etc/ssh/ssh_host_key.pub",
		"/etc/ssh/ssh_host_key.pub-cert",
		"/etc/ssh/user_ca.pub")
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook setting host key permissions", "error", err, "sshkeysChmodOut", sshkeysChmodOut)
		errs = append(errs, fmt.Errorf("chmod host keys: %w", err))
	}

	// Start sshd
	sshdOut, err := exec.Exec(ctx, "/usr/sbin/sshd", "-f", "/etc/ssh/sshd_config")
	if err != nil {
		slog.ErrorContext(ctx, "defaultAlpineContainerHook starting sshd", "error", err, "sshdOut", sshdOut)
		errs = append(errs, fmt.Errorf("start sshd: %w", err))
	}

	if sharedCaches.MiseCacheHostDir != "" {
		var miseBuf bytes.Buffer
		whichMise, err := exec.Exec(ctx, "which", "mise.sh")
		if err != nil {
			slog.ErrorContext(ctx, "defaultAlpineContainerHook checking for mise.sh", "error", err, "whichMiseOut", whichMise)
		} else if err = exec.ExecStream(ctx, &miseBuf, &miseBuf, "mise.sh"); err != nil {
			entryPointOut := miseBuf.String()
			slog.ErrorContext(ctx, "defaultAlpineContainerHook starting mise.sh", "error", err, "entryPointOut", entryPointOut)
			errs = append(errs, fmt.Errorf("mise.sh: %w", err))
		}
	}

	slog.InfoContext(ctx, "defaultAlpineContainerHook completed", "hook", "default container bootstrap")
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// defaultAlpineContainerHook sets up dotfiles and SSH in Alpine-based containers.
func (c *BaseContainerConfiguration) defaultUbuntuContainerHook(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer, username, uid string, sharedCaches sandtypes.SharedCacheMounts) error {
	var errs []error
	// We create a group and a user with the same name and uid as the the host user.
	// This avoids potential permissions issues with volumes mounted from host.
	agOut, err := exec.Exec(ctx, "groupadd", "-g", uid, username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook adding group for user", "error", err, "agOut", agOut, "username", username)
		errs = append(errs, fmt.Errorf("groupadd: %w", err))
	}

	// Create the user if they don't exist
	// -p for no password
	uaOut, err := exec.Exec(ctx, "useradd", "-u", uid, "-g", username, "-s", "/bin/zsh", username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook creating user", "error", err, "uaOut", uaOut, "username", username)
		errs = append(errs, fmt.Errorf("useradd: %w", err))
	}

	// TODO: usermod -aG sudo your_username
	umOut, err := exec.Exec(ctx, "usermod", "-a", "-G", "sudo", username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook creating user", "error", err, "umOut", umOut, "username", username)
		errs = append(errs, fmt.Errorf("usermod: %w", err))
	}

	// Make user home directory
	mdOut, err := exec.Exec(ctx, "mkdir", "-p", "/home/"+username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook making user home dir", "error", err, "mdOut", mdOut, "username", username)
		errs = append(errs, fmt.Errorf("making user home dir: %w", err))
	}

	// Copy dotfiles to the user's home directory
	cpOut, err := exec.Exec(ctx, "cp", "-r", "/dotfiles/.", "/home/"+username+"/.")
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook copying dotfiles", "error", err, "cpOut", cpOut, "username", username)
		errs = append(errs, fmt.Errorf("copy dotfiles: %w", err))
	}

	// Copy config and known_hosts from /root/.ssh to make sure github host keys are already known for the user.
	dotsshOut, err := exec.Exec(ctx, "cp", "-r", "/root/.ssh", "/home/"+username+"/.ssh")
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook copying /root/.ssh to ~/.ssh", "error", err, "dotsshOut", dotsshOut, "username", username)
		errs = append(errs, fmt.Errorf("copy /root/.ssh: %w", err))
	}

	// Create the parent directories before chown so the container user owns them,
	// but delay the symlink creation until after chown so we don't traverse into
	// shared host-mounted cache dirs.
	if sharedCaches.MiseCacheHostDir != "" {
		mkdirGoPkgOut, err := exec.Exec(ctx, "mkdir", "-p", "/home/"+username+"/go/pkg")
		if err != nil {
			slog.ErrorContext(ctx, "defaultUbuntuContainerHook preparing go module cache parent", "error", err, "mkdirGoPkgOut", mkdirGoPkgOut, "username", username)
			errs = append(errs, fmt.Errorf("mkdir go module cache parent: %w", err))
		}

		mkdirGoBuildOut, err := exec.Exec(ctx, "mkdir", "-p", "/home/"+username+"/.cache")
		if err != nil {
			slog.ErrorContext(ctx, "defaultUbuntuContainerHook preparing go build cache parent", "error", err, "mkdirGoBuildOut", mkdirGoBuildOut, "username", username)
			errs = append(errs, fmt.Errorf("mkdir go build cache parent: %w", err))
		}
	}

	// Fix ownership
	cOut, err := exec.Exec(ctx, "chown", "-R", username+":"+username,
		"/home/"+username)
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook chown homedir", "error", err, "cOut", cOut, "username", username)
		errs = append(errs, fmt.Errorf("chown: %w", err))
	}

	// TODO: equivalent package caching for ubuntu

	// if sharedCaches.APKCacheHostDir != "" {
	// 	statOut, err := exec.Exec(ctx, "stat", "/etc/apk")
	// 	if err != nil {
	// 		slog.ErrorContext(ctx, "defaultUbuntuContainerHook checking for /var/cache/apk", "error", err, "statOut", statOut)
	// 	} else if out, err := exec.Exec(ctx, "ln", "-s", "/var/cache/apk", "/etc/apk/cache"); err != nil {
	// 		slog.ErrorContext(ctx, "defaultUbuntuContainerHook linking apk cache", "error", err, "out", out)
	// 		errs = append(errs, fmt.Errorf("link apk cache: %w", err))
	// 	}
	// }

	// mise.sh exports GOMODCACHE/GOCACHE directly, and these symlinks keep
	// direct process execs aligned with the same mise-backed cache paths.
	if sharedCaches.MiseCacheHostDir != "" {
		lnGoPkgOut, err := exec.Exec(ctx, "ln", "-sfn", goModCachePath, "/home/"+username+"/go/pkg/mod")
		if err != nil {
			slog.ErrorContext(ctx, "defaultUbuntuContainerHook linking go module cache", "error", err, "lnGoPkgOut", lnGoPkgOut, "username", username)
			errs = append(errs, fmt.Errorf("link go module cache: %w", err))
		}

		lnGoBuildOut, err := exec.Exec(ctx, "ln", "-sfn", goBuildCachePath, "/home/"+username+"/.cache/go-build")
		if err != nil {
			slog.ErrorContext(ctx, "defaultUbuntuContainerHook linking go build cache", "error", err, "lnGoBuildOut", lnGoBuildOut, "username", username)
			errs = append(errs, fmt.Errorf("link go build cache: %w", err))
		}
	}

	// Copy SSH keys to /etc/ssh
	sshkeysOut, err := exec.Exec(ctx, "cp", "-r", "/sshkeys/.", "/etc/ssh/.")
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook copying host keys", "error", err, "sshkeysOut", sshkeysOut)
		errs = append(errs, fmt.Errorf("copy host keys: %w", err))
	}

	// Set SSH key permissions
	sshkeysChmodOut, err := exec.Exec(ctx, "chmod", "600",
		"/etc/ssh/ssh_host_key",
		"/etc/ssh/ssh_host_key.pub",
		"/etc/ssh/ssh_host_key.pub-cert",
		"/etc/ssh/user_ca.pub")
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook setting host key permissions", "error", err, "sshkeysChmodOut", sshkeysChmodOut)
		errs = append(errs, fmt.Errorf("chmod host keys: %w", err))
	}

	mdSshOut, err := exec.Exec(ctx, "mkdir", "-p", "/run/sshd")
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook creating /run/sshd", "error", err, "mdSshOut", mdSshOut)
		errs = append(errs, fmt.Errorf("creating /run/sshd: %w", err))
	}

	// Start sshd
	sshdOut, err := exec.Exec(ctx, "/usr/sbin/sshd", "-f", "/etc/ssh/sshd_config")
	if err != nil {
		slog.ErrorContext(ctx, "defaultUbuntuContainerHook starting sshd", "error", err, "sshdOut", sshdOut)
		errs = append(errs, fmt.Errorf("start sshd: %w", err))
	}

	if sharedCaches.MiseCacheHostDir != "" {
		var miseBuf bytes.Buffer
		whichMise, err := exec.Exec(ctx, "which", "mise.sh")
		if err != nil {
			slog.ErrorContext(ctx, "defaultUbuntuContainerHook checking for mise.sh", "error", err, "whichMiseOut", whichMise)
		} else if err = exec.ExecStream(ctx, &miseBuf, &miseBuf, "mise.sh"); err != nil {
			entryPointOut := miseBuf.String()
			slog.ErrorContext(ctx, "defaultUbuntuContainerHook starting mise.sh", "error", err, "entryPointOut", entryPointOut)
			errs = append(errs, fmt.Errorf("mise.sh: %w", err))
		}
	}

	slog.InfoContext(ctx, "defaultUbuntuContainerHook completed", "hook", "default container bootstrap")
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
