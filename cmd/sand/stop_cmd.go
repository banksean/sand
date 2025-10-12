package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/banksean/sand"
)

type StopCmd struct {
	ID  string `arg:"" optional:"" help:"ID of the sandbox to stop"`
	All bool   `short:"a" help:"stop all sandboxes"`
}

func (c *StopCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := sand.NewMuxServer(cctx.AppBaseDir, cctx.sber)
	mc, err := mux.NewClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "NewClient", "error", err)
		return err
	}

	ids := []string{}
	if !c.All {
		ids = append(ids, c.ID)
	} else {
		bxs, err := mc.ListSandboxes(ctx)
		if err != nil {
			return err
		}
		for _, bx := range bxs {
			ids = append(ids, bx.ID)
		}
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(ids))

	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := mc.StopSandbox(ctx, id); err != nil {
				slog.ErrorContext(ctx, "StopSandbox", "error", err, "id", id)
				errChan <- err
				return
			}
			fmt.Printf("%s\n", id)
		}(id)
	}

	wg.Wait()
	close(errChan)

	// Return the first error if any occurred
	for err := range errChan {
		return err
	}

	return nil
}
