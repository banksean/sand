package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/banksean/apple-container/sandbox"
	"github.com/google/uuid"
)

type ExecCmd struct {
	ImageName     string   `default:"sandbox" placeholder:"<container-image-name>" help:"name of container image to use"`
	DockerFileDir string   `placeholder:"<docker-file-dir>" help:"location of directory with docker file from which to build the image locally. Uses an embedded dockerfile if unset."`
	Rm            bool     `help:"remove the sandbox after the exec terminates"`
	CloneFromDir  string   `placeholder:"<project-dir>" help:"directory to clone into the sandbox. Defaults to current working directory, if unset."`
	ID            string   `optionl:"" placeholder:"<sandbox-id>" help:"ID of the sandbox to create, or re-attach to"`
	EnvFile       string   `placholder:"<file-path>" help:"path to env file to use when creating a new shell"`
	Arg           []string `arg:"" passthrough:"" help:"command args to exec in the container"`
}

func (sc *ExecCmd) Run(cctx *Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if sc.DockerFileDir == "" {
		slog.InfoContext(ctx, "main: unpacking embedded container build files")
		// TODO: name this dir using a content hash of defaultContainer.
		sc.DockerFileDir = "/tmp/sandbox-container-build"
		os.RemoveAll(sc.DockerFileDir)
		if err := os.MkdirAll(sc.DockerFileDir, 0755); err != nil {
			return err
		}
		if err := os.CopyFS(sc.DockerFileDir, defaultContainer); err != nil {
			return err
		}
		slog.InfoContext(ctx, "main: done unpacking embedded dockerfile")
		sc.DockerFileDir = filepath.Join(sc.DockerFileDir, "defaultcontainer")
	}

	if err := cctx.sber.EnsureDefaultImage(ctx, sc.ImageName, sc.DockerFileDir, "root"); err != nil {
		slog.ErrorContext(ctx, "sber.Init", "error", err)
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		return err
	}
	if sc.CloneFromDir == "" {
		sc.CloneFromDir = cwd
	}

	var sbox *sandbox.Sandbox

	if sc.ID != "" {
		sbox, err = cctx.sber.Get(ctx, sc.ID) // Try to connect to an existing sandbo with this ID
		if err != nil {
			return err
		}
		if sbox == nil { // Create a new sandbox with this ID
			sbox, err = cctx.sber.NewSandbox(ctx, sc.ID, sc.CloneFromDir, sc.ImageName, sc.DockerFileDir, sc.EnvFile)
			if err != nil {
				slog.ErrorContext(ctx, "sber.NewSandbox", "error", err)
				return err
			}
		}
	} else { // Create a new sandbox with a random ID
		sc.ID = uuid.NewString()
		sbox, err = cctx.sber.NewSandbox(ctx, sc.ID, sc.CloneFromDir, sc.ImageName, sc.DockerFileDir, sc.EnvFile)
		if err != nil {
			slog.ErrorContext(ctx, "sber.NewSandbox", "error", err)
			return err
		}
	}
	if sbox.ImageName == "" {
		sbox.ImageName = sandbox.DefaultImageName
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
		// Get the container *again* and this time it should not be nil
		ctr, err = sbox.GetContainer(ctx)
		if err != nil || ctr == nil {
			slog.ErrorContext(ctx, "sbox.GetContainer", "error", err, "ctr", ctr)
			return err
		}
	}

	slog.InfoContext(ctx, "ExecCmd.Run", "ctr", ctr)

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

	slog.InfoContext(ctx, "main: sbox.exec starting")

	args := []string{}
	if len(sc.Arg) > 1 {
		args = sc.Arg[1:]
	}
	out, err := sbox.Exec(ctx, sc.Arg[0], args...)
	if err != nil {
		slog.ErrorContext(ctx, "sbox.exec", "error", err)
	}

	if sc.Rm {
		slog.InfoContext(ctx, "sbox.exec finished, cleaning up...")
		if err := cctx.sber.Cleanup(ctx, sbox); err != nil {
			slog.ErrorContext(ctx, "sber.Cleanup", "error", err)
		}

		slog.InfoContext(ctx, "Cleanup complete. Exiting.")
	}
	fmt.Printf("%s\n", out)
	return nil
}
