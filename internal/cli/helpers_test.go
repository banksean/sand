package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/banksean/sand/internal/sandtypes"
)

func TestBuildInteractiveEnv(t *testing.T) {
	t.Run("default shell env", func(t *testing.T) {
		env := buildInteractiveEnv("sandbox.local", false, nil)
		if env["HOSTNAME"] != "sandbox.local" {
			t.Fatalf("HOSTNAME = %q, want sandbox.local", env["HOSTNAME"])
		}
		if _, ok := env["SSH_AUTH_SOCK"]; ok {
			t.Fatal("SSH_AUTH_SOCK unexpectedly set for normal interactive shell")
		}
		if _, ok := env["SSH_AGENT_PID"]; ok {
			t.Fatal("SSH_AGENT_PID unexpectedly set for normal interactive shell")
		}
	})

	t.Run("agent shell scrubs ssh agent vars", func(t *testing.T) {
		env := buildInteractiveEnv("sandbox.local", true, map[string]string{"OPENAI_API_KEY": "sk-test"})
		if env["SSH_AUTH_SOCK"] != "" {
			t.Fatalf("SSH_AUTH_SOCK = %q, want empty string", env["SSH_AUTH_SOCK"])
		}
		if env["SSH_AGENT_PID"] != "" {
			t.Fatalf("SSH_AGENT_PID = %q, want empty string", env["SSH_AGENT_PID"])
		}
		if env["OPENAI_API_KEY"] != "sk-test" {
			t.Fatalf("OPENAI_API_KEY = %q, want %q", env["OPENAI_API_KEY"], "sk-test")
		}
	})
}

func TestPlainCommandEnvFile(t *testing.T) {
	sbox := &sandtypes.Box{EnvFile: "/tmp/project.env"}

	if got := plainCommandEnvFile(sbox, false); got != "" {
		t.Fatalf("plainCommandEnvFile(false) = %q, want empty", got)
	}
	if got := plainCommandEnvFile(sbox, true); got != sbox.EnvFile {
		t.Fatalf("plainCommandEnvFile(true) = %q, want %q", got, sbox.EnvFile)
	}
}

func TestSelectedProfileEnvPolicy(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(project, ".sand.yaml"), []byte(`
profiles:
  dev:
    env:
      files:
        - path: .env
          scope: auth
      vars:
        - name: OPENAI_API_KEY
          scope: auth
`), 0o644); err != nil {
		t.Fatal(err)
	}

	policy, configured, err := selectedProfileEnvPolicy(&sandtypes.Box{
		HostOriginDir: project,
		ProfileName:   "dev",
	})
	if err != nil {
		t.Fatalf("selectedProfileEnvPolicy: %v", err)
	}
	if !configured {
		t.Fatal("configured = false, want true")
	}
	if len(policy.Files) != 1 || policy.Files[0].Path != filepath.Join(project, ".env") {
		t.Fatalf("files = %#v, want project .env", policy.Files)
	}
	if len(policy.Vars) != 1 || policy.Vars[0].Name != "OPENAI_API_KEY" {
		t.Fatalf("vars = %#v, want OPENAI_API_KEY", policy.Vars)
	}
}

func TestSelectedProfileEnvPolicyAllowsMissingDefaultProfile(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	policy, configured, err := selectedProfileEnvPolicy(&sandtypes.Box{
		HostOriginDir: project,
		ProfileName:   sandtypes.DefaultProfileName,
	})
	if err != nil {
		t.Fatalf("selectedProfileEnvPolicy: %v", err)
	}
	if configured {
		t.Fatal("configured = true, want false")
	}
	if len(policy.Files) != 0 || len(policy.Vars) != 0 {
		t.Fatalf("policy = %#v, want empty", policy)
	}
}

func TestSelectedProfileEnvPolicyRejectsMissingNamedProfile(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	_, _, err := selectedProfileEnvPolicy(&sandtypes.Box{
		HostOriginDir: project,
		ProfileName:   "dev",
	})
	if err == nil {
		t.Fatal("expected missing profile error")
	}
}
