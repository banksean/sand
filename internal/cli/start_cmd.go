package cli

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/banksean/sand/internal/daemon"
)

type StartCmd struct {
	MultiSandboxNameFlags
	SSHAgentFlag
}

func (c *StartCmd) Run(cctx *CLIContext) error {
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
			sb, err := mc.GetSandbox(ctx, id)
			if err != nil {
				slog.ErrorContext(ctx, "GetSandbox", "error", err, "id", id)
				errChan <- err
				return
			}
			if sb.Container == nil {
				fmt.Printf("%s has no container\n", id)
				return
			}
			if sb.Container.Status == "running" {
				if c.SSHAgent && !sb.Container.Configuration.SSH {
					errChan <- fmt.Errorf("%s is already running without ssh-agent forwarding; stop it first and restart with --ssh-agent", id)
					return
				}
				fmt.Printf("%s is already running\n", id)
				return
			}
			if err := mc.StartSandbox(ctx, daemon.StartSandboxOpts{
				ID:       id,
				SSHAgent: c.SSHAgent,
			}); err != nil {
				slog.ErrorContext(ctx, "StartSandbox", "error", err, "id", id)
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
