package cli

import "fmt"

type RecoverCmd struct {
	SandboxID string `arg:"" required:"" help:"ID of the soft-deleted sandbox to recover"`
}

func (c *RecoverCmd) Run(cctx *CLIContext) error {
	sbox, err := cctx.Daemon.RecoverSandbox(cctx.Context, c.SandboxID)
	if err != nil {
		return err
	}
	fmt.Printf("%s\t%s\n", sbox.Name, sbox.ID)
	return nil
}
