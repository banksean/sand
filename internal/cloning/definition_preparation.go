package cloning

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/hostops"
)

// DefinitionWorkspacePreparation extends the base workspace preparation with
// non-secret files declared by an agent definition.
type DefinitionWorkspacePreparation struct {
	base      *BaseWorkspacePreparation
	generated []agentdefs.GeneratedFile
}

func NewDefinitionWorkspacePreparation(definition agentdefs.Definition, cloneRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) *DefinitionWorkspacePreparation {
	return &DefinitionWorkspacePreparation{
		base:      NewBaseWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
		generated: definition.GeneratedFiles,
	}
}

func (p *DefinitionWorkspacePreparation) Prepare(ctx context.Context, req CloneRequest) (*CloneArtifacts, error) {
	artifacts, err := p.base.Prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := p.writeGeneratedFiles(artifacts.PathRegistry); err != nil {
		return nil, err
	}
	return artifacts, nil
}

func (p *DefinitionWorkspacePreparation) writeGeneratedFiles(pathRegistry PathRegistry) error {
	for _, file := range p.generated {
		target, err := generatedFileTarget(pathRegistry.DotfilesDir(), file.Path)
		if err != nil {
			return err
		}
		mode := os.FileMode(file.Mode)
		if mode == 0 {
			mode = 0o600
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("create generated file directory %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, []byte(file.Content), mode); err != nil {
			return fmt.Errorf("write generated file %s: %w", file.Path, err)
		}
	}
	return nil
}

func generatedFileTarget(root, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("generated file path is required")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("generated file path %q must be relative", name)
	}
	clean := filepath.Clean(name)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("generated file path %q must stay inside dotfiles", name)
	}
	return filepath.Join(root, clean), nil
}
