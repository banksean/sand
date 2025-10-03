package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/banksean/apple-container/sandbox"
)

type RmCmd struct {
	ID string `arg:"" optional:"" help:"ID of the sandbox to remove"`
}

func (rm *RmCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sber := sandbox.NewSandBoxer(
		cctx.CloneRoot,
	)

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		return err
	}
	slog.InfoContext(ctx, "LsCmd.Run", "sber", sber, "cwd", cwd)
	sbx, err := sber.Get(ctx, rm.ID)
	if err != nil {
		return err
	}
	if sbx == nil {
		return nil
	}
	if err := sber.Cleanup(ctx, sbx); err != nil {
		return err
	}

	return nil
}
