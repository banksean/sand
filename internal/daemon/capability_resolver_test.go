package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sshimmer"
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

func TestResolveCreateSandboxCapabilitiesAllowsNoAgentWithoutAuthRequirements(t *testing.T) {
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{})

	caps, err := d.resolveCreateSandboxCapabilities(CreateSandboxOpts{})
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

func TestResolveCreateSandboxCapabilitiesSupportsOpenCodeProviderEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "opencode-key")
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{})

	caps, err := d.resolveCreateSandboxCapabilities(CreateSandboxOpts{Agent: "opencode"})
	if err != nil {
		t.Fatalf("resolveCreateSandboxCapabilities returned error: %v", err)
	}
	if !caps.AuthRequired || !caps.AuthAvailable {
		t.Fatalf("resolved capabilities = %+v, want auth required and available", caps)
	}
	if got := caps.AuthEnv["OPENAI_API_KEY"]; got != "opencode-key" {
		t.Fatalf("resolved OPENAI_API_KEY = %q, want %q", got, "opencode-key")
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
	if got := caps.AuthEnv["OPENAI_API_KEY"]; got != "test-key" {
		t.Fatalf("resolved OPENAI_API_KEY = %q, want %q", got, "test-key")
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
	if got := caps.AuthEnv["OPENAI_API_KEY"]; got != "from-file" {
		t.Fatalf("resolved OPENAI_API_KEY = %q, want %q", got, "from-file")
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
	if got := caps.AuthEnv["CLAUDE_CODE_OAUTH_REFRESH_TOKEN"]; got != "refresh" {
		t.Fatalf("resolved refresh token = %q, want %q", got, "refresh")
	}
	if got := caps.AuthEnv["CLAUDE_CODE_OAUTH_SCOPES"]; got != "user:profile" {
		t.Fatalf("resolved scopes = %q, want %q", got, "user:profile")
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

func TestCreateSandboxAllowsMissingAuthBeforeAgentLaunch(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	var createCalls int
	var inspectCalls int
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{
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
	d := newCapabilityTestDaemon(t, capabilityTestRegistry(), &hostops.MockContainerOps{
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

func newCapabilityTestDaemon(t *testing.T, registry *cloning.AgentRegistry, containerSvc *hostops.MockContainerOps) *Daemon {
	t.Helper()

	// Keep the app dir prefix short so the derived unix socket path
	// (<appDir>/containersockets/<sandbox-id>) stays within macOS sun_path limits.
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
		SSHim:         &capabilityTestSSHimmer{},
		AgentRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewBoxerWithDeps: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	return NewDaemonWithBoxer(appDir, "test", b)
}

func capabilityTestRegistry() *cloning.AgentRegistry {
	prep := &capabilityTestPreparation{cloneRoot: filepath.Join(os.TempDir(), "sand-capability-test")}
	config := cloning.NewBaseContainerConfiguration()
	r := cloning.NewAgentRegistry()
	r.Register(&cloning.AgentConfig{
		Name:          "default",
		Preparation:   prep,
		Configuration: config,
	})
	r.Register(&cloning.AgentConfig{
		Name:          "claude",
		Selectable:    true,
		Preparation:   prep,
		Configuration: config,
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
		Name:          "codex",
		Selectable:    true,
		Preparation:   prep,
		Configuration: config,
		Capabilities: cloning.AgentCapabilities{
			Auth: &cloning.AuthCapabilitySpec{
				EnvAnyOf: [][]string{
					{"OPENAI_API_KEY"},
				},
			},
		},
	})
	r.Register(&cloning.AgentConfig{
		Name:          "gemini",
		Selectable:    true,
		Preparation:   prep,
		Configuration: config,
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
		Name:          "opencode",
		Selectable:    true,
		Preparation:   prep,
		Configuration: config,
		Capabilities: cloning.AgentCapabilities{
			Auth: &cloning.AuthCapabilitySpec{
				EnvAnyOf: [][]string{
					{"ANTHROPIC_API_KEY"},
					{"OPENAI_API_KEY"},
					{"GEMINI_API_KEY"},
					{"GOOGLE_API_KEY"},
					{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"},
					{"AWS_PROFILE"},
					{"AWS_BEARER_TOKEN_BEDROCK"},
				},
			},
		},
	})
	return r
}

type capabilityTestPreparation struct {
	cloneRoot string
}

func (p *capabilityTestPreparation) Prepare(_ context.Context, req cloning.CloneRequest) (*cloning.CloneArtifacts, error) {
	sandboxRoot := filepath.Join(p.cloneRoot, req.ID)
	return &cloning.CloneArtifacts{
		SandboxWorkDir:    sandboxRoot,
		PathRegistry:      cloning.NewStandardPathRegistry(sandboxRoot),
		Username:          req.Username,
		Uid:               req.Uid,
		SharedCacheMounts: req.SharedCacheMounts,
	}, nil
}

type capabilityTestSSHimmer struct{}

func (s *capabilityTestSSHimmer) NewKeys(_ context.Context, _, _ string) (*sshimmer.Keys, error) {
	return &sshimmer.Keys{
		HostKey:     []byte("fake-host-key"),
		HostKeyPub:  []byte("fake-host-key-pub"),
		HostKeyCert: []byte("fake-host-key-cert"),
		UserCAPub:   []byte("fake-user-ca-pub"),
	}, nil
}
