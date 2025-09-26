package applecontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

type images struct{}

var Images images

func (i *images) List(ctx context.Context) ([]ImageEntry, error) {
	var images []ImageEntry

	output, err := exec.Command("container", "image", "list", "--format", "json").Output()
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(output, &images); err != nil {
		return nil, err
	}

	return images, nil
}

func (i *images) Inspect(ctx context.Context, name string) ([]*ImageManifest, error) {
	rawJSON, err := exec.Command("container", "image", "inspect", name).Output()
	if err != nil {
		return nil, err
	}

	var entries []*ImageManifest
	if err := json.Unmarshal([]byte(rawJSON), &entries); err != nil {
		return nil, fmt.Errorf("failed to parse image JSON: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no image entries found in inspect output")
	}
	return entries, nil
}
