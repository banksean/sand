package cloning

import (
	"fmt"
	"sort"
	"sync"
)

// AgentRequirements describes the optional launch requirements an agent declares.
// For now this only models authentication requirements, but it is intended to
// grow as new daemon-side requirement checks are added.
type AgentRequirements struct {
	Auth *AuthRequirementSpec
}

// AuthRequirementSpec describes the env-var groups that satisfy agent auth.
// Any one group in EnvAnyOf is sufficient.
type AuthRequirementSpec struct {
	EnvAnyOf [][]string
}

// AgentConfig combines workspace preparation and container configuration for an agent type.
// Each agent (default, claude, opencode) has its own configuration that defines how to
// prepare the workspace and configure the container.
type AgentConfig struct {
	Name          string
	Selectable    bool
	Preparation   WorkspacePreparation
	Configuration ContainerConfiguration
	Requirements  AgentRequirements
}

// AgentRegistry manages the available agent configurations.
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

// Lookup retrieves an agent configuration by name without falling back.
func (r *AgentRegistry) Lookup(name string) (*AgentConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config, ok := r.configs[name]
	return config, ok
}

// Get retrieves an agent configuration by name.
// Returns the "default" agent if the requested name is not found.
func (r *AgentRegistry) Get(name string) *AgentConfig {
	if config, ok := r.Lookup(name); ok {
		return config
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Fallback to default agent
	if defaultConfig, ok := r.configs["default"]; ok {
		return defaultConfig
	}

	panic(fmt.Sprintf("agent registry: no config found for %q and no default agent registered", name))
}

// Has checks if an agent configuration exists in the registry.
func (r *AgentRegistry) Has(name string) bool {
	_, ok := r.Lookup(name)
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
	sort.Strings(names)
	return names
}

// SelectableNames returns the user-selectable agent names in sorted order.
func (r *AgentRegistry) SelectableNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.configs))
	for name, config := range r.configs {
		if config.Selectable {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
