package cloning

import (
	"path/filepath"
	"sync"

	"github.com/banksean/sand/sandtypes"
)

var (
	globalRegistry     *AgentRegistry
	globalRegistryOnce sync.Once
)

// InitializeGlobalRegistry creates and populates the global agent registry.
// This should be called once at application startup.
func InitializeGlobalRegistry(appRoot string, messenger sandtypes.UserMessenger, gitOps sandtypes.GitOps, fileOps sandtypes.FileOps) *AgentRegistry {
	globalRegistryOnce.Do(func() {
		globalRegistry = NewAgentRegistry()

		cloneRoot := filepath.Join(appRoot, "clones")

		// Register default agent
		globalRegistry.Register(&AgentConfig{
			Name:          "default",
			Preparation:   NewBaseWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
			Configuration: NewBaseContainerConfiguration(),
		})

		// Register claude agent
		globalRegistry.Register(&AgentConfig{
			Name:          "claude",
			Preparation:   NewClaudeWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
			Configuration: NewClaudeContainerConfiguration(),
		})

		// Register opencode agent
		globalRegistry.Register(&AgentConfig{
			Name:          "opencode",
			Preparation:   NewOpenCodeWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
			Configuration: NewOpenCodeContainerConfiguration(),
		})
	})

	return globalRegistry
}

// GetGlobalRegistry returns the global agent registry.
// InitializeGlobalRegistry must be called first, or this will panic.
func GetGlobalRegistry() *AgentRegistry {
	if globalRegistry == nil {
		panic("global agent registry not initialized - call InitializeGlobalRegistry first")
	}
	return globalRegistry
}
