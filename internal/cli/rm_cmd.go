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
	Force bool `short:"f" help:"remove without confirmation"`
}

func (c *RmCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	slog.InfoContext(ctx, "RmCmd", "run", *c)

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

	if !c.Force {
		reader := bufio.NewReader(rmCmdStdin)
		confirmed := make([]string, 0, len(ids))
		for _, id := range ids {
			ok, err := confirmSandboxRemoval(id, reader, rmCmdStdout)
			if err != nil {
				return err
			}
			if ok {
				confirmed = append(confirmed, id)
			}
		}
		ids = confirmed
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
