package main

import (
	"context"

	"github.com/banksean/apple-container/sand"
)

type DaemonCmd struct {
	Stop bool `help:"stop the daemon"`
}

func (c *DaemonCmd) Run(cctx *Context) error {
	ctx := context.Background()
	mux := sand.NewMux(cctx.AppBaseDir)
	if c.Stop {
		mc, err := mux.NewClient(ctx)
		if err != nil {
			return err
		}
		return mc.Shutdown(ctx)
	}

	// Otherwise run the daemon
	return mux.ServeUnix(ctx)
}
