package cloning

import (
	"path/filepath"
	"sync"

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
		globalRegistry = NewAgentRegistry()

		cloneRoot := filepath.Join(appRoot, "clones")

		// Register default agent
		globalRegistry.Register(&AgentConfig{
			Name:          "default",
			Selectable:    false,
			Preparation:   NewBaseWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
			Configuration: NewBaseContainerConfiguration(),
		})

		// Register claude agent
		globalRegistry.Register(&AgentConfig{
			Name:          "claude",
			Selectable:    true,
			Preparation:   NewClaudeWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
			Configuration: NewClaudeContainerConfiguration(),
			Capabilities: AgentCapabilities{
				Auth: &AuthCapabilitySpec{
					EnvAnyOf: [][]string{
						{"CLAUDE_CODE_OAUTH_TOKEN"},
						{"ANTHROPIC_API_KEY"},
						{"CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_OAUTH_SCOPES"},
					},
				},
			},
		})

		// Register gemini agent
		globalRegistry.Register(&AgentConfig{
			Name:          "gemini",
			Selectable:    true,
			Preparation:   NewBaseWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
			Configuration: NewBaseContainerConfiguration(),
			Capabilities: AgentCapabilities{
				Auth: &AuthCapabilitySpec{
					EnvAnyOf: [][]string{
						{"GEMINI_API_KEY"},
						{"GOOGLE_API_KEY"},
					},
				},
			},
		})

		// Register codex agent
		globalRegistry.Register(&AgentConfig{
			Name:          "codex",
			Selectable:    true,
			Preparation:   NewBaseWorkspacePreparation(cloneRoot, messenger, gitOps, fileOps),
			Configuration: NewBaseContainerConfiguration(),
			Capabilities: AgentCapabilities{
				Auth: &AuthCapabilitySpec{
					EnvAnyOf: [][]string{
						{"OPENAI_API_KEY"},
					},
				},
			},
		})

		// Register opencode agent
		globalRegistry.Register(&AgentConfig{
			Name:          "opencode",
			Selectable:    true,
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
