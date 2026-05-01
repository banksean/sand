package cloning

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/banksean/sand/internal/hostops"
)

const claudeJSON = `{"hasCompletedOnboarding":true,"projects":{"/app":{}}}`

// ClaudeWorkspacePreparation extends base preparation with the minimum
// non-secret state Claude Code needs to start from environment auth.
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

	artifacts, err := p.base.Prepare(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := p.configureClaude(artifacts.PathRegistry); err != nil {
		return nil, fmt.Errorf("failed to configure Claude: %w", err)
	}

	return artifacts, nil
}

func (p *ClaudeWorkspacePreparation) configureClaude(pathRegistry PathRegistry) error {
	clone := filepath.Join(pathRegistry.DotfilesDir(), ".claude.json")
	if err := os.WriteFile(clone, []byte(claudeJSON), 0o700); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}
	return nil
}
