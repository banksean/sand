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
			sb, err := mc.GetSandbox(ctx, name)
			if err != nil {
				slog.ErrorContext(ctx, "GetSandbox", "error", err, "name", name)
				errChan <- err
				return
			}
			if sb.Container == nil {
				fmt.Printf("%s has no container\n", name)
				return
			}
			if sb.Container.Status == "running" {
				if c.SSHAgent && !sb.Container.Configuration.SSH {
					errChan <- fmt.Errorf("%s is already running without ssh-agent forwarding; stop it first and restart with --ssh-agent", name)
					return
				}
				fmt.Printf("%s is already running\n", name)
				return
			}
			if err := mc.StartSandbox(ctx, daemon.StartSandboxOpts{
				Name:     name,
				SSHAgent: c.SSHAgent,
			}); err != nil {
				slog.ErrorContext(ctx, "StartSandbox", "error", err, "name", name)
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
