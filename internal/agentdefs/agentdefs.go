package agentdefs

import "sort"

// PreparationKind identifies the Go workspace-preparation strategy for an agent.
type PreparationKind string

const (
	PreparationBase     PreparationKind = "base"
	PreparationClaude   PreparationKind = "claude"
	PreparationOpenCode PreparationKind = "opencode"
)

// ContainerKind identifies the Go container-configuration strategy for an agent.
type ContainerKind string

const (
	ContainerBase     ContainerKind = "base"
	ContainerClaude   ContainerKind = "claude"
	ContainerOpenCode ContainerKind = "opencode"
)

// Definition is the shared declaration for a built-in agent.
//
// Behavioral fields intentionally stay as typed strategy keys. The concrete
// workspace preparation and container hooks remain implemented in Go.
type Definition struct {
	Name               string
	Selectable         bool
	Preparation        PreparationKind
	Container          ContainerKind
	AuthEnvAnyOf       [][]string
	InteractiveCommand string
	OneShotCommand     string
	DefaultImage       string
}

var definitions = []Definition{
	{
		Name:        "default",
		Preparation: PreparationBase,
		Container:   ContainerBase,
	},
	{
		Name:        "claude",
		Selectable:  true,
		Preparation: PreparationClaude,
		Container:   ContainerClaude,
		AuthEnvAnyOf: [][]string{
			{"CLAUDE_CODE_OAUTH_TOKEN"},
			{"ANTHROPIC_API_KEY"},
			{"CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_OAUTH_SCOPES"},
		},
		InteractiveCommand: "claude --permission-mode=bypassPermissions",
		OneShotCommand:     `claude --permission-mode=bypassPermissions --print "$SAND_ONESHOT_PROMPT"`,
		DefaultImage:       "ghcr.io/banksean/sand/claude:latest",
	},
	{
		Name:        "codex",
		Selectable:  true,
		Preparation: PreparationBase,
		Container:   ContainerBase,
		AuthEnvAnyOf: [][]string{
			{"OPENAI_API_KEY"},
		},
		InteractiveCommand: "codex --dangerously-bypass-approvals-and-sandbox",
		DefaultImage:       "ghcr.io/banksean/sand/codex:latest",
	},
	{
		Name:        "gemini",
		Selectable:  true,
		Preparation: PreparationBase,
		Container:   ContainerBase,
		AuthEnvAnyOf: [][]string{
			{"GEMINI_API_KEY"},
			{"GOOGLE_API_KEY"},
		},
		InteractiveCommand: "gemini --approval-mode=yolo",
		OneShotCommand:     `gemini --approval-mode=yolo -p "$SAND_ONESHOT_PROMPT"`,
		DefaultImage:       "ghcr.io/banksean/sand/gemini:latest",
	},
	{
		Name:        "opencode",
		Selectable:  true,
		Preparation: PreparationOpenCode,
		Container:   ContainerOpenCode,
		AuthEnvAnyOf: [][]string{
			{"ANTHROPIC_API_KEY"},
			{"OPENAI_API_KEY"},
			{"GEMINI_API_KEY"},
			{"GOOGLE_API_KEY"},
			{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"},
			{"AWS_PROFILE"},
			{"AWS_BEARER_TOKEN_BEDROCK"},
		},
		InteractiveCommand: "opencode",
		OneShotCommand:     `opencode run "$SAND_ONESHOT_PROMPT"`,
		DefaultImage:       "ghcr.io/banksean/sand/opencode:latest",
	},
}

// All returns all built-in agent definitions in declaration order.
func All() []Definition {
	return cloneDefinitions(definitions)
}

// Lookup returns the built-in agent definition with name.
func Lookup(name string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.Name == name {
			return cloneDefinition(definition), true
		}
	}
	return Definition{}, false
}

// LaunchableNames returns the names of agents with an interactive command.
func LaunchableNames() []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if definition.InteractiveCommand != "" {
			names = append(names, definition.Name)
		}
	}
	sort.Strings(names)
	return names
}

func cloneDefinitions(src []Definition) []Definition {
	dst := make([]Definition, len(src))
	for i, definition := range src {
		dst[i] = cloneDefinition(definition)
	}
	return dst
}

func cloneDefinition(src Definition) Definition {
	src.AuthEnvAnyOf = cloneEnvGroups(src.AuthEnvAnyOf)
	return src
}

func cloneEnvGroups(src [][]string) [][]string {
	if src == nil {
		return nil
	}
	dst := make([][]string, len(src))
	for i, group := range src {
		dst[i] = append([]string(nil), group...)
	}
	return dst
}
