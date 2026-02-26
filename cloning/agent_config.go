package cloning

import (
	"fmt"
	"sync"
)

// AgentConfig combines workspace preparation and container configuration for an agent type.
// Each agent (default, claude, opencode) has its own configuration that defines how to
// prepare the workspace and configure the container.
type AgentConfig struct {
	Name          string
	Preparation   WorkspacePreparation
	Configuration ContainerConfiguration
}

// AgentRegistry manages the available agent configurations.
// It replaces the decorator pattern with a simple registry lookup.
type AgentRegistry struct {
	mu      sync.RWMutex
	configs map[string]*AgentConfig
}

// NewAgentRegistry creates a new agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		configs: make(map[string]*AgentConfig),
	}
}

// Register adds an agent configuration to the registry.
func (r *AgentRegistry) Register(config *AgentConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs[config.Name] = config
}

// Get retrieves an agent configuration by name.
// Returns the "default" agent if the requested name is not found.
func (r *AgentRegistry) Get(name string) *AgentConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if config, ok := r.configs[name]; ok {
		return config
	}

	// Fallback to default agent
	if defaultConfig, ok := r.configs["default"]; ok {
		return defaultConfig
	}

	panic(fmt.Sprintf("agent registry: no config found for %q and no default agent registered", name))
}

// Has checks if an agent configuration exists in the registry.
func (r *AgentRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.configs[name]
	return ok
}

// List returns all registered agent names.
func (r *AgentRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.configs))
	for name := range r.configs {
		names = append(names, name)
	}
	return names
}
