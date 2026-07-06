package applecontainer

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/banksean/sand/internal/applecontainer/options"
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
	return strings.TrimSpace(string(output)), err
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

// PropertyList returns a slice of system property values, or an error.
func (s *SystemSvc) GetConfig(ctx context.Context) (*ContainerSystemConfig, error) {
	cmd := exec.CommandContext(ctx, "container", "system", "property", "ls")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	cfg, err := ParseContainerSystemConfig(output)
	return cfg, err
}
