package agentlaunch

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildInteractiveExec(t *testing.T) {
	tests := []struct {
		name      string
		agent     string
		shell     string
		sandboxID string
		hostname  string
		tmux      bool
		atch      bool
		wantShell string
		wantArgs  []string
		wantErr   string
	}{
		{
			name:      "default shell",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			wantShell: "/bin/zsh",
			wantArgs:  nil,
		},
		{
			name:      "codex single command",
			agent:     "codex",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			wantShell: "/bin/zsh",
			wantArgs:  []string{"-c", "codex --dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name:      "tmux without agent",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			tmux:      true,
			wantShell: "/usr/bin/tmux",
			wantArgs:  []string{"new-session", "-A", "-s", "sand-1"},
		},
		{
			name:      "tmux wraps opencode tui command",
			agent:     "opencode",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			tmux:      true,
			wantShell: "/usr/bin/tmux",
			wantArgs: []string{
				"new-session",
				"-A",
				"-s",
				"opencode-sand-1",
				"opencode",
			},
		},
		{
			name:      "atch without agent",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			atch:      true,
			wantShell: "/usr/local/bin/atch",
			wantArgs:  []string{"sand-1", "/bin/zsh"},
		},
		{
			name:      "atch wraps opencode tui command",
			agent:     "opencode",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			atch:      true,
			wantShell: "/usr/local/bin/atch",
			wantArgs: []string{
				"sand-1",
				"/bin/zsh",
				"-c",
				"opencode",
			},
		},
		{
			name:      "atch wraps codex uses sandbox id as session name",
			agent:     "codex",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			atch:      true,
			wantShell: "/usr/local/bin/atch",
			wantArgs: []string{
				"sand-1",
				"/bin/zsh",
				"-c",
				"codex --dangerously-bypass-approvals-and-sandbox",
			},
		},
		{
			name:      "tmux and atch conflict",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			tmux:      true,
			atch:      true,
			wantErr:   "--tmux and --atch cannot be used together",
		},
		{
			name:      "unknown agent",
			agent:     "unknown",
			shell:     "/bin/zsh",
			sandboxID: "sand-1",
			hostname:  "sand-1.test.",
			wantErr:   `interactive mode not supported for agent "unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotShell, gotArgs, err := BuildInteractiveExec(tt.agent, tt.shell, tt.sandboxID, tt.hostname, tt.tmux, tt.atch)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildInteractiveExec() error = %v", err)
			}
			if gotShell != tt.wantShell {
				t.Fatalf("expected shell %q, got %q", tt.wantShell, gotShell)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Fatalf("expected args %#v, got %#v", tt.wantArgs, gotArgs)
			}
		})
	}
}

func TestBuildOneShotExec(t *testing.T) {
	tests := []struct {
		name    string
		agent   string
		want    string
		wantErr string
	}{
		{
			name:  "gemini",
			agent: "gemini",
			want:  `gemini --approval-mode=yolo -p "$SAND_ONESHOT_PROMPT"`,
		},
		{
			name:  "claude",
			agent: "claude",
			want:  `claude --permission-mode=bypassPermissions --print "$SAND_ONESHOT_PROMPT"`,
		},
		{
			name:  "opencode",
			agent: "opencode",
			want:  `opencode run "$SAND_ONESHOT_PROMPT"`,
		},
		{
			name:    "unsupported agent",
			agent:   "codex",
			wantErr: `one-shot mode not supported for agent "codex"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildOneShotExec(tt.agent)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildOneShotExec() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestEphemeralSessionEnvUsesAgentNativeRoots(t *testing.T) {
	tests := map[string]string{
		"codex":    "CODEX_HOME",
		"claude":   "CLAUDE_CONFIG_DIR",
		"gemini":   "HOME",
		"opencode": "XDG_DATA_HOME",
	}
	for agent, key := range tests {
		t.Run(agent, func(t *testing.T) {
			env := EphemeralSessionEnv(agent, "sandbox-1")
			if env[key] == "" {
				t.Fatalf("%s environment = %v", agent, env)
			}
			if got := env[key]; !strings.HasPrefix(got, "/tmp/sand-no-archive/sandbox-1/") {
				t.Fatalf("%s = %q", key, got)
			}
		})
	}
}
