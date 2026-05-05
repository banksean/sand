package cli

import (
	"fmt"
	"log/slog"
	"sync"
)

type StopCmd struct {
	MultiSandboxNameFlags
}

func (c *StopCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	ids := []string{}
	if !c.All {
		ids = append(ids, c.SandboxNames...)
		if len(ids) == 0 {
			return fmt.Errorf("sandbox name required unless --all is set")
		}
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
