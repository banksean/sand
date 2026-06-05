package cloning

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/sandtypes"
)

// DefinitionContainerConfiguration combines the shared base container setup
// with hooks declared by an agent definition.
type DefinitionContainerConfiguration struct {
	base             *BaseContainerConfiguration
	agentName        string
	firstStartScript string
	startHooks       []string
}

func NewDefinitionContainerConfiguration(definition agentdefs.Definition) *DefinitionContainerConfiguration {
	return &DefinitionContainerConfiguration{
		base:             NewBaseContainerConfiguration(),
		agentName:        definition.Name,
		firstStartScript: definition.FirstStartScript,
		startHooks:       definition.StartHooks,
	}
}

var _ ContainerConfiguration = &DefinitionContainerConfiguration{}

func (c *DefinitionContainerConfiguration) GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec {
	return c.base.GetMounts(artifacts)
}

func (c *DefinitionContainerConfiguration) GetFirstStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	hooks := c.base.GetFirstStartHooks(artifacts)
	if strings.TrimSpace(c.firstStartScript) != "" {
		hooks = append(hooks, c.installAgentHook())
	}
	hooks = append(hooks, c.namedHooks(artifacts)...)
	return hooks
}

func (c *DefinitionContainerConfiguration) GetStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	return c.namedHooks(artifacts)
}

func (c *DefinitionContainerConfiguration) installAgentHook() sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("install "+c.agentName+" agent", func(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer) error {
		var buf bytes.Buffer
		if err := exec.ExecStream(ctx, &buf, &buf, "sh", "-c", c.firstStartScript); err != nil {
			out := strings.TrimSpace(buf.String())
			if out != "" {
				return fmt.Errorf("install %s agent: %w: %s", c.agentName, err, out)
			}
			return fmt.Errorf("install %s agent: %w", c.agentName, err)
		}
		return nil
	})
}

func (c *DefinitionContainerConfiguration) namedHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	hooks := make([]sandtypes.ContainerHook, 0, len(c.startHooks))
	for _, name := range c.startHooks {
		switch name {
		case agentdefs.HookOpenCodeTunnel:
			hooks = append(hooks, openCodeSSHTunnelHook(artifacts.Username))
		default:
			hooks = append(hooks, unknownAgentHook(name))
		}
	}
	return hooks
}

func unknownAgentHook(name string) sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("unknown agent hook "+name, func(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer) error {
		return fmt.Errorf("unknown agent hook %q", name)
	})
}
