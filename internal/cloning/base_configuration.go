package cloning

import (
	"bytes"
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

const (
	miseCachePath    = "/opt/tool-cache/mise"
	goModCachePath   = miseCachePath + "/go/mod"
	goBuildCachePath = miseCachePath + "/go/build"
	apkCachePath     = "/var/cache/apk"
)

type containerBootstrapFlavor struct {
	name             string
	hookName         string
	createUser       func(*containerHookRunner, string, string)
	linkPackageCache func(*containerHookRunner, sandtypes.SharedCacheMounts)
	prepareSSHD      func(*containerHookRunner)
}

type containerHookRunner struct {
	ctx      context.Context
	exec     sandtypes.HookStreamer
	hookName string
	username string
	errs     []error
}

func newContainerHookRunner(ctx context.Context, exec sandtypes.HookStreamer, hookName, username string) *containerHookRunner {
	return &containerHookRunner{
		ctx:      ctx,
		exec:     exec,
		hookName: hookName,
		username: username,
	}
}

func (r *containerHookRunner) run(step, wrap string, cmd ...string) bool {
	out, err := r.exec.Exec(r.ctx, cmd[0], cmd[1:]...)
	if err != nil {
		slog.ErrorContext(r.ctx, r.hookName+" "+step, "error", err, "out", out, "username", r.username)
		r.errs = append(r.errs, fmt.Errorf("%s: %w", wrap, err))
		return false
	}
	return true
}

func (r *containerHookRunner) probe(step string, cmd ...string) bool {
	out, err := r.exec.Exec(r.ctx, cmd[0], cmd[1:]...)
	if err != nil {
		slog.ErrorContext(r.ctx, r.hookName+" "+step, "error", err, "out", out, "username", r.username)
		return false
	}
	return true
}

func (r *containerHookRunner) runStream(step, wrap, cmd string) bool {
	var buf bytes.Buffer
	if err := r.exec.ExecStream(r.ctx, &buf, &buf, cmd); err != nil {
		slog.ErrorContext(r.ctx, r.hookName+" "+step, "error", err, "out", buf.String(), "username", r.username)
		r.errs = append(r.errs, fmt.Errorf("%s: %w", wrap, err))
		return false
	}
	return true
}

func (r *containerHookRunner) err() error {
	if len(r.errs) == 0 {
		return nil
	}
	return errors.Join(r.errs...)
}

var alpineBootstrapFlavor = containerBootstrapFlavor{
	name:             "alpine",
	hookName:         "defaultAlpineContainerHook",
	createUser:       alpineCreateUser,
	linkPackageCache: alpineLinkPackageCache,
	prepareSSHD:      alpinePrepareSSHD,
}

var ubuntuBootstrapFlavor = containerBootstrapFlavor{
	name:             "ubuntu",
	hookName:         "defaultUbuntuContainerHook",
	createUser:       ubuntuCreateUser,
	linkPackageCache: ubuntuLinkPackageCache,
	prepareSSHD:      ubuntuPrepareSSHD,
}

func alpineCreateUser(r *containerHookRunner, username, uid string) {
	r.run("adding group for user", "addgroup", "addgroup", "-g", uid, username)
	r.run("creating user", "useradd", "adduser", "-u", uid, "-D", "-G", username, "-s", "/bin/zsh", username)
	r.run("unlocking account", "passwd -u", "passwd", "-u", username)
	r.run("adding user to wheel", "addgroup", "addgroup", username, "wheel")
}

func alpineLinkPackageCache(r *containerHookRunner, sharedCaches sandtypes.SharedCacheMounts) {
	if sharedCaches.APKCacheHostDir == "" {
		return
	}
	if r.probe("checking for /etc/apk", "stat", "/etc/apk") {
		r.run("linking apk cache", "link apk cache", "ln", "-s", "/var/cache/apk", "/etc/apk/cache")
	}
}

func alpinePrepareSSHD(r *containerHookRunner) {}

func ubuntuCreateUser(r *containerHookRunner, username, uid string) {
	r.run("adding group for user", "groupadd", "groupadd", "-g", uid, username)
	r.run("creating user", "useradd", "useradd", "-u", uid, "-g", username, "-s", "/bin/zsh", username)
	r.run("adding user to sudo", "usermod", "usermod", "-a", "-G", "sudo", username)
	r.run("making user home dir", "making user home dir", "mkdir", "-p", "/home/"+username)
}

func ubuntuLinkPackageCache(r *containerHookRunner, sharedCaches sandtypes.SharedCacheMounts) {}

func ubuntuPrepareSSHD(r *containerHookRunner) {
	r.run("creating /run/sshd", "creating /run/sshd", "mkdir", "-p", "/run/sshd")
}

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
		flavor, err := c.detectBootstrapFlavor(ctx, exec)
		if err != nil {
			return err
		}

		return c.runDefaultContainerHook(ctx, ctr, exec, flavor, username, uid, sharedCaches)
	})
}

func (c *BaseContainerConfiguration) detectBootstrapFlavor(ctx context.Context, exec sandtypes.HookStreamer) (containerBootstrapFlavor, error) {
	if _, err := exec.Exec(ctx, "apk", "--version"); err == nil {
		return alpineBootstrapFlavor, nil
	}
	return ubuntuBootstrapFlavor, nil
}

func (c *BaseContainerConfiguration) runDefaultContainerHook(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer, flavor containerBootstrapFlavor, username, uid string, sharedCaches sandtypes.SharedCacheMounts) error {
	runner := newContainerHookRunner(ctx, exec, flavor.hookName, username)

	// We create a group and a user with the same name and uid as the the host user.
	// This avoids potential permissions issues with volumes mounted from host.
	flavor.createUser(runner, username, uid)

	runner.run("copying dotfiles", "copy dotfiles", "cp", "-r", "/dotfiles/.", "/home/"+username+"/.")

	// Copy config and known_hosts from /root/.ssh to make sure github host keys are already known for the user.
	runner.run("copying /root/.ssh to ~/.ssh", "copy /root/.ssh", "cp", "-r", "/root/.ssh", "/home/"+username+"/.ssh")

	// Create the parent directories before chown so the container user owns them,
	// but delay the symlink creation until after chown so we don't traverse into
	// shared host-mounted cache dirs.
	if sharedCaches.MiseCacheHostDir != "" {
		runner.run("preparing go module cache parent", "mkdir go module cache parent", "mkdir", "-p", "/home/"+username+"/go/pkg")
		runner.run("preparing go build cache parent", "mkdir go build cache parent", "mkdir", "-p", "/home/"+username+"/.cache")
	}

	// Fix ownership
	runner.run("chown homedir", "chown", "chown", "-R", username+":"+username,
		"/home/"+username)

	flavor.linkPackageCache(runner, sharedCaches)

	// mise.sh exports GOMODCACHE/GOCACHE directly, and these symlinks keep
	// direct process execs aligned with the same mise-backed cache paths.
	if sharedCaches.MiseCacheHostDir != "" {
		runner.run("linking go module cache", "link go module cache", "ln", "-sfn", goModCachePath, "/home/"+username+"/go/pkg/mod")
		runner.run("linking go build cache", "link go build cache", "ln", "-sfn", goBuildCachePath, "/home/"+username+"/.cache/go-build")
	}

	// Copy SSH keys to /etc/ssh
	runner.run("copying host keys", "copy host keys", "cp", "-r", "/sshkeys/.", "/etc/ssh/.")

	// Set SSH key permissions
	runner.run("setting host key permissions", "chmod host keys", "chmod", "600",
		"/etc/ssh/ssh_host_key",
		"/etc/ssh/ssh_host_key.pub",
		"/etc/ssh/ssh_host_key.pub-cert",
		"/etc/ssh/user_ca.pub")

	flavor.prepareSSHD(runner)

	// Start sshd
	runner.run("starting sshd", "start sshd", "/usr/sbin/sshd", "-f", "/etc/ssh/sshd_config")

	if sharedCaches.MiseCacheHostDir != "" {
		if runner.probe("checking for mise.sh", "which", "mise.sh") {
			runner.runStream("starting mise.sh", "mise.sh", "mise.sh")
		}
	}

	slog.InfoContext(ctx, flavor.hookName+" completed", "hook", "default container bootstrap", "flavor", flavor.name)
	return runner.err()
}
