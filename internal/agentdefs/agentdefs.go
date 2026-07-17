package agentdefs

import "sort"

const (
	HookOpenCodeTunnel = "opencode-ssh-tunnel"

	InstallerNPM      = "npm"
	InstallerOpenCode = "opencode"
)

// Definition is the shared declaration for a built-in agent.
type Definition struct {
	Name               string
	Selectable         bool
	AuthEnvAnyOf       [][]string
	InteractiveCommand string
	OneShotCommand     string
	GeneratedFiles     []GeneratedFile
	Install            *InstallSpec
	StartHooks         []string
}

// GeneratedFile is non-secret startup state written below the sandbox dotfiles
// mount and copied into the container user's home directory during bootstrap.
type GeneratedFile struct {
	Path    string
	Content string
	Mode    uint32
}

type InstallSpec struct {
	Kind    string
	Package string
	Version string
	Command string
}

var definitions = []Definition{
	{
		Name: "default",
	},
	{
		Name:       "claude",
		Selectable: true,
		AuthEnvAnyOf: [][]string{
			{"CLAUDE_CODE_OAUTH_TOKEN"},
			{"ANTHROPIC_API_KEY"},
			{"CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_OAUTH_SCOPES"},
		},
		InteractiveCommand: "claude --permission-mode=bypassPermissions",
		OneShotCommand:     `claude --permission-mode=bypassPermissions --print "$SAND_ONESHOT_PROMPT"`,
		GeneratedFiles: []GeneratedFile{{
			Path:    ".claude.json",
			Content: `{"hasCompletedOnboarding":true,"projects":{"/app":{}}}`,
			Mode:    0o700,
		}},
		Install: &InstallSpec{
			Kind:    InstallerNPM,
			Package: "@anthropic-ai/claude-code",
			Version: "2.1.165",
			Command: "claude",
		},
	},
	{
		Name:       "codex",
		Selectable: true,
		AuthEnvAnyOf: [][]string{
			{"OPENAI_API_KEY"},
		},
		InteractiveCommand: "codex --dangerously-bypass-approvals-and-sandbox",
		GeneratedFiles: []GeneratedFile{{
			Path: ".codex/config.toml",
			Content: `[otel]
trace_exporter = { otlp-http = {
  endpoint = "http://tempo.dev.local:4318/v1/traces",
  protocol = "binary"
}}
`,
			Mode: 0o600,
		}},
		Install: &InstallSpec{
			Kind:    InstallerNPM,
			Package: "@openai/codex",
			Version: "0.137.0",
			Command: "codex",
		},
	},
	{
		Name:       "gemini",
		Selectable: true,
		AuthEnvAnyOf: [][]string{
			{"GEMINI_API_KEY"},
			{"GOOGLE_API_KEY"},
		},
		InteractiveCommand: "gemini --approval-mode=yolo",
		OneShotCommand:     `gemini --approval-mode=yolo -p "$SAND_ONESHOT_PROMPT"`,
		Install: &InstallSpec{
			Kind:    InstallerNPM,
			Package: "@google/gemini-cli",
			Version: "0.45.2",
			Command: "gemini",
		},
	},
	{
		Name:       "opencode",
		Selectable: true,
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
		GeneratedFiles: []GeneratedFile{{
			Path: ".config/opencode/opencode.json",
			Content: `{
  "$schema": "https://opencode.ai/config.json"
}`,
			Mode: 0o700,
		}},
		Install: &InstallSpec{
			Kind:    InstallerOpenCode,
			Version: "1.14.48",
			Command: "opencode",
		},
		StartHooks: []string{HookOpenCodeTunnel},
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
	src.GeneratedFiles = append([]GeneratedFile(nil), src.GeneratedFiles...)
	if src.Install != nil {
		install := *src.Install
		src.Install = &install
	}
	src.StartHooks = append([]string(nil), src.StartHooks...)
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
