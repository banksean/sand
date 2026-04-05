package cloning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/hostops"
)

// ClaudeWorkspacePreparation extends base preparation with Claude-specific files.
// It clones the ~/.claude directory and filters .claude.json for the current project.
type ClaudeWorkspacePreparation struct {
	base      *BaseWorkspacePreparation
	messenger hostops.UserMessenger
}

// NewClaudeWorkspacePreparation creates a new Claude workspace preparation instance.
func NewClaudeWorkspacePreparation(cloneRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) *ClaudeWorkspacePreparation {
	return &ClaudeWorkspacePreparation{
		base:      NewBaseWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
		messenger: messenger,
	}
}

func (p *ClaudeWorkspacePreparation) Prepare(ctx context.Context, req CloneRequest) (*CloneArtifacts, error) {
	slog.InfoContext(ctx, "ClaudeWorkspacePreparation.Prepare", "req", req)

	// First do base preparation
	artifacts, err := p.base.Prepare(ctx, req)
	if err != nil {
		return nil, err
	}

	// Clone Claude directory
	if err := p.cloneClaudeDir(ctx, req.HostWorkDir, artifacts.PathRegistry); err != nil {
		return nil, fmt.Errorf("failed to clone Claude directory: %w", err)
	}

	// Clone and filter Claude JSON
	if err := p.cloneClaudeJSON(ctx, req.HostWorkDir, artifacts.PathRegistry); err != nil {
		return nil, fmt.Errorf("failed to clone Claude JSON: %w", err)
	}

	return artifacts, nil
}

func (p *ClaudeWorkspacePreparation) cloneClaudeDir(ctx context.Context, hostWorkDir string, pathRegistry PathRegistry) error {
	cloneClaude := pathRegistry.ClaudeDir()
	dotClaude := filepath.Join(os.Getenv("HOME"), ".claude")

	// If ~/.claude doesn't exist, create empty placeholder
	if _, err := os.Stat(dotClaude); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneClaude)
		if err != nil {
			return fmt.Errorf("failed to create empty .claude placeholder: %w", err)
		}
		f.Close()
		return nil
	}

	// Copy ~/.claude to sandbox
	cmd := exec.CommandContext(ctx, "cp", "-Rc", dotClaude, cloneClaude)
	slog.InfoContext(ctx, "cloneClaudeDir", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "cloneClaudeDir", "error", err, "output", string(output))
		return fmt.Errorf("failed to copy .claude directory: %w", err)
	}

	// Rename project directory to /app if it exists
	// Claude stores projects with filesystem path as the key, but in the container it's /app
	projDirName := filepath.Join(cloneClaude, "projects", strings.Replace(hostWorkDir, string(filepath.Separator), "-", -1))
	slog.InfoContext(ctx, "cloneClaudeDir: checking for project dir to rename", "projDirName", projDirName)

	if _, err := os.Stat(projDirName); err == nil {
		mvProjDirCmd := exec.CommandContext(ctx, "mv", projDirName, filepath.Join(cloneClaude, "projects", "-app"))
		slog.InfoContext(ctx, "cloneClaudeDir", "mvProjDirCmd", strings.Join(mvProjDirCmd.Args, " "))
		output, err = mvProjDirCmd.CombinedOutput()
		if err != nil {
			slog.ErrorContext(ctx, "cloneClaudeDir: failed to rename project dir", "error", err, "output", string(output))
			return fmt.Errorf("failed to rename Claude project directory: %w", err)
		}
	}

	return nil
}

func (p *ClaudeWorkspacePreparation) cloneClaudeJSON(ctx context.Context, hostWorkDir string, pathRegistry PathRegistry) error {
	claudeJSON, err := p.filterClaudeJSON(ctx, hostWorkDir)
	if err != nil {
		return err
	}

	clone := filepath.Join(pathRegistry.DotfilesDir(), ".claude.json")
	if err := os.WriteFile(clone, claudeJSON, 0o700); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	return nil
}

func (p *ClaudeWorkspacePreparation) filterClaudeJSON(ctx context.Context, hostWorkDir string) ([]byte, error) {
	slog.InfoContext(ctx, "filterClaudeJSON", "hostWorkDir", hostWorkDir)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	claudeJSONPath := filepath.Join(homeDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.InfoContext(ctx, "filterClaudeJSON missing file, using default", "path", claudeJSONPath)
			return json.Marshal(map[string]any{"projects": map[string]any{}})
		}
		return nil, fmt.Errorf("failed to read %s: %w", claudeJSONPath, err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Extract projects map
	projectsMap := map[string]interface{}{}
	if projects, ok := config["projects"]; ok {
		if cast, ok := projects.(map[string]interface{}); ok {
			projectsMap = cast
		} else {
			slog.InfoContext(ctx, "filterClaudeJSON non-map projects field, resetting", "type", fmt.Sprintf("%T", projects))
		}
	}

	// Filter to only include the current host working directory, mapped to /app
	filteredProjects := map[string]interface{}{}
	if value, ok := projectsMap[hostWorkDir]; ok {
		filteredProjects["/app"] = value
	}

	config["projects"] = filteredProjects

	slog.InfoContext(ctx, "filterClaudeJSON success")
	result, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filtered JSON: %w", err)
	}

	return result, nil
}
