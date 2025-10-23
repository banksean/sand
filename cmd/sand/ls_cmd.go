package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/banksean/sand"
)

type LsCmd struct{}

func (c *LsCmd) Run(cctx *Context) error {
	ctx := cctx.Context

	mux := sand.NewMuxServer(cctx.AppBaseDir, cctx.sber)
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

	if len(list) == 0 {
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SANDBOX ID\tSTATUS\tCONTAINER ID\tHOSTNAME\tORIGIN DIR\tIMAGE NAME\t")
	for _, sbox := range list {
		sbox.Sync(ctx)
		ctr, err := sbox.GetContainer(ctx)
		if err != nil {
			return err
		}
		status := []string{"dormant"}
		hostname := ""
		if ctr != nil {
			status[0] = ctr.Status
			hostname = getContainerHostname(ctr)
		}
		if sbox.SandboxContainerError != "" {
			status = append(status, sbox.SandboxContainerError)
		}
		if sbox.SandboxWorkDirError != "" {
			status = append(status, sbox.SandboxWorkDirError)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t\n", sbox.ID, strings.Join(status, ", "), sbox.ContainerID, hostname, sbox.HostOriginDir, sbox.ImageName)
	}
	w.Flush()
	return nil
}
