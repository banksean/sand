package agentlaunch

import (
	"fmt"
	"sort"
)

type spec struct {
	interactive func(hostname string) string
	oneshot     string
	image       string
}

var specs = map[string]spec{
	"claude": {
		interactive: func(_ string) string {
			return "claude --permission-mode=bypassPermissions"
		},
		oneshot: `claude --permission-mode=bypassPermissions --print "$SAND_ONESHOT_PROMPT"`,
		image:   "ghcr.io/banksean/sand/claude:latest",
	},
	"codex": {
		interactive: func(_ string) string {
			return "codex --dangerously-bypass-approvals-and-sandbox"
		},
		image: "ghcr.io/banksean/sand/codex:latest",
	},
	"gemini": {
		interactive: func(_ string) string {
			return "gemini --approval-mode=yolo"
		},
		oneshot: `gemini --approval-mode=yolo -p "$SAND_ONESHOT_PROMPT"`,
		image:   "ghcr.io/banksean/sand/gemini:latest",
	},
	"opencode": {
		interactive: func(_ string) string {
			return "opencode"
		},
		oneshot: `opencode run "$SAND_ONESHOT_PROMPT"`,
		image:   "ghcr.io/banksean/sand/opencode:latest",
	},
}

func BuildInteractiveExec(agent, shell, sandboxID, hostname string, tmux bool) (string, []string, error) {
	if tmux {
		if agent == "" {
			return "/usr/bin/tmux", []string{"new-session", "-A", "-s", sandboxID}, nil
		}

		spec, ok := specs[agent]
		if !ok {
			return "", nil, fmt.Errorf("interactive mode not supported for agent %q", agent)
		}

		return "/usr/bin/tmux", []string{
			"new-session",
			"-A",
			"-s",
			agent + "-" + sandboxID,
			spec.interactive(hostname),
		}, nil
	}

	if agent == "" {
		return shell, nil, nil
	}

	spec, ok := specs[agent]
	if !ok {
		return "", nil, fmt.Errorf("interactive mode not supported for agent %q", agent)
	}

	return shell, []string{"-c", spec.interactive(hostname)}, nil
}

func BuildOneShotExec(agent string) (string, error) {
	spec, ok := specs[agent]
	if !ok || spec.oneshot == "" {
		return "", fmt.Errorf("one-shot mode not supported for agent %q", agent)
	}
	return spec.oneshot, nil
}

func DefaultImage(agent, fallback string) string {
	spec, ok := specs[agent]
	if !ok || spec.image == "" {
		return fallback
	}
	return spec.image
}

func HasAgent(agent string) bool {
	_, ok := specs[agent]
	return ok
}

func SupportedAgents() []string {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
