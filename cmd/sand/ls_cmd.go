package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

type LsCmd struct{}

func (ls *LsCmd) Run(cctx *Context) error {
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

	for _, sbox := range list {
		ctr, err := sbox.GetContainer(ctx)
		if err != nil {
			return err
		}
		status := "dormant"
		if ctr != nil {
			status = ctr.Status
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", sbox.ID, status, sbox.HostOriginDir, sbox.ImageName)
	}
	return nil
}
