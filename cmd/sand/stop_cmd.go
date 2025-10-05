package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	applecontainer "github.com/banksean/apple-container"
)

type StopCmd struct {
	ID  string `arg:"" optional:"" help:"ID of the sandbox to stop"`
	All bool   `short:"a" help:"stop all sandboxes"`
}

func (sc *StopCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ids := []string{}
	if !sc.All {
		sbox, err := cctx.sber.Get(ctx, sc.ID)
		if err != nil {
			return err
		}

		ids = append(ids, sbox.ContainerID)
	} else {
		bxs, err := cctx.sber.List(ctx)
		if err != nil {
			return err
		}
		for _, bx := range bxs {
			ids = append(ids, bx.ContainerID)
		}
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(ids))

	for _, containerID := range ids {
		wg.Add(1)
		go func(containerID string) {
			defer wg.Done()
			out, err := applecontainer.Containers.Stop(ctx, nil, containerID)
			if err != nil {
				slog.ErrorContext(ctx, "StopCmd Containers.Stop", "error", err, "out", out)
				errChan <- err
				return
			}
			fmt.Printf("%s\t%s\n", containerID, out)
		}(containerID)
	}

	wg.Wait()
	close(errChan)

	// Return the first error if any occurred
	for err := range errChan {
		return err
	}

	return nil
}
