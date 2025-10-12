package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/banksean/sand"
)

type RmCmd struct {
	ID  string `arg:"" optional:"" help:"ID of the sandbox to remove"`
	All bool   `short:"a" help:"remove all sandboxes"`
}

func (c *RmCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slog.InfoContext(ctx, "RmCmd", "run", *c)

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
			if err := mc.RemoveSandbox(ctx, id); err != nil {
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
