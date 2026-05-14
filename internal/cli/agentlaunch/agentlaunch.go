package agentlaunch

import (
	"fmt"

	"github.com/banksean/sand/internal/agentdefs"
)

func BuildInteractiveExec(agent, shell, sandboxID, hostname string, tmux, atch bool) (string, []string, error) {
	if tmux && atch {
		return "", nil, fmt.Errorf("--tmux and --atch cannot be used together")
	}

	if tmux {
		if agent == "" {
			return "/usr/bin/tmux", []string{"new-session", "-A", "-s", sandboxID}, nil
		}

		definition, ok := agentdefs.Lookup(agent)
		if !ok || definition.InteractiveCommand == "" {
			return "", nil, fmt.Errorf("interactive mode not supported for agent %q", agent)
		}

		return "/usr/bin/tmux", []string{
			"new-session",
			"-A",
			"-s",
			agent + "-" + sandboxID,
			definition.InteractiveCommand,
		}, nil
	}

	if atch {
		if agent == "" {
			return "/usr/local/bin/atch", []string{sandboxID, shell}, nil
		}

		definition, ok := agentdefs.Lookup(agent)
		if !ok || definition.InteractiveCommand == "" {
			return "", nil, fmt.Errorf("interactive mode not supported for agent %q", agent)
		}

		return "/usr/local/bin/atch", []string{
			sandboxID,
			shell,
			"-c",
			definition.InteractiveCommand,
		}, nil
	}

	if agent == "" {
		return shell, nil, nil
	}

	definition, ok := agentdefs.Lookup(agent)
	if !ok || definition.InteractiveCommand == "" {
		return "", nil, fmt.Errorf("interactive mode not supported for agent %q", agent)
	}

	return shell, []string{"-c", definition.InteractiveCommand}, nil
}

func BuildOneShotExec(agent string) (string, error) {
	definition, ok := agentdefs.Lookup(agent)
	if !ok || definition.OneShotCommand == "" {
		return "", fmt.Errorf("one-shot mode not supported for agent %q", agent)
	}
	return definition.OneShotCommand, nil
}

func DefaultImage(agent, fallback string) string {
	definition, ok := agentdefs.Lookup(agent)
	if !ok || definition.DefaultImage == "" {
		return fallback
	}
	return definition.DefaultImage
}

func HasAgent(agent string) bool {
	definition, ok := agentdefs.Lookup(agent)
	return ok && definition.InteractiveCommand != ""
}

func SupportedAgents() []string {
	return agentdefs.LaunchableNames()
}
