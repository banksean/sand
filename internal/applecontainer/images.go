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

	"github.com/banksean/sand/internal/applecontainer/types"
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
	slog.InfoContext(ctx, "ImageSvc.Inspect", "cmd", "container image inspect "+name)
	rawJSON, err := exec.CommandContext(ctx, "container", "image", "inspect", name).Output()
	if err != nil {
		return nil, err
	}

	var entries []*types.ImageManifest
	if err := json.Unmarshal([]byte(rawJSON), &entries); err != nil {
		slog.ErrorContext(ctx, "ImageSvc.Inspect json parse error", "image", name, "error", err)
		return nil, fmt.Errorf("failed to parse image JSON: %w", err)
	}
	return entries, nil
}

// Pull pulls the image with the given name and returns a wait func to release the cmd resources, or returns an error.
// Progress output from the 'container' command is written to w. Note that the 'container' command may detect
// when it is not writing to a tty and suppress its progress output; the wrapper messages in Boxer.pullImage
// will still be written regardless.
func (i *ImagesSvc) Pull(ctx context.Context, name string, w io.Writer) (func() error, error) {
	cmd := exec.CommandContext(ctx, "container", "image", "pull", name)
	slog.InfoContext(ctx, "ImagesSvc.Pull", "cmd.Dir", cmd.Dir, "cmd", strings.Join(cmd.Args, " "))

	// This Setpgid business is basically PTSD-induced superstition learned through Linux debugging nightmares.
	// It may not be necessary on MacOS at all.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	cmd.Stderr = w
	cmd.Stdout = w

	if err := cmd.Start(); err != nil {
		return cmd.Wait, err
	}

	return cmd.Wait, nil
}
