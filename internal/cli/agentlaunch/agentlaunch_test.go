package agentlaunch

import (
	"fmt"
	"reflect"
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
			name:      "tmux wraps opencode command",
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
				"opencode --port 80 --hostname sand-1.test",
			},
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
			gotShell, gotArgs, err := BuildInteractiveExec(tt.agent, tt.shell, tt.sandboxID, tt.hostname, tt.tmux)
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

func TestDefaultImage(t *testing.T) {
	tests := []struct {
		agent    string
		fallback string
		want     string
	}{
		{agent: "", fallback: "fallback:latest", want: "fallback:latest"},
		{agent: "claude", fallback: "fallback:latest", want: "ghcr.io/banksean/sand/claude:latest"},
		{agent: "codex", fallback: "fallback:latest", want: "ghcr.io/banksean/sand/codex:latest"},
		{agent: "gemini", fallback: "fallback:latest", want: "ghcr.io/banksean/sand/gemini:latest"},
		{agent: "opencode", fallback: "fallback:latest", want: "ghcr.io/banksean/sand/opencode:latest"},
		{agent: "unknown", fallback: "fallback:latest", want: "fallback:latest"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.agent, tt.fallback), func(t *testing.T) {
			got := DefaultImage(tt.agent, tt.fallback)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
