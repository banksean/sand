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

	type expungeTarget struct {
		id  string
		row lsRow
	}

	var deletedByID map[string]lsRow
	var deletedTargets []expungeTarget
	if len(c.SandboxIDs) == 0 || !c.Force {
		boxes, err := mc.ListDeletedSandboxes(ctx)
		if err != nil {
			return err
		}
		userHomeDir, _ := os.UserHomeDir()
		deletedByID = make(map[string]lsRow, len(boxes))
		for _, box := range boxes {
			row := rowFromSandbox(box, userHomeDir, nil)
			deletedByID[box.ID] = row
			deletedTargets = append(deletedTargets, expungeTarget{id: box.ID, row: row})
		}
	}

	targets := make([]expungeTarget, 0, len(c.SandboxIDs))
	if len(c.SandboxIDs) == 0 {
		targets = deletedTargets
	} else {
		for _, id := range c.SandboxIDs {
			target := expungeTarget{id: id}
			if row, ok := deletedByID[id]; ok {
				target.row = row
			} else {
				target.row = lsRow{ID: id}
			}
			targets = append(targets, target)
		}
	}

	if !c.Force {
		reader := bufio.NewReader(expungeCmdStdin)
		confirmed := make([]expungeTarget, 0, len(targets))
		for _, target := range targets {
			ok, err := confirmSandboxExpunge(target.row, reader, expungeCmdStdout)
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
		go func(id string) {
			defer wg.Done()
			if err := mc.ExpungeSandbox(ctx, id); err != nil {
				errChan <- err
				return
			}
			fmt.Fprintf(expungeCmdStdout, "%s\n", id)
		}(target.id)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		return err
	}

	return nil
}

func confirmSandboxExpunge(row lsRow, reader *bufio.Reader, stdout io.Writer) (bool, error) {
	fmt.Fprintf(stdout, "expunge %s [y/N]? ", formatExpungeSummary(row))

	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("couldn't read from stdin: %w", err)
	}

	return isYes(text), nil
}

func formatExpungeSummary(row lsRow) string {
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
		row.Name,
		shortSandboxID(row.ID),
		row.FromDir,
		row.FromGit,
		row.ImageName,
	)
}
