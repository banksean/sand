package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
)

type StatsCmd struct {
	MultiSandboxNameFlags
}

func (c *StatsCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon
	ids := []string{}
	if c.SandboxName != "" {
		ids = append(ids, c.SandboxName)
	} else {
		sboxes, err := mc.ListSandboxes(ctx)
		if err != nil {
			return err
		}
		for _, sb := range sboxes {
			if sb.Container != nil {
				ids = append(ids, sb.ContainerID)
			}
		}
	}
	list, err := mc.Stats(ctx, ids...)
	if err != nil {
		slog.ErrorContext(ctx, "Stats", "error", err)
		return err
	}

	if len(list) == 0 {
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	colHeadings := []string{
		"SANDBOX NAME",
		"CPU USAGE",
		"PROCS",
		"MEMORY USAGE/LIMIT",
		"BLOCK R/W",
		"NET TX/RX",
	}
	fmt.Fprintln(w, strings.Join(colHeadings, "\t"))
	for _, ctr := range list {
		row := []string{
			ctr.ID,
			fmt.Sprintf("%d", ctr.CPUUsageUsec),
			fmt.Sprintf("%d", ctr.NumProcesses),
			fmt.Sprintf("%d/%d", ctr.MemoryUsageBytes, ctr.MemoryLimitBytes),
			fmt.Sprintf("%d/%d", ctr.BlockReadBytes, ctr.BlockWriteBytes),
			fmt.Sprintf("%d/%d", ctr.NetworkTxBytes, ctr.NetworkRxBytes),
		}
		fmt.Fprintf(w, "%s\n", strings.Join(row, "\t"))
	}
	w.Flush()
	return nil
}
