package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/box"
)

type ShellCmd struct {
	ShellFlags
	SandboxNameFlag
}

func (c *ShellCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.MuxClient

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "id", c.SandboxName)
		return fmt.Errorf("error while trying to find sandbox with ID %s: %w", c.SandboxName, err)
	}

	if sbox == nil {
		return fmt.Errorf("could not find sandbox with ID %s", c.SandboxName)
	}

	hostname := types.GetContainerHostname(sbox.Container)
	env := map[string]string{
		"HOSTNAME": hostname,
	}
	fmt.Printf("container hostname: %s\n", hostname)

	slog.InfoContext(ctx, "main: sbox.shell starting")
	// This will only work on the *host* OS, since it makes calls to apple's container service.
	// TODO: Sort out how "new" and "shell" should work when invoked inside a container.
	containerSvc := box.NewAppleContainerOps()
	wait, err := containerSvc.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
				Env:         env,
				EnvFile:     sbox.EnvFile,
			},
		}, sbox.ContainerID, c.Shell, os.Environ(), os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		slog.ErrorContext(ctx, "shell: containerService.ExecStream", "sandbox", sbox.ID, "error", err)
		return fmt.Errorf("failed to execute shell command for sandbox %s: %w", sbox.ID, err)
	}

	return wait()
}
