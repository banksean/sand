package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

type RmCmd struct {
	ID  string `arg:"" optional:"" help:"ID of the sandbox to remove"`
	All bool   `short:"a" help:"remove all sandboxes"`
}

func (rm *RmCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slog.InfoContext(ctx, "RmCmd", "run", *rm)

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		return err
	}
	ids := []string{}
	if !rm.All {
		ids = append(ids, rm.ID)
	} else {
		bxs, err := cctx.sber.List(ctx)
		if err != nil {
			return err
		}
		for _, bx := range bxs {
			ids = append(ids, bx.ID)
		}
	}

	slog.InfoContext(ctx, "RmCmd.Run", "sber", cctx.sber, "cwd", cwd)

	var wg sync.WaitGroup
	errChan := make(chan error, len(ids))

	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sbx, err := cctx.sber.Get(ctx, id)
			if err != nil {
				errChan <- err
				return
			}
			if sbx == nil {
				return
			}
			if err := cctx.sber.Cleanup(ctx, sbx); err != nil {
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
