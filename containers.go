package applecontainer

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"syscall"

	"github.com/banksean/apple-container/options"
)

type containers struct{}

var Containers containers

func (c *containers) List(ctx context.Context) ([]Container, error) {
	var containers []Container
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

func (c *containers) Inspect(ctx context.Context, id ...string) ([]Container, error) {
	cmd := exec.CommandContext(ctx, "container", append([]string{"inspect"}, id...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	rawJSON, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	ret := []Container{}
	if err := json.Unmarshal(rawJSON, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

func (c *containers) Logs(ctx context.Context, opts options.ContainerLogs, id string) (io.ReadCloser, func() error, error) {
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

func (c *containers) Create(ctx context.Context, opts options.CreateContainer, imageName string, initArgs []string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"create"}, append(args, imageName)...)
	cmd := exec.Command("container", append(args, initArgs...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// TODO: make id variadic
func (c *containers) Start(ctx context.Context, opts options.StartContainer, id string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"start"}, append(args, id)...)
	cmd := exec.Command("container", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// TODO: make id variadic
func (c *containers) Stop(ctx context.Context, opts options.StopContainer, id string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"stop"}, append(args, id)...)
	cmd := exec.Command("container", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
