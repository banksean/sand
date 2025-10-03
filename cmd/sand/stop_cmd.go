package main

import (
	"context"
	"log/slog"

	applecontainer "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/sandbox"
)

type StopCmd struct {
	ID string `arg:"" optional:"" help:"ID of the sandbox to stop"`
}

func (sc *StopCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sber := sandbox.NewSandBoxer(
		cctx.CloneRoot,
	)
	sbox, err := sber.Get(ctx, sc.ID)
	if err != nil {
		return err
	}

	out, err := applecontainer.Containers.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "StopCmd Containers.Stop", "error", err, "out", out)
		return err
	}

	return nil
}
