package main

import (
	"context"
	"fmt"
	"log/slog"

	applecontainer "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/sandbox"
)

type StopCmd struct {
	ID  string `arg:"" optional:"" help:"ID of the sandbox to stop"`
	All bool   `help:"stop all sandboxes"`
}

func (sc *StopCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sber := sandbox.NewSandBoxer(
		cctx.CloneRoot,
	)

	ids := []string{}
	if !sc.All {
		sbox, err := sber.Get(ctx, sc.ID)
		if err != nil {
			return err
		}

		ids = append(ids, sbox.ContainerID)
	} else {
		bxs, err := sber.List(ctx)
		if err != nil {
			return err
		}
		for _, bx := range bxs {
			ids = append(ids, bx.ContainerID)
		}
	}

	for _, containerID := range ids {
		out, err := applecontainer.Containers.Stop(ctx, nil, containerID)
		if err != nil {
			slog.ErrorContext(ctx, "StopCmd Containers.Stop", "error", err, "out", out)
			return err
		}
		fmt.Printf("%s\t%s\n", containerID, out)
	}

	return nil
}
