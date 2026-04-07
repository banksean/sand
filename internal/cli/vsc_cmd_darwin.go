//go:build darwin

package cli

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/banksean/sand/internal/applecontainer/types"
)

type VscCmd struct {
	SandboxNameFlag
}

func (c *VscCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	slog.InfoContext(ctx, "VscCmd", "run", *c)

	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "id", c.SandboxName)
		return fmt.Errorf("could not find sandbox with ID %s: %w", c.SandboxName, err)
	}

	ctr := sbox.Container

	if ctr == nil || ctr.Status != "running" {
		return fmt.Errorf("cannot connect to sandbox %q becacuse it is not currently running", c.SandboxName)
	}

	hostname := types.GetContainerHostname(ctr)
	vscCmd := exec.Command("code", "--remote", fmt.Sprintf("ssh-remote+%s", hostname), "/app", "-n")
	slog.InfoContext(ctx, "main: running vsc with", "cmd", strings.Join(vscCmd.Args, " "))
	out, err := vscCmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "VscCmd.Run cmd", "out", out, "error", err)
	}

	return nil
}
