package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/banksean/apple-container/sand"
)

type VscCmd struct {
	ID string `arg:"" help:"ID of the sandbox to vsc remote to"`
}

func (c *VscCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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

	if ctr.Status != "running" {
		slog.InfoContext(ctx, "main: sbox.startContainer")
		if err := sbox.StartContainer(ctx); err != nil {
			slog.ErrorContext(ctx, "sbox.startContainer", "error", err)
			return err
		}
		// Get the container again to get the full struct details filled out now that it's running.
		ctr, err = sbox.GetContainer(ctx)
		if err != nil || ctr == nil {
			slog.ErrorContext(ctx, "sbox.GetContainer", "error", err, "ctr", ctr)
			return err
		}
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
