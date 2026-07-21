package containerruntime

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/hookscript"
	"github.com/banksean/sand/internal/sandtypes"
)

// DefinitionContainerConfiguration combines the shared base container setup
// with hooks declared by an agent definition.
type DefinitionContainerConfiguration struct {
	base       *BaseContainerConfiguration
	agentName  string
	install    *agentdefs.InstallSpec
	startHooks []string
}

func NewDefinitionContainerConfiguration(definition agentdefs.Definition) *DefinitionContainerConfiguration {
	return &DefinitionContainerConfiguration{
		base:       NewBaseContainerConfiguration(),
		agentName:  definition.Name,
		install:    definition.Install,
		startHooks: definition.StartHooks,
	}
}

var _ ContainerConfiguration = &DefinitionContainerConfiguration{}

func (c *DefinitionContainerConfiguration) GetMounts(artifacts Artifacts) []sandtypes.MountSpec {
	return c.base.GetMounts(artifacts)
}

func (c *DefinitionContainerConfiguration) GetFirstStartHooks(artifacts Artifacts) []sandtypes.ContainerHook {
	hooks := c.base.GetFirstStartHooks(artifacts)
	if c.install != nil {
		hooks = append(hooks, c.installAgentHook())
	}
	hooks = append(hooks, c.namedHooks(artifacts)...)
	return hooks
}

func (c *DefinitionContainerConfiguration) GetStartHooks(artifacts Artifacts) []sandtypes.ContainerHook {
	return append(c.base.GetStartHooks(artifacts), c.namedHooks(artifacts)...)
}

func (c *DefinitionContainerConfiguration) installAgentHook() sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("install "+c.agentName+" agent", func(ctx context.Context, ctr *sandtypes.Container, exec sandtypes.HookStreamer) error {
		script, err := agentInstallHookScript(c.agentName, *c.install)
		if err != nil {
			return err
		}
		slog.InfoContext(ctx, "agentInstallHookScript", "script", script)
		var buf bytes.Buffer
		if err := hookscript.Execute(ctx, exec, "install-"+c.agentName+".txt", script, &buf); err != nil {
			out := strings.TrimSpace(buf.String())
			if out != "" {
				return fmt.Errorf("install %s agent: %w: %s", c.agentName, err, out)
			}
			return fmt.Errorf("install %s agent: %w", c.agentName, err)
		}
		return nil
	})
}

var safeScriptToken = regexp.MustCompile(`^[A-Za-z0-9@._/+:-]+$`)

func agentInstallHookScript(agentName string, install agentdefs.InstallSpec) (string, error) {
	if err := validateInstallSpec(agentName, install); err != nil {
		return "", err
	}

	switch install.Kind {
	case agentdefs.InstallerNPM:
		return fmt.Sprintf("install-npm-agent %s %s %s\n", install.Command, install.Package, install.Version), nil
	case agentdefs.InstallerOpenCode:
		return fmt.Sprintf("install-opencode-agent %s %s\n", install.Command, install.Version), nil
	default:
		return "", fmt.Errorf("unknown installer kind %q for agent %q", install.Kind, agentName)
	}
}

func validateInstallSpec(agentName string, install agentdefs.InstallSpec) error {
	for label, value := range map[string]string{
		"agent":   agentName,
		"kind":    install.Kind,
		"version": install.Version,
		"command": install.Command,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required for agent install spec", label)
		}
		if !safeScriptToken.MatchString(value) {
			return fmt.Errorf("%s %q contains unsafe script characters", label, value)
		}
	}
	if install.Kind == agentdefs.InstallerNPM {
		if strings.TrimSpace(install.Package) == "" {
			return fmt.Errorf("package is required for npm agent install spec")
		}
		if !safeScriptToken.MatchString(install.Package) {
			return fmt.Errorf("package %q contains unsafe script characters", install.Package)
		}
	}
	return nil
}

func (c *DefinitionContainerConfiguration) namedHooks(artifacts Artifacts) []sandtypes.ContainerHook {
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
	return sandtypes.NewContainerHook("unknown agent hook "+name, func(ctx context.Context, ctr *sandtypes.Container, exec sandtypes.HookStreamer) error {
		return fmt.Errorf("unknown agent hook %q", name)
	})
}
