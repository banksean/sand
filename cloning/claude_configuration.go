package cloning

import "github.com/banksean/sand/sandtypes"

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

func (c *ClaudeContainerConfiguration) GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec {
	// Claude uses the same mounts as base
	return c.base.GetMounts(artifacts)
}

func (c *ClaudeContainerConfiguration) GetStartupHooks(artifacts CloneArtifacts) []sandtypes.ContainerStartupHook {
	// Claude uses the same hooks as base
	return c.base.GetStartupHooks(artifacts)
}
