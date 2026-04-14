package cloning

import "github.com/banksean/sand/internal/sandtypes"

// ClaudeContainerConfiguration extends base configuration for Claude agent.
// Currently uses the same configuration as the base, but kept separate
// to allow for Claude-specific container configuration in the future.
type ClaudeContainerConfiguration struct {
	base *BaseContainerConfiguration
}

// NewClaudeContainerConfiguration creates a new Claude container configuration instance.
func NewClaudeContainerConfiguration() *ClaudeContainerConfiguration {
	return &ClaudeContainerConfiguration{
		base: NewBaseContainerConfiguration(),
	}
}

var _ ContainerConfiguration = &ClaudeContainerConfiguration{}

func (c *ClaudeContainerConfiguration) GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec {
	// Claude uses the same mounts as base
	return c.base.GetMounts(artifacts)
}

func (c *ClaudeContainerConfiguration) GetFirstStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	// Claude uses the same hooks as base
	return c.base.GetFirstStartHooks(artifacts)
}

func (c *ClaudeContainerConfiguration) GetStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	return nil
}
