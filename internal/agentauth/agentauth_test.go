package agentauth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSelectionRejectsUnknownAgent(t *testing.T) {
	err := ValidateSelection("not-real", "")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if got := err.Error(); !strings.Contains(got, `unknown agent "not-real"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSelectionAllowsKnownAgentWithoutAuthRequirements(t *testing.T) {
	if err := ValidateSelection("opencode", ""); err != nil {
		t.Fatalf("ValidateSelection returned error: %v", err)
	}
}

func TestValidateSelectionUsesProcessEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	if err := ValidateSelection("codex", ""); err != nil {
		t.Fatalf("ValidateSelection returned error: %v", err)
	}
}

func TestValidateSelectionUsesEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("OPENAI_API_KEY=from-file\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	if err := ValidateSelection("codex", envFile); err != nil {
		t.Fatalf("ValidateSelection returned error: %v", err)
	}
}

func TestValidateSelectionSupportsMultiVarRequirementGroup(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_CODE_OAUTH_REFRESH_TOKEN=refresh\nCLAUDE_CODE_OAUTH_SCOPES=user:profile\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	if err := ValidateSelection("claude", envFile); err != nil {
		t.Fatalf("ValidateSelection returned error: %v", err)
	}
}

func TestValidateSelectionRejectsMissingAuthEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	err := ValidateSelection("gemini", envFile)
	if err == nil {
		t.Fatal("expected error for missing auth env")
	}
	if got := err.Error(); !strings.Contains(got, "GEMINI_API_KEY") || !strings.Contains(got, "GOOGLE_API_KEY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadEnvFileValuesParsesExportAndQuotedValues(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := strings.Join([]string{
		"# comment",
		`export OPENAI_API_KEY="abc123"`,
		`GEMINI_API_KEY='def456'`,
		"INVALID LINE",
		"",
	}, "\n")
	if err := os.WriteFile(envFile, []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	values, err := loadEnvFileValues(envFile)
	if err != nil {
		t.Fatalf("loadEnvFileValues returned error: %v", err)
	}
	if got := values["OPENAI_API_KEY"]; got != "abc123" {
		t.Fatalf("OPENAI_API_KEY = %q, want %q", got, "abc123")
	}
	if got := values["GEMINI_API_KEY"]; got != "def456" {
		t.Fatalf("GEMINI_API_KEY = %q, want %q", got, "def456")
	}
}
