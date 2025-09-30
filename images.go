package applecontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/banksean/apple-container/options"
	"github.com/banksean/apple-container/types"
)

type ImagesSvc struct{}

// Images is a service interface to interact with apple container images.
var Images ImagesSvc

// List returns all images, or an error.
func (i *ImagesSvc) List(ctx context.Context) ([]types.ImageEntry, error) {
	var images []types.ImageEntry

	output, err := exec.Command("container", "image", "list", "--format", "json").Output()
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
	rawJSON, err := exec.Command("container", "image", "inspect", name).Output()
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

// Build builds an image.
func (i *ImagesSvc) Build(ctx context.Context, opts options.BuildOptions) (string, error) {
	args := options.ToArgs(opts)
	cmd := exec.Command("container", append([]string{"build"}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
