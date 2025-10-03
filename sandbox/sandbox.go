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
	"github.com/banksean/apple-container/types"
)

const (
	DefaultImageName = "sandbox"
)

// Sandbox connects a local container instance to a dedicated, persistent local storage volume.
// Dedicated local storage volumes are visible to the host OS, regardless of the current state of the container.
// We can "revive" a Sandbox by starting a new container that mounts a previously-used local storage volume
type Sandbox struct {
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
}

func (sb *Sandbox) GetContainer(ctx context.Context) (*types.Container, error) {
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
func (sb *Sandbox) CreateContainer(ctx context.Context) error {
	containerID, err := ac.Containers.Create(ctx,
		&options.CreateContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
			},
			ManagementOptions: options.ManagementOptions{
				Name:      sb.ID,
				SSH:       true,
				DNSDomain: sb.DNSDomain,
				Remove:    false,
				Mount: []string{
					// TODO: mount other image-independent config files etc into the default user's home directory after the container starts up.
					fmt.Sprintf(`type=bind,source=%s,target=/root/.claude`, filepath.Join(sb.SandboxWorkDir, ".claude")),
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
func (sb *Sandbox) StartContainer(ctx context.Context) error {
	output, err := ac.Containers.Start(ctx, nil, sb.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainer", "error", err, "output", output)
		return err
	}
	slog.InfoContext(ctx, "startContainer succeeded", "output", output)
	return nil
}

// ShellExec executes a command in the container. The container must be in state "running".
func (sb *Sandbox) ShellExec(ctx context.Context, shellCmd string, env map[string]string, stdin io.Reader, stdout, stderr io.Writer) error {
	wait, err := ac.Containers.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
				Env:         env,
			},
		}, sb.ContainerID, shellCmd, os.Environ(), stdin, stdout, stderr)
	if err != nil {
		slog.ErrorContext(ctx, "shell: ac.Containers.Exec", "error", err)
		return err
	}

	return wait()
}
