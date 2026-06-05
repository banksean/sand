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

// openCodeSSHTunnelHook sets up an SSH reverse tunnel for Chrome DevTools MCP.
func openCodeSSHTunnelHook(username string) sandtypes.ContainerHook {
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

		go func() {
			if err := cmd.Wait(); err != nil {
				slog.ErrorContext(ctx, "openSSHTunnelHook ssh remote port forward cmd", "error", err)
			}
		}()

		return nil
	})
}

func getContainerHostname(ctr *types.Container) string {
	for _, n := range ctr.Networks {
		return strings.TrimSuffix(n.Hostname, ".")
	}
	return ctr.Configuration.ID
}
