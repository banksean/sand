package sandbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	ac "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
)

const (
	DefaultImageName = "sandbox"
)

// SandBox connects a local container instance to a dedicated, persistent local storage volume.
// Dedicated local storage volumes are visible to the host OS, regardless of the current state of the container.
// We can "revive" a SandBox by starting a new container that mounts a previously-used local storage volume
type SandBox struct {
	id          string
	containerID string
	// hostWorkDir is the origin of the sandbox, from which we clone its contents
	hostWorkDir    string
	sandboxWorkDir string
	imageName      string
}

// CreateContainer creates a new container instance. The container image must exist.
func (sb *SandBox) CreateContainer(ctx context.Context) error {
	containerID, err := ac.Containers.Create(ctx,
		&options.CreateContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
			},
			ManagementOptions: options.ManagementOptions{
				Name:   "sandbox-" + sb.id,
				SSH:    true,
				Remove: true, // TODO: make this a field on either SandBox or SandBoxer so we can set it on the cli via flags.
				Mount: []string{
					// TODO: mount image-independent config files etc into the default user's home directory after the container starts up.
					fmt.Sprintf(`type=bind,source=%s,target=/root/.claude,readonly`, filepath.Join(os.Getenv("HOME"), ".claude")),
					fmt.Sprintf(`type=bind,source=%s,target=/app`, filepath.Join(sb.sandboxWorkDir, "app")),
				},
			},
		},
		sb.imageName, nil)
	if err != nil {
		slog.ErrorContext(ctx, "createContainer", "error", err, "output", containerID)
		return err
	}
	sb.containerID = containerID
	return nil
}

// StartContainer starts a container instance. The container must exist, and it should not be in the "running" state.
func (sb *SandBox) StartContainer(ctx context.Context) error {
	output, err := ac.Containers.Start(ctx, nil, sb.containerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainer", "error", err, "output", output)
		return err
	}
	slog.InfoContext(ctx, "startContainer succeeded", "output", output)
	return nil
}

// ShellExec executes a command in the container. The container must be in state "running".
func (sb *SandBox) ShellExec(ctx context.Context, shellCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	wait, err := ac.Containers.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
			},
		}, sb.containerID, shellCmd, os.Environ(), stdin, stdout, stderr)
	if err != nil {
		slog.ErrorContext(ctx, "shell: ac.Containers.Exec", "error", err)
		return err
	}

	return wait()
}
