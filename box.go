package sand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	ac "github.com/banksean/sand/applecontainer"
	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
)

const (
	DefaultImageName = "ghcr.io/banksean/sand/default:latest"
)

// Box is a "sandbox" - it represents the connection between
// - a local filesystem clone of a local dev workspace directory
// - a local container instance (whose state is managed by a separate container service)
//
// At startup, the sand.Mux server will synchronize its internal database with the current
// observed state of the local filesystem clone root and the local container service.
type Box struct {
	// ID is an opaque identifier for the sandbox
	ID string
	// ContainerID is the ID of the container
	ContainerID string
	// HostOriginDir is the origin of the sandbox, from which we clone its contents
	HostOriginDir string
	// SandboxWorkDir is the host OS filesystem path containing the sandbox's c-o-w clone of hostOriginDir.
	SandboxWorkDir string
	// ImageName is the name of the container image
	ImageName string
	// DNSDomain is the dns domain for the sandbox's network
	DNSDomain string
	// EnvFile is the host filesystem path to the env file to use when executing commands in the container
	EnvFile string
	// Mounts defines bind mounts that should be attached when creating the container.
	Mounts []MountSpec
	// SandboxWorkDirError and SandboxContainerError are the most recently updated error states of the sandbox
	// work dir and container instance. In-memory only. Updated once either at
	// server startup or sandbox creation time, and then updated periodically thereafter.
	// Empty string implies things are ok.
	// TODO: Make sandbox operations conditional on these values, so that e.g. you don't try to start
	// a sandbox container instance if the sandbox's work dir is not available.
	SandboxWorkDirError   string
	SandboxContainerError string
	// ContainerHooks run after the container has started to perform any bootstrap logic.
	ContainerHooks []ContainerStartupHook `json:"-"`
}

func (sb *Box) GetContainer(ctx context.Context) (*types.Container, error) {
	ctrs, err := ac.Containers.Inspect(ctx, sb.ContainerID)
	if err != nil {
		return nil, err
	}
	if len(ctrs) == 0 {
		return nil, nil
	}

	return &ctrs[0], nil
}

func (sb *Box) Sync(ctx context.Context) error {
	fi, err := os.Stat(sb.SandboxWorkDir)
	if err != nil || !fi.IsDir() {
		slog.ErrorContext(ctx, "Boxer.Sync SandboxWorkDir stat", "fi", fi, "error", err)
		sb.SandboxWorkDirError = "NO CLONE DIR"
	}
	// What *should* this code do, if we get an error while trying to inspect the sandbox's container state?
	_, err = sb.GetContainer(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.Sync GetContainer", "error", err)
		sb.SandboxContainerError = fmt.Sprintf("NO CONTAINER: %q", err.Error())
	}
	return nil
}

// CreateContainer creates a new container instance. The container image must exist.
func (sb *Box) CreateContainer(ctx context.Context) error {
	mounts := sb.effectiveMounts()
	mountOpts := make([]string, 0, len(mounts))
	for _, m := range mounts {
		mountOpts = append(mountOpts, m.String())
	}

	containerID, err := ac.Containers.Create(ctx,
		&options.CreateContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				EnvFile:     sb.EnvFile,
			},
			ManagementOptions: options.ManagementOptions{
				// TODO: Try to name the container after the sandbox, and handle collisions
				// if the name is already in use (e.g. append random chars to sb.ID).
				Name:      sb.ID,
				SSH:       true,
				DNSDomain: sb.DNSDomain,
				Remove:    false,
				Mount:     mountOpts,
			},
		},
		sb.ImageName, nil)
	if err != nil {
		slog.ErrorContext(ctx, "createContainer", "error", err, "output", containerID)
		return err
	}
	sb.ContainerID = containerID
	return nil
}

// StartContainer starts a container instance. The container must exist, and it should not be in the "running" state.
func (sb *Box) StartContainer(ctx context.Context) error {
	slog.InfoContext(ctx, "Box.StartContainer", "box", *sb, "ContainerHooks", len(sb.ContainerHooks))
	output, err := ac.Containers.Start(ctx, nil, sb.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainer", "error", err, "output", output)
		return err
	}

	var hookErrs []error
	for _, hook := range sb.ContainerHooks {
		slog.InfoContext(ctx, "Box.StartContainer running hook", "hook", hook.Name())
		if err := hook.OnStart(ctx, sb); err != nil {
			slog.ErrorContext(ctx, "Box.StartContainer hook error", "hook", hook.Name(), "error", err)
			hookErrs = append(hookErrs, fmt.Errorf("%s: %w", hook.Name(), err))
		}
	}

	slog.InfoContext(ctx, "Box.StartContainer succeeded", "output", output)

	if len(hookErrs) > 0 {
		return errors.Join(hookErrs...)
	}
	return nil
}

// Shell executes a command in the container. The container must be in state "running".
func (sb *Box) Shell(ctx context.Context, env map[string]string, shellCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	slog.InfoContext(ctx, "Sandbox.Shell", "shellCmd", shellCmd)

	wait, err := ac.Containers.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
				Env:         env,
				EnvFile:     sb.EnvFile,
			},
		}, sb.ContainerID, shellCmd, os.Environ(), stdin, stdout, stderr)
	if err != nil {
		slog.ErrorContext(ctx, "shell: ac.Containers.Exec", "error", err)
		return err
	}

	return wait()
}

// Exec executes a command in the container. The container must be in state "running".
func (sb *Box) Exec(ctx context.Context, shellCmd string, args ...string) (string, error) {
	output, err := ac.Containers.Exec(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: false,
				TTY:         true,
				WorkDir:     "/app",
				EnvFile:     sb.EnvFile,
			},
		}, sb.ContainerID, shellCmd, os.Environ(), args...)
	if err != nil {
		slog.ErrorContext(ctx, "shell: ac.Containers.Exec", "error", err)
		return "", err
	}

	return output, nil
}

func (sb *Box) effectiveMounts() []MountSpec {
	if len(sb.Mounts) > 0 {
		return sb.Mounts
	}
	if sb.SandboxWorkDir == "" {
		return nil
	}
	return []MountSpec{
		{
			Source:   filepath.Join(sb.SandboxWorkDir, "hostkeys"),
			Target:   "/hostkeys",
			ReadOnly: true,
		},
		{
			Source:   filepath.Join(sb.SandboxWorkDir, "dotfiles"),
			Target:   "/dotfiles",
			ReadOnly: true,
		},
		{
			Source: filepath.Join(sb.SandboxWorkDir, "app"),
			Target: "/app",
		},
	}
}
