package applecontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
)

type ImagesSvc struct{}

// Images is a service interface to interact with apple container images.
var Images ImagesSvc

// List returns all images, or an error.
func (i *ImagesSvc) List(ctx context.Context) ([]types.ImageEntry, error) {
	var images []types.ImageEntry

	output, err := exec.CommandContext(ctx, "container", "image", "list", "--format", "json").Output()
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(output, &images); err != nil {
		return nil, err
	}

	return images, nil
}

// Inspect returns details about the image with the given name, or an error.
func (i *ImagesSvc) Inspect(ctx context.Context, name string) ([]*types.ImageManifest, error) {
	rawJSON, err := exec.CommandContext(ctx, "container", "image", "inspect", name).Output()
	if err != nil {
		return nil, err
	}

	var entries []*types.ImageManifest
	if err := json.Unmarshal([]byte(rawJSON), &entries); err != nil {
		return nil, fmt.Errorf("failed to parse image JSON: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no image entries found in inspect output")
	}
	return entries, nil
}

// Pull pulls the image with the given name, or returns an error.
func (i *ImagesSvc) Pull(ctx context.Context, name string) (func() error, error) {
	cmd := exec.CommandContext(ctx, "container", "image", "pull", name)
	slog.InfoContext(ctx, "ImagesSvc.Pull", "cmd.Dir", cmd.Dir, "cmd", strings.Join(cmd.Args, " "))

	// This Setpgid business is basically PTSD-induced superstition learned through Linux debugging nightmares.
	// It may not be necessary on MacOS at all.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Go ahead and write these to our stdio.  It turns out that the 'container' command sometimes
	// detects when it's not writing to a tty, and goes slient.  Which means we can't pipe its terminal
	// updates into somewhere else, so we just show them to the user as they happen.
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd.Wait, nil
}

// Build builds an image. TODO: Since this can take a while, make it stream the command output
// similar to how ContainerSvc.Logs works.
func (i *ImagesSvc) Build(ctx context.Context, dockerFileDir string, opts *options.BuildOptions) (io.ReadCloser, io.ReadCloser, func() error, error) {
	args := append(options.ToArgs(opts), dockerFileDir)
	cmd := exec.CommandContext(ctx, "container", append([]string{"build"}, args...)...)
	cmd.Dir = dockerFileDir
	slog.InfoContext(ctx, "ImagesSvc.Build", "cmd.Dir", cmd.Dir, "cmd", strings.Join(cmd.Args, " "))

	// This Setpgid business is basically PTSD-induced superstition learned through Linux debugging nightmares.
	// It may not be necessary on MacOS at all.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}

	return outPipe, errPipe, cmd.Wait, nil
}
