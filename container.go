package applecontainer

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"syscall"
)

func ListAllContainers() ([]Container, error) {
	var containers []Container

	output, err := exec.Command("container", "list", "--all", "--format", "json").Output()
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, err
	}

	return containers, nil
}

func InspectContainer(id ...string) ([]Container, error) {
	cmd := exec.Command("container", append([]string{"inspect"}, id...)...)
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

func ContainerLogs(ctx context.Context, opts ContainerLogsOptions, id string) (io.ReadCloser, func() error, error) {
	args := ToArgs(opts)
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

func CreateContainer(opts CreateContainerOptions, imageName string, initArgs []string) (string, error) {
	args := ToArgs(opts)
	args = append([]string{"create"}, append(args, imageName)...)
	cmd := exec.Command("container", append(args, initArgs...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func StartContainer(opts StartContainerOptions, id string) (string, error) {
	args := ToArgs(opts)
	args = append([]string{"start"}, append(args, id)...)
	cmd := exec.Command("container", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func StopContainer(opts StopContainerOptions, id string) (string, error) {
	args := ToArgs(opts)
	args = append([]string{"stop"}, append(args, id)...)
	cmd := exec.Command("container", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
