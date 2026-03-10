package cli

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/hostops"
	"github.com/banksean/sand/mux"
	"github.com/goombaio/namegenerator"
)

type ExecCmd struct {
	SandboxCreationFlags
	SandboxNameFlag
	Arg []string `arg:"" passthrough:"" help:"command args to exec in the container"`
}

func (c *ExecCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.MuxClient

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		return err
	}
	if c.CloneFromDir == "" {
		c.CloneFromDir = cwd
	}

	// Generate ID if not provided
	if c.SandboxName == "" {
		seed := time.Now().UTC().UnixNano()
		nameGenerator := namegenerator.NewNameGenerator(seed)

		c.SandboxName = nameGenerator.Generate()
	}

	if c.ImageName == "" {
		c.ImageName = DefaultImageName
	}

	// Try to get existing sandbox
	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		// Sandbox doesn't exist, create it via daemon
		slog.InfoContext(ctx, "Creating new sandbox via daemon", "id", c.SandboxName)
		sbox, err = mc.CreateSandbox(ctx, mux.CreateSandboxOpts{
			ID:           c.SandboxName,
			CloneFromDir: c.CloneFromDir,
			ImageName:    c.ImageName,
			EnvFile:      c.EnvFile,
		})
		if err != nil {
			slog.ErrorContext(ctx, "CreateSandbox", "error", err)
			return err
		}
	}

	if sbox.ImageName == "" {
		sbox.ImageName = DefaultImageName
	}

	// At this point the sandbox and container exist and are running (created by daemon)

	slog.InfoContext(ctx, "main: sbox.exec starting")

	args := []string{}
	if len(c.Arg) > 1 {
		args = c.Arg[1:]
	}
	containerSvc := hostops.NewAppleContainerOps()
	out, err := containerSvc.Exec(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				WorkDir: "/app",
				EnvFile: sbox.EnvFile,
			},
		}, sbox.ContainerID, "/bin/sh", os.Environ(),
		append([]string{c.Arg[0]}, args...)...)
	if err != nil {
		slog.ErrorContext(ctx, "sbox.exec", "error", err, "out", out)
	}

	if c.Rm {
		slog.InfoContext(ctx, "sbox.exec finished, cleaning up...")
		// Use daemon for cleanup
		if err := mc.RemoveSandbox(ctx, sbox.ID); err != nil {
			slog.ErrorContext(ctx, "RemoveSandbox", "error", err)
		}
		slog.InfoContext(ctx, "Cleanup complete. Exiting.")
	}
	fmt.Printf("%s\n", out)
	return nil
}
