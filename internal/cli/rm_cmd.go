package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	rmCmdStdin  io.Reader = os.Stdin
	rmCmdStdout io.Writer = os.Stdout
)

type RmCmd struct {
	MultiSandboxNameFlags
	Force bool `short:"f" help:"move sandbox to trash without confirmation"`
}

func (c *RmCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	slog.InfoContext(ctx, "RmCmd", "run", *c)

	type rmTarget struct {
		name string
		id   string
	}
	targets := []rmTarget{}
	if !c.All {
		if len(c.SandboxNames) == 0 {
			return fmt.Errorf("sandbox name required unless --all is set")
		}
		for _, name := range c.SandboxNames {
			targets = append(targets, rmTarget{name: name})
		}
	} else {
		bxs, err := mc.ListSandboxes(ctx)
		if err != nil {
			return err
		}
		for _, bx := range bxs {
			targets = append(targets, rmTarget{name: bx.Name, id: bx.ID})
		}
	}

	if !c.Force {
		reader := bufio.NewReader(rmCmdStdin)
		confirmed := make([]rmTarget, 0, len(targets))
		for _, target := range targets {
			ok, err := confirmSandboxRemoval(target.name, reader, rmCmdStdout)
			if err != nil {
				return err
			}
			if ok {
				confirmed = append(confirmed, target)
			}
		}
		targets = confirmed
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(targets))

	for _, target := range targets {
		wg.Add(1)
		go func(target rmTarget) {
			defer wg.Done()
			if target.id == "" {
				sbox, err := mc.GetSandbox(ctx, target.name)
				if err != nil {
					errChan <- err
					return
				}
				if sbox != nil {
					target.id = sbox.ID
				}
			}
			if err := mc.RemoveSandbox(ctx, target.name); err != nil {
				errChan <- err
				return
			}
			if target.id != "" {
				fmt.Printf("%s\t%s\n", target.name, target.id)
			} else {
				fmt.Printf("%s\n", target.name)
			}
		}(target)
	}

	wg.Wait()
	close(errChan)

	// Return the first error if any occurred
	for err := range errChan {
		return err
	}

	return nil
}

func confirmSandboxRemoval(id string, reader *bufio.Reader, stdout io.Writer) (bool, error) {
	fmt.Fprintf(stdout, "remove %s [y/N]? ", id)

	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("couldn't read from stdin: %w", err)
	}

	switch strings.TrimSpace(strings.ToLower(text)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
