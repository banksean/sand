package cloning

import (
	"path/filepath"
	"sync"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/hostops"
)

var (
	globalRegistry     *AgentRegistry
	globalRegistryOnce sync.Once
)

// InitializeGlobalRegistry creates and populates the global agent registry.
// This should be called once at application startup.
func InitializeGlobalRegistry(appRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) *AgentRegistry {
	globalRegistryOnce.Do(func() {
		cloneRoot := filepath.Join(appRoot, "clones")
		globalRegistry = NewAgentRegistryFromDefinitions(cloneRoot, messenger, gitOps, fileOps)
	})

	return globalRegistry
}

// NewAgentRegistryFromDefinitions builds a registry from the central built-in
// agent declaration table.
func NewAgentRegistryFromDefinitions(cloneRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) *AgentRegistry {
	registry := NewAgentRegistry()
	for _, definition := range agentdefs.All() {
		registry.Register(newAgentConfigFromDefinition(definition, cloneRoot, messenger, gitOps, fileOps))
	}
	return registry
}

func newAgentConfigFromDefinition(definition agentdefs.Definition, cloneRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) *AgentConfig {
	return &AgentConfig{
		Name:          definition.Name,
		Selectable:    definition.Selectable,
		Preparation:   workspacePreparationForDefinition(definition, cloneRoot, messenger, gitOps, fileOps),
		Configuration: containerConfigurationForDefinition(definition),
		Requirements:  requirementsForDefinition(definition),
	}
}

func workspacePreparationForDefinition(definition agentdefs.Definition, cloneRoot string, messenger hostops.UserMessenger, gitOps hostops.GitOps, fileOps hostops.FileOps) WorkspacePreparation {
	switch definition.Preparation {
	case agentdefs.PreparationBase:
		return NewBaseWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps)
	case agentdefs.PreparationClaude:
		return NewClaudeWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps)
	case agentdefs.PreparationOpenCode:
		return NewOpenCodeWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps)
	default:
		panic("unknown agent preparation kind " + string(definition.Preparation))
	}
}

func containerConfigurationForDefinition(definition agentdefs.Definition) ContainerConfiguration {
	switch definition.Container {
	case agentdefs.ContainerBase:
		return NewBaseContainerConfiguration()
	case agentdefs.ContainerClaude:
		return NewClaudeContainerConfiguration()
	case agentdefs.ContainerOpenCode:
		return NewOpenCodeContainerConfiguration()
	default:
		panic("unknown agent container kind " + string(definition.Container))
	}
}

func requirementsForDefinition(definition agentdefs.Definition) AgentRequirements {
	if len(definition.AuthEnvAnyOf) == 0 {
		return AgentRequirements{}
	}
	return AgentRequirements{
		Auth: &AuthRequirementSpec{
			EnvAnyOf: definition.AuthEnvAnyOf,
		},
	}
}

// GetGlobalRegistry returns the global agent registry.
// InitializeGlobalRegistry must be called first, or this will panic.
func GetGlobalRegistry() *AgentRegistry {
	if globalRegistry == nil {
		panic("global agent registry not initialized - call InitializeGlobalRegistry first")
	}
	return globalRegistry
}
