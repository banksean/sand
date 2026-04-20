package agentlaunch

import (
	"reflect"
	"testing"
)

func TestBuildInteractiveExec_DefaultShell(t *testing.T) {
	shell, args, err := BuildInteractiveExec("", "/bin/zsh", "sand-1", "sand-1.test.", false)
	if err != nil {
		t.Fatalf("BuildInteractiveExec() error = %v", err)
	}
	if shell != "/bin/zsh" {
		t.Fatalf("expected shell /bin/zsh, got %q", shell)
	}
	if args != nil {
		t.Fatalf("expected nil args for plain shell, got %#v", args)
	}
}

func TestBuildInteractiveExec_CodexUsesSingleCommandString(t *testing.T) {
	shell, args, err := BuildInteractiveExec("codex", "/bin/zsh", "sand-1", "sand-1.test.", false)
	if err != nil {
		t.Fatalf("BuildInteractiveExec() error = %v", err)
	}
	if shell != "/bin/zsh" {
		t.Fatalf("expected shell /bin/zsh, got %q", shell)
	}

	want := []string{"-c", "codex --dangerously-bypass-approvals-and-sandbox"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("expected args %#v, got %#v", want, args)
	}
}

func TestBuildInteractiveExec_TmuxWrapsAgentCommand(t *testing.T) {
	shell, args, err := BuildInteractiveExec("opencode", "/bin/zsh", "sand-1", "sand-1.test.", true)
	if err != nil {
		t.Fatalf("BuildInteractiveExec() error = %v", err)
	}
	if shell != "/usr/bin/tmux" {
		t.Fatalf("expected tmux shell, got %q", shell)
	}

	want := []string{
		"new-session",
		"-A",
		"-s",
		"opencode-sand-1",
		"opencode --port 80 --hostname sand-1.test",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("expected args %#v, got %#v", want, args)
	}
}

func TestBuildInteractiveExec_UnknownAgentErrors(t *testing.T) {
	if _, _, err := BuildInteractiveExec("unknown", "/bin/zsh", "sand-1", "sand-1.test.", false); err == nil {
		t.Fatal("expected error for unknown interactive agent")
	}
}

func TestBuildOneShotExec(t *testing.T) {
	got, err := BuildOneShotExec("gemini")
	if err != nil {
		t.Fatalf("BuildOneShotExec() error = %v", err)
	}
	want := `gemini --approval-mode=yolo -p "$SAND_ONESHOT_PROMPT"`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildOneShotExec_UnsupportedAgent(t *testing.T) {
	if _, err := BuildOneShotExec("codex"); err == nil {
		t.Fatal("expected error for unsupported one-shot agent")
	}
}
