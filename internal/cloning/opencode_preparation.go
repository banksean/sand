package cloning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
// It clones OpenCode authentication, storage, logs, and configuration.
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

	// Create OpenCode directory structure
	openCodeDir := artifacts.PathRegistry.OpenCodeDir()
	if err := os.MkdirAll(openCodeDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create OpenCode directory: %w", err)
	}

	// Clone OpenCode files
	if err := p.cloneOpenCodeAuth(ctx, artifacts.PathRegistry); err != nil {
		return nil, fmt.Errorf("failed to clone OpenCode auth: %w", err)
	}

	if err := p.cloneOpenCodeDirs(ctx, artifacts.PathRegistry); err != nil {
		return nil, fmt.Errorf("failed to clone OpenCode directories: %w", err)
	}

	if err := p.configureOpenCode(ctx, artifacts.PathRegistry); err != nil {
		return nil, fmt.Errorf("failed to configure OpenCode: %w", err)
	}

	return artifacts, nil
}

func (p *OpenCodeWorkspacePreparation) cloneOpenCodeAuth(ctx context.Context, pathRegistry PathRegistry) error {
	cloneOpenCodeAuth := filepath.Join(pathRegistry.OpenCodeDir(), "auth.json")
	openCodeAuth := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "auth.json")

	if _, err := os.Stat(openCodeAuth); errors.Is(err, os.ErrNotExist) {
		// Create empty placeholder
		f, err := os.Create(cloneOpenCodeAuth)
		if err != nil {
			return fmt.Errorf("failed to create empty auth.json: %w", err)
		}
		f.Close()
		return nil
	}

	cmd := exec.CommandContext(ctx, "cp", "-Rc", openCodeAuth, cloneOpenCodeAuth)
	slog.InfoContext(ctx, "cloneOpenCodeAuth", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "cloneOpenCodeAuth", "error", err, "output", string(output))
		return fmt.Errorf("failed to copy OpenCode auth.json: %w", err)
	}

	return nil
}

func (p *OpenCodeWorkspacePreparation) cloneOpenCodeDirs(ctx context.Context, pathRegistry PathRegistry) error {
	// Clone storage directory
	cloneOpenCodeStorage := filepath.Join(pathRegistry.OpenCodeDir(), "storage")
	openCodeStorage := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "storage")

	if _, err := os.Stat(openCodeStorage); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneOpenCodeStorage)
		if err != nil {
			return fmt.Errorf("failed to create empty storage: %w", err)
		}
		f.Close()
	} else {
		cmd := exec.CommandContext(ctx, "cp", "-Rc", openCodeStorage, cloneOpenCodeStorage)
		slog.InfoContext(ctx, "cloneOpenCodeStorage", "cmd", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.ErrorContext(ctx, "cloneOpenCodeStorage", "error", err, "output", string(output))
			return fmt.Errorf("failed to copy OpenCode storage: %w", err)
		}
	}

	// Clone log directory
	cloneOpenCodeLog := filepath.Join(pathRegistry.OpenCodeDir(), "log")
	openCodeLog := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "log")

	if _, err := os.Stat(openCodeLog); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneOpenCodeLog)
		if err != nil {
			return fmt.Errorf("failed to create empty log: %w", err)
		}
		f.Close()
	} else {
		cmd := exec.CommandContext(ctx, "cp", "-Rc", openCodeLog, cloneOpenCodeLog)
		slog.InfoContext(ctx, "cloneOpenCodeLog", "cmd", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.ErrorContext(ctx, "cloneOpenCodeLog", "error", err, "output", string(output))
			return fmt.Errorf("failed to copy OpenCode log: %w", err)
		}
	}

	// Clone snapshot directory
	cloneOpenCodeSnapshot := filepath.Join(pathRegistry.OpenCodeDir(), "snapshot")
	openCodeSnapshot := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "snapshot")

	if _, err := os.Stat(openCodeSnapshot); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneOpenCodeSnapshot)
		if err != nil {
			return fmt.Errorf("failed to create empty snapshot: %w", err)
		}
		f.Close()
	} else {
		cmd := exec.CommandContext(ctx, "cp", "-Rc", openCodeSnapshot, cloneOpenCodeSnapshot)
		slog.InfoContext(ctx, "cloneOpenCodeSnapshot", "cmd", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.ErrorContext(ctx, "cloneOpenCodeSnapshot", "error", err, "output", string(output))
			return fmt.Errorf("failed to copy OpenCode snapshot: %w", err)
		}
	}

	return nil
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
