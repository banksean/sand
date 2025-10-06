package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"
)

type LsCmd struct{}

func (c *LsCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		return err
	}
	slog.InfoContext(ctx, "LsCmd.Run", "sber", cctx.sber, "cwd", cwd)
	list, err := cctx.sber.List(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "sber.List", "error", err)
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SANDBOX ID\tSTATUS\tCONTAINER ID\tORIGIN DIR\tSANDBOX DIR\tIMAGE NAME\t")
	for _, sbox := range list {
		ctr, err := sbox.GetContainer(ctx)
		if err != nil {
			return err
		}
		status := "dormant"
		if ctr != nil {
			status = ctr.Status
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t\n", sbox.ID, status, sbox.ContainerID, sbox.HostOriginDir, sbox.SandboxWorkDir, sbox.ImageName)
	}
	w.Flush()
	return nil
}
