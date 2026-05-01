package cloning

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/banksean/sand/internal/hostops"
)

const opencodeJSON = `
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "chrome-devtools": {
      "type": "local",
      "command": [
        "npx",
        "-y",
        "chrome-devtools-mcp@latest",
        "--browserUrl",
        "http://127.0.0.1:9222"
      ],
      "enabled": true,
      "environment": {
      }
    }
  }
}`

// OpenCodeWorkspacePreparation extends base preparation with OpenCode-specific files.
// It writes non-secret OpenCode configuration. Authentication is provided at
// launch time through agent capabilities, not by copying host auth files.
type OpenCodeWorkspacePreparation struct {
	base      *BaseWorkspacePreparation
	messenger hostops.UserMessenger
}

// NewOpenCodeWorkspacePreparation creates a new OpenCode workspace preparation instance.
func NewOpenCodeWorkspacePreparation(cloneRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) *OpenCodeWorkspacePreparation {
	return &OpenCodeWorkspacePreparation{
		base:      NewBaseWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
		messenger: messenger,
	}
}

func (p *OpenCodeWorkspacePreparation) Prepare(ctx context.Context, req CloneRequest) (*CloneArtifacts, error) {
	slog.InfoContext(ctx, "OpenCodeWorkspacePreparation.Prepare", "req", req)

	// First do base preparation
	artifacts, err := p.base.Prepare(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := p.configureOpenCode(ctx, artifacts.PathRegistry); err != nil {
		return nil, fmt.Errorf("failed to configure OpenCode: %w", err)
	}

	return artifacts, nil
}

func (p *OpenCodeWorkspacePreparation) configureOpenCode(ctx context.Context, pathRegistry PathRegistry) error {
	// TODO: read existing opencode.json files and merge them with the settings we want to override here.
	cloneOpenCodeConfig := filepath.Join(pathRegistry.DotfilesDir(), ".config", "opencode", "opencode.json")

	cloneOpenCodeDir := filepath.Dir(cloneOpenCodeConfig)
	if err := os.MkdirAll(cloneOpenCodeDir, 0o750); err != nil {
		return fmt.Errorf("failed to create OpenCode config directory: %w", err)
	}

	if err := os.WriteFile(cloneOpenCodeConfig, []byte(opencodeJSON), 0o700); err != nil {
		slog.ErrorContext(ctx, "configureOpenCode", "error", err)
		return fmt.Errorf("failed to write OpenCode config: %w", err)
	}

	return nil
}
