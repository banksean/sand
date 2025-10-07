package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"

	"github.com/banksean/apple-container/sand"
)

type LsCmd struct{}

func (c *LsCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := sand.NewMux(cctx.AppBaseDir, cctx.sber)
	mc, err := mux.NewClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "NewClient", "error", err)
		return err
	}

	list, err := mc.ListSandboxes(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "ListSandboxes", "error", err)
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
