package agentlaunch

import (
	"fmt"
	"strings"
)

type spec struct {
	interactive func(hostname string) string
	oneshot     string
}

var specs = map[string]spec{
	"claude": {
		interactive: func(_ string) string {
			return "claude --permission-mode=bypassPermissions"
		},
		oneshot: `claude --permission-mode=bypassPermissions --print "$SAND_ONESHOT_PROMPT"`,
	},
	"codex": {
		interactive: func(_ string) string {
			return "codex --dangerously-bypass-approvals-and-sandbox"
		},
	},
	"gemini": {
		interactive: func(_ string) string {
			return "gemini --approval-mode=yolo"
		},
		oneshot: `gemini --approval-mode=yolo -p "$SAND_ONESHOT_PROMPT"`,
	},
	"opencode": {
		interactive: func(hostname string) string {
			return "opencode --port 80 --hostname " + strings.TrimSuffix(hostname, ".")
		},
		oneshot: `opencode run "$SAND_ONESHOT_PROMPT"`,
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
