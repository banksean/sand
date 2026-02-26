package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/banksean/sand"
)

type VscCmd struct {
	ID string `arg:"" help:"ID of the sandbox to vsc remote to"`
}

func (c *VscCmd) Run(cctx *Context) error {
	ctx := cctx.Context

	slog.InfoContext(ctx, "VscCmd", "run", *c)

	mux := sand.NewMuxServer(cctx.AppBaseDir, cctx.sber)
	mc, err := mux.NewClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "NewClient", "error", err)
		return err
	}

	sbox, err := mc.GetSandbox(ctx, c.ID)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "id", c.ID)
		return fmt.Errorf("could not find sandbox with ID %s: %w", c.ID, err)
	}

	ctr, err := sbox.GetContainer(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "sbox.GetContainer", "error", err)
		return err
	}

	if ctr == nil || ctr.Status != "running" {
		return fmt.Errorf("cannot connect to sandbox %q becacuse it is not currently running", c.ID)
	}

	hostname := getContainerHostname(ctr)
	vscCmd := exec.Command("code", "--remote", fmt.Sprintf("ssh-remote+root@%s", hostname), "/app", "-n")
	slog.InfoContext(ctx, "main: running vsc with", "cmd", strings.Join(vscCmd.Args, " "))
	out, err := vscCmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "VscCmd.Run cmd", "out", out, "error", err)
	}

	return nil
}
