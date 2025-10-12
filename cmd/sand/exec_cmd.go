package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/banksean/sand/sand"
	"github.com/google/uuid"
)

type ExecCmd struct {
	ImageName     string   `short:"i" default:"sandbox" placeholder:"<container-image-name>" help:"name of container image to use"`
	DockerFileDir string   `short:"d" placeholder:"<docker-file-dir>" help:"location of directory with docker file from which to build the image locally. Uses an embedded dockerfile if unset."`
	CloneFromDir  string   `short:"c" placeholder:"<project-dir>" help:"directory to clone into the sandbox. Defaults to current working directory, if unset."`
	EnvFile       string   `short:"e" placholder:"<file-path>" help:"path to env file to use when creating a new shell"`
	Rm            bool     `help:"remove the sandbox after the shell terminates"`
	ID            string   `arg:"" help:"ID of the sandbox to create, or re-attach to"`
	Arg           []string `arg:"" passthrough:"" help:"command args to exec in the container"`
}

func (c *ExecCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if c.DockerFileDir == "" {
		slog.InfoContext(ctx, "main: unpacking embedded container build files")
		// TODO: name this dir using a content hash of defaultContainer.
		c.DockerFileDir = "/tmp/sandbox-container-build"
		os.RemoveAll(c.DockerFileDir)
		if err := os.MkdirAll(c.DockerFileDir, 0o755); err != nil {
			return err
		}
		if err := os.CopyFS(c.DockerFileDir, defaultImageFS); err != nil {
			return err
		}
		slog.InfoContext(ctx, "main: done unpacking embedded dockerfile")
		c.DockerFileDir = filepath.Join(c.DockerFileDir, "defaultimage")
	}

	if err := cctx.sber.EnsureDefaultImage(ctx, c.ImageName, c.DockerFileDir, "root"); err != nil {
		slog.ErrorContext(ctx, "sber.Init", "error", err)
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		return err
	}
	if c.CloneFromDir == "" {
		c.CloneFromDir = cwd
	}

	// Generate ID if not provided
	if c.ID == "" {
		c.ID = uuid.NewString()
	}

	// Use MuxClient to check if sandbox exists or create it
	mux := sand.NewMuxServer(cctx.AppBaseDir, cctx.sber)
	mc, err := mux.NewClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "NewClient", "error", err)
		return err
	}

	// Try to get existing sandbox
	sbox, err := mc.GetSandbox(ctx, c.ID)
	if err != nil {
		// Sandbox doesn't exist, create it via daemon
		slog.InfoContext(ctx, "Creating new sandbox via daemon", "id", c.ID)
		sbox, err = mc.CreateSandbox(ctx, sand.CreateSandboxOpts{
			ID:            c.ID,
			CloneFromDir:  c.CloneFromDir,
			ImageName:     c.ImageName,
			DockerFileDir: c.DockerFileDir,
			EnvFile:       c.EnvFile,
		})
		if err != nil {
			slog.ErrorContext(ctx, "CreateSandbox", "error", err)
			return err
		}
	}

	if sbox.ImageName == "" {
		sbox.ImageName = sand.DefaultImageName
	}

	// At this point the sandbox and container exist and are running (created by daemon)

	slog.InfoContext(ctx, "main: sbox.exec starting")

	args := []string{}
	if len(c.Arg) > 1 {
		args = c.Arg[1:]
	}
	out, err := sbox.Exec(ctx, c.Arg[0], args...)
	if err != nil {
		slog.ErrorContext(ctx, "sbox.exec", "error", err)
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
