package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"time"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/hostops"
	"github.com/goombaio/namegenerator"
)

type ExecCmd struct {
	SandboxCreationFlags
	SandboxNameFlag
	Username string   `help:"name of user to exec as (defaults to $USER)"`
	Uid      string   `help:"id of user to exec as (defaults to $UID)"`
	Arg      []string `arg:"" passthrough:"" help:"command args to exec in the container"`
}

func (c *ExecCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

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
	userInfo, err := user.Current()
	if err != nil {
		return err
	}
	if c.Username == "" {
		c.Username = userInfo.Username
	}
	if c.Uid == "" {
		c.Uid = userInfo.Uid
	}
	// Try to get existing sandbox
	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if err != nil {
		// Sandbox doesn't exist, create it via daemon
		slog.InfoContext(ctx, "Creating new sandbox via daemon", "id", c.SandboxName)
		sbox, err = mc.CreateSandbox(ctx, daemon.CreateSandboxOpts{
			ID:           c.SandboxName,
			CloneFromDir: c.CloneFromDir,
			ImageName:    c.ImageName,
			EnvFile:      c.EnvFile,
			Volumes:      c.Volume,
			CPUs:         c.CPU,
			Memory:       c.Memory,
			Username:     c.Username,
			Uid:          c.Uid,
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
	hostname := types.GetContainerHostname(sbox.Container)
	env := map[string]string{
		"HOSTNAME": hostname,
	}
	out, err := containerSvc.Exec(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				WorkDir: "/app",
				Env:     env,
				EnvFile: sbox.EnvFile,
				User:    c.Username,
				UID:     c.Uid,
			},
		}, sbox.ContainerID, c.Arg[0], os.Environ(), args...)
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
