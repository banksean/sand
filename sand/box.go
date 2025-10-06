package sand

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	ac "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
	"github.com/banksean/apple-container/types"
)

const (
	DefaultImageName = "sandbox"
)

// Box connects a local container instance to a dedicated, persistent local storage volume.
// Dedicated local storage volumes are visible to the host OS, regardless of the current state of the container.
// We can "revive" a Box by starting a new container that mounts a previously-used local storage volume
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
}

func (sb *Box) GetContainer(ctx context.Context) (*types.Container, error) {
	ctrs, err := ac.Containers.Inspect(ctx, sb.ID)
	if err != nil {
		return nil, err
	}
	if len(ctrs) == 0 {
		return nil, nil
	}
	return &ctrs[0], nil
}

// CreateContainer creates a new container instance. The container image must exist.
func (sb *Box) CreateContainer(ctx context.Context) error {
	containerID, err := ac.Containers.Create(ctx,
		&options.CreateContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				EnvFile:     sb.EnvFile,
			},
			ManagementOptions: options.ManagementOptions{
				Name:      sb.ID,
				SSH:       true,
				DNSDomain: sb.DNSDomain,
				Remove:    false,
				Mount: []string{
					// TODO: mount other image-independent config files etc into the default user's home directory after the container starts up.
					fmt.Sprintf(`type=bind,source=%s,target=/dotfiles,readonly`, filepath.Join(sb.SandboxWorkDir, "dotfiles")),
					fmt.Sprintf(`type=bind,source=%s,target=/app`, filepath.Join(sb.SandboxWorkDir, "app")),
				},
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
	output, err := ac.Containers.Start(ctx, nil, sb.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainer", "error", err, "output", output)
		return err
	}

	// TODO: move these to a separate "on first container start for this sandbox" lifecycle state.
	// We probably do not want to clobber doftile changes that the user may have made previously
	// in this sandbox's filesystem.

	// At startup, copy whatever is in /dotfiles into the root user's home directory.
	cpOut, err := sb.Exec(ctx, "cp", "-r", "/dotfiles/.", "/root/.")
	if err != nil {
		slog.ErrorContext(ctx, "Sandbox.StartContainer: copying dotfiles", "error", err, "cpOut", cpOut)
	}
	// TODO: run "git gc" or "git repack"? Or do that *before* cloning?

	slog.InfoContext(ctx, "startContainer succeeded", "output", output)
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
