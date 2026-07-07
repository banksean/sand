package cli

import (
	"fmt"
	"os"
)

type RenameCmd struct {
	OldName string `arg:"" required:"" help:"current sandbox name" predictor:"sandbox-name"`
	NewName string `arg:"" required:"" help:"new sandbox name"`
}

func (c *RenameCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	_, err := mc.RenameSandbox(ctx, c.OldName, c.NewName, os.Stdout)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", c.NewName)
	return nil
}
