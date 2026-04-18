package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

type ExportCmd struct {
	SandboxNameFlag
	OutputPath string `short:"o" required:"" placeholder:"<host FS path>" help:"where to write the exported FS archive to"`
}

const expectedStatus = "stopped"

func (c *ExportCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	sb, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		return fmt.Errorf("couldn't get sanbox %s: %w", c.SandboxName, err)
	}
	if sb.Container.Status != expectedStatus {
		return fmt.Errorf("sandbox container %s is in state %q, but this command only works with %q", c.SandboxName, sb.Container.Status, expectedStatus)
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	destinationPath := filepath.Join(wd, c.OutputPath)

	err = mc.ExportImage(ctx, c.SandboxName, destinationPath)

	return err
}
