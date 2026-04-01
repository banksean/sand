package cli

import (
	"fmt"
)

type ExportCmd struct {
	SandboxNameFlag
	ImageName string `short:"i" placeholder:"<container-image-name>" help:"name of container image to export"`
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

	err = mc.ExportImage(ctx, c.SandboxName, c.ImageName)

	return err
}
