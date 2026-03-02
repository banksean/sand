package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/banksean/sand/cli"
	"github.com/banksean/sand/mux"
)

// TODO: make 'sand vsc' work from inside the container, so that it tells the outie to run `code --remote=` etc on the host

type VscCmd struct {
	ID string `arg:"" completion-predictor:"sandbox-name" help:"ID of the sandbox to vsc remote to"`
}

func (c *VscCmd) Run(cctx *cli.Context) error {
	ctx := cctx.Context

	slog.InfoContext(ctx, "VscCmd", "run", *c)

	server := mux.NewMuxServer(cctx.AppBaseDir, cctx.Boxer)
	mc, err := server.NewUnixSocketClient(ctx)
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

	hostname := cli.GetContainerHostname(ctr)
	vscCmd := exec.Command("code", "--remote", fmt.Sprintf("ssh-remote+root@%s", hostname), "/app", "-n")
	slog.InfoContext(ctx, "main: running vsc with", "cmd", strings.Join(vscCmd.Args, " "))
	out, err := vscCmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "VscCmd.Run cmd", "out", out, "error", err)
	}

	return nil
}
