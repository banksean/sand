package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/banksean/apple-container/sand"
)

type ShellCmd struct {
	Shell   string `short:"s" default:"/bin/zsh" placeholder:"<shell-command>" help:"shell command to exec in the container"`
	EnvFile string `short:"e" placholder:"<file-path>" help:"path to env file to use when creating a new shell"`
	ID      string `arg:"" optional:"" help:"ID of the sandbox to create, or re-attach to"`
}

func (c *ShellCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sbox *sand.Sandbox

	sbox, err := cctx.sber.Get(ctx, c.ID)
	if err != nil {
		return err
	}
	if sbox == nil {
		slog.ErrorContext(ctx, "sber.NewSandbox not found", "id", c.ID)
		return fmt.Errorf("could not find sandbox with ID %s", c.ID)
	}

	ctr, err := sbox.GetContainer(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "sbox.GetContainer", "error", err)
		return err
	}

	if ctr == nil { // The container doesn't exist.
		slog.InfoContext(ctx, "main: sbox.createContainer")
		if err := sbox.CreateContainer(ctx); err != nil {
			slog.ErrorContext(ctx, "sbox.createContainer", "error", err)
			return err
		}
		if err := cctx.sber.UpdateContainerID(ctx, sbox, sbox.ContainerID); err != nil {
			slog.ErrorContext(ctx, "sber.UpdateContainerID", "error", err)
			return err
		}
		// Get the container *again* and this time it should not be nil
		ctr, err = sbox.GetContainer(ctx)
		if err != nil || ctr == nil {
			slog.ErrorContext(ctx, "sbox.GetContainer", "error", err, "ctr", ctr)
			return err
		}
	}

	slog.InfoContext(ctx, "ShellCmd.Run", "ctr", ctr)

	if ctr.Status != "running" {
		slog.InfoContext(ctx, "main: sbox.startContainer")
		if err := sbox.StartContainer(ctx); err != nil {
			slog.ErrorContext(ctx, "sbox.startContainer", "error", err)
			return err
		}
		// Get the container again to get the full struct details filled out now that it's running.
		ctr, err = sbox.GetContainer(ctx)
		if err != nil || ctr == nil {
			slog.ErrorContext(ctx, "sbox.GetContainer", "error", err, "ctr", ctr)
			return err
		}
	}

	hostname := getContainerHostname(ctr)
	env := map[string]string{
		"HOSTNAME": hostname,
	}
	fmt.Printf("container hostname: %s\n", hostname)

	slog.InfoContext(ctx, "main: sbox.shell starting")

	if err := sbox.Shell(ctx, env, c.Shell, os.Stdin, os.Stdout, os.Stderr); err != nil {
		slog.ErrorContext(ctx, "sbox.shell", "error", err)
	}

	return nil
}
