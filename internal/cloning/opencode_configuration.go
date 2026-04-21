package cloning

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/sandtypes"
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

var _ ContainerConfiguration = &OpenCodeContainerConfiguration{}

func (c *OpenCodeContainerConfiguration) GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec {
	// OpenCode uses the same mounts as base
	return c.base.GetMounts(artifacts)
}

func (c *OpenCodeContainerConfiguration) GetStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	// Start with base hooks
	hooks := c.base.GetStartHooks(artifacts)

	// Add OpenCode-specific hooks
	hooks = append(hooks,
		c.openSSHTunnelHook(artifacts.Username),
	)

	return hooks
}

func (c *OpenCodeContainerConfiguration) GetFirstStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	// Start with base hooks
	hooks := c.base.GetFirstStartHooks(artifacts)

	// Add OpenCode-specific hooks
	hooks = append(hooks,
		c.copyOpenCodeBinaryHook(artifacts.Username),
		c.openSSHTunnelHook(artifacts.Username),
	)

	return hooks
}

// copyOpenCodeBinaryHook copies the OpenCode binary to /usr/local/bin in the container.
func (c *OpenCodeContainerConfiguration) copyOpenCodeBinaryHook(username string) sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("Copy opencode binary to /usr/local/bin", func(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer) error {
		// The Dockerfile doesn't know about username, only root. So that's where it ends up installing the opencode binary.
		mkdirOut, err := exec.Exec(ctx, "mkdir", "-p", "/root/.opencode/bin")
		if err != nil {
			slog.ErrorContext(ctx, "copyOpenCodeBinaryHook mkdir for opencode binary", "error", err, "mkdirOut", mkdirOut)
			return fmt.Errorf("mkdir for opencode binary: %w", err)
		}
		cpOut, err := exec.Exec(ctx, "cp", "-r", "/root/.opencode/bin/opencode", "/usr/local/bin/opencode")
		if err != nil {
			slog.ErrorContext(ctx, "copyOpenCodeBinaryHook copying opencode binary", "error", err, "cpOut", cpOut)
			return fmt.Errorf("copy opencode binary: %w", err)
		}
		return nil
	})
}

// openSSHTunnelHook sets up an SSH reverse tunnel for Chrome DevTools MCP.
func (c *OpenCodeContainerConfiguration) openSSHTunnelHook(username string) sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("open remote ssh tunnel for chrome-devtools mcp", func(ctx context.Context, ctr *types.Container, execFn sandtypes.HookStreamer) error {
		hostname := getContainerHostname(ctr)

		// No context - this should run in a separate process that outlives the cloner startup hook invocations.
		cmd := exec.Command("ssh", "-R", "9222:127.0.0.1:9222", "-N", "-o", "ExitOnForwardFailure=yes", "-o", "BatchMode=yes", username+"@"+hostname)
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
