package cloning

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"

	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/sandtypes"
)

// OpenCodeContainerConfiguration extends base configuration with OpenCode-specific hooks.
// It adds hooks to copy the OpenCode binary and set up SSH tunneling for Chrome DevTools.
type OpenCodeContainerConfiguration struct {
	base *BaseContainerConfiguration
}

// NewOpenCodeContainerConfiguration creates a new OpenCode container configuration instance.
func NewOpenCodeContainerConfiguration() *OpenCodeContainerConfiguration {
	return &OpenCodeContainerConfiguration{
		base: NewBaseContainerConfiguration(),
	}
}

func (c *OpenCodeContainerConfiguration) GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec {
	// OpenCode uses the same mounts as base
	return c.base.GetMounts(artifacts)
}

func (c *OpenCodeContainerConfiguration) GetStartupHooks(artifacts CloneArtifacts) []sandtypes.ContainerStartupHook {
	// Start with base hooks
	hooks := c.base.GetStartupHooks(artifacts)

	// Add OpenCode-specific hooks
	hooks = append(hooks,
		c.copyOpenCodeBinaryHook(),
		c.openSSHTunnelHook(),
	)

	return hooks
}

// copyOpenCodeBinaryHook copies the OpenCode binary to /usr/local/bin in the container.
func (c *OpenCodeContainerConfiguration) copyOpenCodeBinaryHook() sandtypes.ContainerStartupHook {
	return sandtypes.NewContainerStartupHook("Copy opencode binary to /usr/local/bin", func(ctx context.Context, box sandtypes.BoxOperations) error {
		cpOut, err := box.Exec(ctx, "cp", "-r", "/root/.opencode/bin/opencode", "/usr/local/bin/opencode")
		if err != nil {
			slog.ErrorContext(ctx, "copyOpenCodeBinaryHook copying opencode binary", "error", err, "cpOut", cpOut)
			return fmt.Errorf("copy opencode binary: %w", err)
		}
		return nil
	})
}

// openSSHTunnelHook sets up an SSH reverse tunnel for Chrome DevTools MCP.
func (c *OpenCodeContainerConfiguration) openSSHTunnelHook() sandtypes.ContainerStartupHook {
	return sandtypes.NewContainerStartupHook("open remote ssh tunnel for chrome-devtools mcp", func(ctx context.Context, box sandtypes.BoxOperations) error {
		ctr, err := box.GetContainer(ctx)
		if err != nil {
			return fmt.Errorf("failed to get container: %w", err)
		}

		hostname := getContainerHostname(ctr)

		// No context - this should run in a separate process that outlives the cloner startup hook invocations.
		cmd := exec.Command("ssh", "-R", "9222:127.0.0.1:9222", "-N", "-o", "ExitOnForwardFailure=yes", "-o", "BatchMode=yes", "root@"+hostname)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}

		slog.InfoContext(ctx, "openSSHTunnelHook opening ssh remote port forward", "cmd", strings.Join(cmd.Args, " "))
		if err := cmd.Start(); err != nil {
			slog.ErrorContext(ctx, "openSSHTunnelHook opening ssh remote port forward", "error", err)
			return fmt.Errorf("failed to start SSH tunnel: %w", err)
		}

		slog.InfoContext(ctx, "openSSHTunnelHook ssh remote port forward", "pid", cmd.Process.Pid)

		// Wait for the SSH tunnel in the background
		go func() {
			if err := cmd.Wait(); err != nil {
				slog.ErrorContext(ctx, "openSSHTunnelHook ssh remote port forward cmd", "error", err)
			}
		}()

		// TODO: save the pid of the ssh tunnel process somewhere in the db so we can kill it later during cleanup,
		// which may occur in a different process (sand cli invocation) than this one.
		return nil
	})
}

// getContainerHostname extracts the hostname from a container's network configuration.
func getContainerHostname(ctr *types.Container) string {
	for _, n := range ctr.Networks {
		return strings.TrimSuffix(n.Hostname, ".")
	}
	return ctr.Configuration.ID
}
