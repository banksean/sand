package applecontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"

	"github.com/banksean/apple-container/options"
	"github.com/banksean/apple-container/types"
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

// Build builds an image. TODO: Since this can take a while, make it stream the command output
// similar to how ContainerSvc.Logs works.
func (i *ImagesSvc) Build(ctx context.Context, dockerFileDir string, opts *options.BuildOptions) (io.ReadCloser, io.ReadCloser, func() error, error) {
	args := options.ToArgs(opts)
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
