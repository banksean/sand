package applecontainer

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/banksean/apple-container/types"
)

type NetworkSvc struct{}

// Network is a service interface to interact with the apple container system.
var Network NetworkSvc

// List lists networks.
func (s *NetworkSvc) List(ctx context.Context) ([]types.Network, error) {
	cmd := exec.CommandContext(ctx, "container", "network", "list", "--format", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	ret := []types.Network{}
	if err := json.Unmarshal(output, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

// Inspect inspects a network.
func (s *NetworkSvc) Inspect(ctx context.Context, name string) ([]types.Network, error) {
	cmd := exec.CommandContext(ctx, "container", "network", "inspect", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	ret := []types.Network{}
	if err := json.Unmarshal(output, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

// Create creates a network.
func (s *NetworkSvc) Create(ctx context.Context, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "container", "network", "create", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Delete deletes a network.
func (s *NetworkSvc) Delete(ctx context.Context, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "container", "network", "delete", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
