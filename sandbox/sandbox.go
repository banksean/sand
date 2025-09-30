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
	DefaultImageName = "claude-code-sandbox"
)

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
		options.CreateContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
			},
			ManagementOptions: options.ManagementOptions{
				Name:   "sandbox-" + sb.id,
				Remove: true, // TODO: make this a field on either SandBox or SandBoxer so we can set it on the cli via flags.
				// Mounting the host user's entire home dir readonly is overkill here, since what we really want is to
				// mount "~/.gitconfig:/home/node/.gitconfig:ro" but when I try to do that here, it fails with:
				// 	"Error Domain=VZErrorDomain Code=2 "A directory sharing device configuration is invalid."
				// 	UserInfo={NSLocalizedFailure=Invalid virtual machine configuration.,
				//	NSLocalizedFailureReason=A directory sharing device configuration is invalid.,
				// 	NSUnderlyingError=0xa6507c060 {Error Domain=NSPOSIXErrorDomain Code=20 "Not a directory"}}"

				//Volume: fmt.Sprintf(`%s:/home/hostuserhome:ro`, home), // ro makes it readonly

				// Mounting an entirely new home dir doesn't really do what we want either.
				//Volume: fmt.Sprintf(`%s:/home/node`, filepath.Join(sb.sandboxWorkDir, "home")),
				Mount: []string{
					// TODO: figure out how to clone these settings into the container and actually have them work.
					// fmt.Sprintf(`type=bind,source=%s/.claude,target=/home/node/.claude,readonly`, os.Getenv("HOME")),
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
	output, err := ac.Containers.Start(ctx, options.StartContainer{}, sb.containerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainer", "error", err, "output", output)
		return err
	}
	slog.InfoContext(ctx, "startContainer succeeded", "output", output)
	return nil
}

// ShellExec executes a command in the container. The container must be in state "running".
func (sb *SandBox) ShellExec(ctx context.Context, shellCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	wait, err := ac.Containers.Exec(ctx,
		options.ExecContainer{
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
