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

	names := []string{}
	if !c.All {
		names = append(names, c.SandboxNames...)
		if len(names) == 0 {
			return fmt.Errorf("sandbox name required unless --all is set")
		}
	} else {
		bxs, err := mc.ListSandboxes(ctx)
		if err != nil {
			return err
		}
		for _, bx := range bxs {
			names = append(names, bx.Name)
		}
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(names))

	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := mc.StopSandbox(ctx, name); err != nil {
				slog.ErrorContext(ctx, "StopSandbox", "error", err, "name", name)
				errChan <- err
				return
			}
			fmt.Printf("%s\n", name)
		}(name)
	}

	wg.Wait()
	close(errChan)

	// Return the first error if any occurred
	for err := range errChan {
		return err
	}

	return nil
}
