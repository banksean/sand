package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

var (
	expungeCmdStdin  io.Reader = os.Stdin
	expungeCmdStdout io.Writer = os.Stdout
)

type ExpungeCmd struct {
	SandboxIDs []string `arg:"" optional:"" help:"IDs of soft-deleted sandboxes to hard-delete"`
	Force      bool     `short:"f" help:"hard-delete without confirmation"`
}

func (c *ExpungeCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	slog.InfoContext(ctx, "ExpungeCmd", "run", *c)

	targets := c.SandboxIDs
	if len(targets) == 0 {
		boxes, err := mc.ListDeletedSandboxes(ctx)
		if err != nil {
			return err
		}
		targets = make([]string, 0, len(boxes))
		for _, box := range boxes {
			targets = append(targets, box.ID)
		}
	}

	if !c.Force {
		reader := bufio.NewReader(expungeCmdStdin)
		confirmed := make([]string, 0, len(targets))
		for _, id := range targets {
			ok, err := confirmSandboxExpunge(id, reader, expungeCmdStdout)
			if err != nil {
				return err
			}
			if ok {
				confirmed = append(confirmed, id)
			}
		}
		targets = confirmed
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(targets))

	for _, id := range targets {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := mc.ExpungeSandbox(ctx, id); err != nil {
				errChan <- err
				return
			}
			fmt.Fprintf(expungeCmdStdout, "%s\n", id)
		}(id)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		return err
	}

	return nil
}

func confirmSandboxExpunge(id string, reader *bufio.Reader, stdout io.Writer) (bool, error) {
	fmt.Fprintf(stdout, "expunge %s [y/N]? ", id)

	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("couldn't read from stdin: %w", err)
	}

	return isYes(text), nil
}
