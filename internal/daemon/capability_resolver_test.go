package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/hostops"
)

func TestResolveCreateSandboxCapabilitiesRejectsUnknownAgent(t *testing.T) {
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{})

	_, err := d.resolveCreateSandboxCapabilities(CreateSandboxOpts{Agent: "not-real"})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if got := err.Error(); !strings.Contains(got, `unknown agent "not-real"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "claude, codex, gemini, opencode") {
		t.Fatalf("unexpected supported agent list: %v", err)
	}
}

func TestResolveCreateSandboxCapabilitiesAllowsKnownAgentWithoutAuthRequirements(t *testing.T) {
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{})

	caps, err := d.resolveCreateSandboxCapabilities(CreateSandboxOpts{Agent: "opencode"})
	if err != nil {
		t.Fatalf("resolveCreateSandboxCapabilities returned error: %v", err)
	}
	if caps.AuthRequired {
		t.Fatal("AuthRequired = true, want false")
	}
	if caps.AuthAvailable {
		t.Fatal("AuthAvailable = true, want false")
	}
}

func TestResolveCreateSandboxCapabilitiesUsesProcessEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{})

	caps, err := d.resolveCreateSandboxCapabilities(CreateSandboxOpts{Agent: "codex"})
	if err != nil {
		t.Fatalf("resolveCreateSandboxCapabilities returned error: %v", err)
	}
	if !caps.AuthRequired || !caps.AuthAvailable {
		t.Fatalf("resolved capabilities = %+v, want auth required and available", caps)
	}
}

func TestResolveCreateSandboxCapabilitiesUsesEnvFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("OPENAI_API_KEY=from-file\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{})
	caps, err := d.resolveCreateSandboxCapabilities(CreateSandboxOpts{Agent: "codex", EnvFile: envFile})
	if err != nil {
		t.Fatalf("resolveCreateSandboxCapabilities returned error: %v", err)
	}
	if !caps.AuthRequired || !caps.AuthAvailable {
		t.Fatalf("resolved capabilities = %+v, want auth required and available", caps)
	}
}

func TestResolveCreateSandboxCapabilitiesSupportsMultiVarRequirementGroup(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_SCOPES", "")
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_CODE_OAUTH_REFRESH_TOKEN=refresh\nCLAUDE_CODE_OAUTH_SCOPES=user:profile\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{})
	caps, err := d.resolveCreateSandboxCapabilities(CreateSandboxOpts{Agent: "claude", EnvFile: envFile})
	if err != nil {
		t.Fatalf("resolveCreateSandboxCapabilities returned error: %v", err)
	}
	if !caps.AuthRequired || !caps.AuthAvailable {
		t.Fatalf("resolved capabilities = %+v, want auth required and available", caps)
	}
}

func TestResolveCreateSandboxCapabilitiesRejectsMissingAuthEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{})

	_, err := d.resolveCreateSandboxCapabilities(CreateSandboxOpts{Agent: "gemini", EnvFile: envFile})
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

func TestCreateSandboxRejectsMissingAuthBeforeSandboxCreation(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	var createCalls int
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{
		CreateFunc: func(ctx context.Context, _ *options.CreateContainer, image string, args []string) (string, error) {
			createCalls++
			return "mock-container-id", nil
		},
	})

	_, err := d.createSandbox(context.Background(), CreateSandboxOpts{
		ID:    "test-box",
		Agent: "codex",
	}, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing auth env")
	}
	if createCalls != 0 {
		t.Fatalf("container Create called %d times, want 0", createCalls)
	}
}

func newCapabilityTestDaemon(t *testing.T, registry *cloning.AgentRegistry, containerSvc *hostops.MockContainerOps) *Daemon {
	t.Helper()

	appDir := t.TempDir()
	b, err := boxer.NewBoxerWithDeps(appDir, boxer.BoxerDeps{
		ContainerService: containerSvc,
		AgentRegistry:    registry,
	})
	if err != nil {
		t.Fatalf("NewBoxerWithDeps: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	return NewDaemonWithBoxer(appDir, "test", b)
}

func capabilityTestRegistry() *cloning.AgentRegistry {
	r := cloning.NewAgentRegistry()
	r.Register(&cloning.AgentConfig{Name: "default"})
	r.Register(&cloning.AgentConfig{
		Name:       "claude",
		Selectable: true,
		Capabilities: cloning.AgentCapabilities{
			Auth: &cloning.AuthCapabilitySpec{
				EnvAnyOf: [][]string{
					{"CLAUDE_CODE_OAUTH_TOKEN"},
					{"ANTHROPIC_API_KEY"},
					{"CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_OAUTH_SCOPES"},
				},
			},
		},
	})
	r.Register(&cloning.AgentConfig{
		Name:       "codex",
		Selectable: true,
		Capabilities: cloning.AgentCapabilities{
			Auth: &cloning.AuthCapabilitySpec{
				EnvAnyOf: [][]string{
					{"OPENAI_API_KEY"},
				},
			},
		},
	})
	r.Register(&cloning.AgentConfig{
		Name:       "gemini",
		Selectable: true,
		Capabilities: cloning.AgentCapabilities{
			Auth: &cloning.AuthCapabilitySpec{
				EnvAnyOf: [][]string{
					{"GEMINI_API_KEY"},
					{"GOOGLE_API_KEY"},
				},
			},
		},
	})
	r.Register(&cloning.AgentConfig{
		Name:       "opencode",
		Selectable: true,
	})
	return r
}
