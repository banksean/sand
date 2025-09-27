package applecontainer

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"syscall"

	"github.com/banksean/apple-container/options"
)

type system struct{}

// System is a service interface to interact with the apple container system.
var System system

// Status returns the output of `container system status`, or an error.
func (s *system) Status(ctx context.Context, opts options.SystemStatus) (string, error) {
	args := options.ToArgs(opts)
	cmd := exec.Command("container", append([]string{"system", "status"}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Start starts the container system. It returns the output of the command, or an error.
func (s *system) Start(ctx context.Context, opts options.SystemStart) (string, error) {
	args := options.ToArgs(opts)
	cmd := exec.Command("container", append([]string{"system", "start"}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Stop stops the container system. It returns the output of the command, or an error.
func (s *system) Stop(ctx context.Context, opts options.SystemStop) (string, error) {
	args := options.ToArgs(opts)
	cmd := exec.Command("container", append([]string{"system", "stop"}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Logs returns an io.ReadCloser for streaming log output and a wait func that blocks on the command's completion, or an error.
func (s *system) Logs(ctx context.Context, opts options.SystemLogs) (io.ReadCloser, func() error, error) {
	args := options.ToArgs(opts)
	cmd := exec.CommandContext(ctx, "container", append([]string{"system", "logs"}, args...)...)
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
