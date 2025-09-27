package applecontainer

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"syscall"

	"github.com/banksean/apple-container/options"
	"github.com/banksean/apple-container/types"
)

type ContainerSvc struct{}

// Containers is a service interface to interact with apple containers.
var Containers ContainerSvc

// List returns all containers, or an error.
func (c *ContainerSvc) List(ctx context.Context) ([]types.Container, error) {
	var containers []types.Container
	cmd := exec.CommandContext(ctx, "container", "list", "--all", "--format", "json")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, err
	}

	return containers, nil
}

// Inspect returns details about the requested container IDs, or an error.
func (c *ContainerSvc) Inspect(ctx context.Context, id ...string) ([]types.Container, error) {
	cmd := exec.CommandContext(ctx, "container", append([]string{"inspect"}, id...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	rawJSON, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	ret := []types.Container{}
	if err := json.Unmarshal(rawJSON, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

// Logs returns an io.ReadCloser for streaming log output and a wait func that blocks on the command's completion, or an error.
func (c *ContainerSvc) Logs(ctx context.Context, opts options.ContainerLogs, id string) (io.ReadCloser, func() error, error) {
	args := options.ToArgs(opts)
	args = append([]string{"logs"}, append(args, id)...)
	cmd := exec.CommandContext(ctx, "container", args...)
	// This Setpgid business is basically PTSD-induced superstition learned through Linux debugging nightmares.
	// It may not be necessary on MacOS at all.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	return out, cmd.Wait, nil
}

// Create creates a new container with the given options, name and init args. It returns the ID of the new container instance.
func (c *ContainerSvc) Create(ctx context.Context, opts options.CreateContainer, imageName string, initArgs []string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"create"}, append(args, imageName)...)
	cmd := exec.CommandContext(ctx, "container", append(args, initArgs...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Start starts a container instance with a given ID. It returns the start command output, or an error.
func (c *ContainerSvc) Start(ctx context.Context, opts options.StartContainer, id string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"start"}, append(args, id)...)
	cmd := exec.CommandContext(ctx, "container", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Stop stops a container instance with a given ID. It returns the stop command output, or an error.
func (c *ContainerSvc) Stop(ctx context.Context, opts options.StopContainer, id string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"stop"}, append(args, id)...)
	cmd := exec.CommandContext(ctx, "container", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Run runs a command in a container instance with a given ID.
func (c *ContainerSvc) Run(ctx context.Context, opts options.RunContainer, imageName, command string, env []string, stdin io.Reader, stdout, stderr io.Writer, cmdArgs ...string) (func() error, error) {
	args := options.ToArgs(opts)
	args = append(args, append([]string{imageName, command}, cmdArgs...)...)
	cmd := exec.CommandContext(ctx, "container", append([]string{"run"}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd.Wait, nil
}
