package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sshimmer"
)

func TestResolveCreateSandboxRequirementsRejectsUnknownAgent(t *testing.T) {
	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{})

	_, err := d.resolveCreateSandboxRequirements(CreateSandboxOpts{Agent: "not-real"})
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

func TestResolveCreateSandboxRequirementsAllowsNoAgentWithoutAuthRequirements(t *testing.T) {
	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{})

	caps, err := d.resolveCreateSandboxRequirements(CreateSandboxOpts{})
	if err != nil {
		t.Fatalf("resolveCreateSandboxRequirements returned error: %v", err)
	}
	if caps.AuthRequired {
		t.Fatal("AuthRequired = true, want false")
	}
	if caps.AuthAvailable {
		t.Fatal("AuthAvailable = true, want false")
	}
}

func TestResolveCreateSandboxRequirementsSupportsOpenCodeProviderEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "opencode-key")
	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{})

	caps, err := d.resolveCreateSandboxRequirements(CreateSandboxOpts{Agent: "opencode"})
	if err != nil {
		t.Fatalf("resolveCreateSandboxRequirements returned error: %v", err)
	}
	if !caps.AuthRequired || !caps.AuthAvailable {
		t.Fatalf("resolved requirements = %+v, want auth required and available", caps)
	}
	if got := caps.AuthEnv["OPENAI_API_KEY"]; got != "opencode-key" {
		t.Fatalf("resolved OPENAI_API_KEY = %q, want %q", got, "opencode-key")
	}
}

func TestResolveCreateSandboxRequirementsUsesProcessEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{})

	caps, err := d.resolveCreateSandboxRequirements(CreateSandboxOpts{Agent: "codex"})
	if err != nil {
		t.Fatalf("resolveCreateSandboxRequirements returned error: %v", err)
	}
	if !caps.AuthRequired || !caps.AuthAvailable {
		t.Fatalf("resolved requirements = %+v, want auth required and available", caps)
	}
	if got := caps.AuthEnv["OPENAI_API_KEY"]; got != "test-key" {
		t.Fatalf("resolved OPENAI_API_KEY = %q, want %q", got, "test-key")
	}
}

func TestResolveCreateSandboxRequirementsUsesEnvFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("OPENAI_API_KEY=from-file\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{})
	caps, err := d.resolveCreateSandboxRequirements(CreateSandboxOpts{Agent: "codex", EnvFile: envFile})
	if err != nil {
		t.Fatalf("resolveCreateSandboxRequirements returned error: %v", err)
	}
	if !caps.AuthRequired || !caps.AuthAvailable {
		t.Fatalf("resolved requirements = %+v, want auth required and available", caps)
	}
	if got := caps.AuthEnv["OPENAI_API_KEY"]; got != "from-file" {
		t.Fatalf("resolved OPENAI_API_KEY = %q, want %q", got, "from-file")
	}
}

func TestResolveCreateSandboxRequirementsSupportsMultiVarRequirementGroup(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_SCOPES", "")
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("CLAUDE_CODE_OAUTH_REFRESH_TOKEN=refresh\nCLAUDE_CODE_OAUTH_SCOPES=user:profile\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{})
	caps, err := d.resolveCreateSandboxRequirements(CreateSandboxOpts{Agent: "claude", EnvFile: envFile})
	if err != nil {
		t.Fatalf("resolveCreateSandboxRequirements returned error: %v", err)
	}
	if !caps.AuthRequired || !caps.AuthAvailable {
		t.Fatalf("resolved requirements = %+v, want auth required and available", caps)
	}
	if got := caps.AuthEnv["CLAUDE_CODE_OAUTH_REFRESH_TOKEN"]; got != "refresh" {
		t.Fatalf("resolved refresh token = %q, want %q", got, "refresh")
	}
	if got := caps.AuthEnv["CLAUDE_CODE_OAUTH_SCOPES"]; got != "user:profile" {
		t.Fatalf("resolved scopes = %q, want %q", got, "user:profile")
	}
}

func TestResolveCreateSandboxRequirementsRejectsMissingAuthEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{})

	_, err := d.resolveCreateSandboxRequirements(CreateSandboxOpts{Agent: "gemini", EnvFile: envFile})
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

func TestCreateSandboxAllowsMissingAuthBeforeAgentLaunch(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	var createCalls int
	var inspectCalls int
	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{
		CreateFunc: func(ctx context.Context, _ *options.CreateContainer, image string, args []string) (string, error) {
			createCalls++
			return "mock-container-id", nil
		},
		InspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
			inspectCalls++
			if inspectCalls == 1 {
				return nil, nil
			}
			return []types.Container{{Status: "running"}}, nil
		},
	})

	_, err := d.createSandbox(context.Background(), CreateSandboxOpts{
		ID:    "test-box",
		Agent: "codex",
	}, io.Discard)
	if err != nil {
		t.Fatalf("createSandbox() error = %v, want nil", err)
	}
	if createCalls != 1 {
		t.Fatalf("container Create called %d times, want 1", createCalls)
	}
}

func TestCreateSandboxRejectsUnknownAgentBeforeSandboxCreation(t *testing.T) {
	var createCalls int
	d := newRequirementTestDaemon(t, requirementTestRegistry(), &hostops.MockContainerOps{
		CreateFunc: func(ctx context.Context, _ *options.CreateContainer, image string, args []string) (string, error) {
			createCalls++
			return "mock-container-id", nil
		},
		InspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
			return nil, nil
		},
	})

	_, err := d.createSandbox(context.Background(), CreateSandboxOpts{
		ID:    "test-box",
		Agent: "not-real",
	}, io.Discard)
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if createCalls != 0 {
		t.Fatalf("container Create called %d times, want 0", createCalls)
	}
}

func newRequirementTestDaemon(t *testing.T, registry *cloning.AgentRegistry, containerSvc *hostops.MockContainerOps) *Daemon {
	t.Helper()

	// Keep this short because it still appears in a few test fixture paths. Runtime
	// container sockets use /tmp/sand-<uid>/ and are independent of appDir length.
	appDir, err := os.MkdirTemp("", "sdt-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(appDir) })

	b, err := boxer.NewBoxerWithDeps(appDir, boxer.BoxerDeps{
		ContainerService: containerSvc,
		GitOps:           &hostops.MockGitOps{},
		FileOps: &hostops.MockFileOps{
			MkdirAllFunc: os.MkdirAll,
			CreateFunc:   os.Create,
		},
		SSHim:         &requirementTestSSHimmer{},
		AgentRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewBoxerWithDeps: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	return NewDaemonWithBoxer(appDir, "test", b)
}

func requirementTestRegistry() *cloning.AgentRegistry {
	prep := &requirementTestPreparation{cloneRoot: filepath.Join(os.TempDir(), "sand-requirement-test")}
	config := cloning.NewBaseContainerConfiguration()
	r := cloning.NewAgentRegistry()
	for _, definition := range agentdefs.All() {
		r.Register(&cloning.AgentConfig{
			Name:          definition.Name,
			Selectable:    definition.Selectable,
			Preparation:   prep,
			Configuration: config,
			Requirements:  requirementTestRequirements(definition),
		})
	}
	return r
}

func requirementTestRequirements(definition agentdefs.Definition) cloning.AgentRequirements {
	if len(definition.AuthEnvAnyOf) == 0 {
		return cloning.AgentRequirements{}
	}
	return cloning.AgentRequirements{
		Auth: &cloning.AuthRequirementSpec{
			EnvAnyOf: definition.AuthEnvAnyOf,
		},
	}
}

type requirementTestPreparation struct {
	cloneRoot string
}

func (p *requirementTestPreparation) Prepare(_ context.Context, req cloning.CloneRequest) (*cloning.CloneArtifacts, error) {
	sandboxRoot := filepath.Join(p.cloneRoot, req.ID)
	return &cloning.CloneArtifacts{
		SandboxWorkDir:    sandboxRoot,
		PathRegistry:      cloning.NewStandardPathRegistry(sandboxRoot),
		Username:          req.Username,
		Uid:               req.Uid,
		SharedCacheMounts: req.SharedCacheMounts,
	}, nil
}

type requirementTestSSHimmer struct{}

func (s *requirementTestSSHimmer) NewKeys(_ context.Context, _, _ string) (*sshimmer.Keys, error) {
	return &sshimmer.Keys{
		HostKey:     []byte("fake-host-key"),
		HostKeyPub:  []byte("fake-host-key-pub"),
		HostKeyCert: []byte("fake-host-key-cert"),
		UserCAPub:   []byte("fake-user-ca-pub"),
	}, nil
}
