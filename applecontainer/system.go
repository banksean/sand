package applecontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
)

type SystemSvc struct{}

// System is a service interface to interact with the apple container system.
var System SystemSvc

// Version returns the version string for the "container" command, or an error.
func (s *SystemSvc) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "container", "system", "--version")
	slog.InfoContext(ctx, "SystemSvc.Version", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// Status returns the output of `container system status`, or an error.
func (s *SystemSvc) Status(ctx context.Context, opts *options.SystemStatus) (string, error) {
	args := options.ToArgs(opts)
	cmd := exec.CommandContext(ctx, "container", append([]string{"system", "status"}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Start starts the container system. It returns the output of the command, or an error.
func (s *SystemSvc) Start(ctx context.Context, opts *options.SystemStart) (string, error) {
	args := options.ToArgs(opts)
	cmd := exec.CommandContext(ctx, "container", append([]string{"system", "start"}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Stop stops the container system. It returns the output of the command, or an error.
func (s *SystemSvc) Stop(ctx context.Context, opts *options.SystemStop) (string, error) {
	args := options.ToArgs(opts)
	cmd := exec.CommandContext(ctx, "container", append([]string{"system", "stop"}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// DNSList lists the container system's dns domains, or an error.
func (s *SystemSvc) DNSList(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "container", "system", "dns", "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	var ret []string
	for _, s := range strings.Split(string(output), "\n") {
		s = strings.TrimSpace(s)
		if s != "" {
			ret = append(ret, s)
		}
	}
	return ret, nil
}

// DNSCreate adds a new local dns domain to the container system.
func (s *SystemSvc) DNSCreate(ctx context.Context, domain string) error {
	cmd := exec.CommandContext(ctx, "container", "system", "dns", "create", domain)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "SystemSvc.DNSCreate", "output", output, "error", err)
		return err
	}
	return nil
}

// PropertyList returns a slice of system property values, or an error.
func (s *SystemSvc) PropertyList(ctx context.Context) ([]types.SystemProperty, error) {
	cmd := exec.CommandContext(ctx, "container", "system", "property", "list", "--format", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	var ret []types.SystemProperty
	if err := json.Unmarshal(output, &ret); err != nil {
		slog.ErrorContext(ctx, "SystemSvc.PropertyList", "output", string(output), "error", err)
		return nil, err
	}
	return ret, nil
}

// Logs returns an io.ReadCloser for streaming log output and a wait func that blocks on the command's completion, or an error.
func (s *SystemSvc) Logs(ctx context.Context, opts *options.SystemLogs) (io.ReadCloser, func() error, error) {
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
