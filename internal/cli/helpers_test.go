package cli

import (
	"os"
	"path/filepath"
	"strings"
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

func TestPlainCommandProjectEnvUsesLegacyEnvFileWhenDefaultProfileMissing(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	sbox := &sandtypes.Box{
		EnvFile:       "/tmp/project.env",
		HostOriginDir: project,
		ProfileName:   sandtypes.DefaultProfileName,
	}

	env, err := plainCommandProjectEnv(sbox, true)
	if err != nil {
		t.Fatalf("plainCommandProjectEnv: %v", err)
	}
	if env.EnvFile != sbox.EnvFile {
		t.Fatalf("EnvFile = %q, want %q", env.EnvFile, sbox.EnvFile)
	}
	if len(env.Env) != 0 {
		t.Fatalf("Env = %#v, want empty", env.Env)
	}
}

func TestPlainCommandProjectEnvUsesOnlyProjectScopedProfileEnv(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PROJECT_TOKEN", "project-token")
	t.Setenv("AUTH_TOKEN", "auth-token")

	projectFile := filepath.Join(project, "project.env")
	authFile := filepath.Join(project, "auth.env")
	if err := os.WriteFile(projectFile, []byte("PROJECT_FILE=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authFile, []byte("AUTH_FILE=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".sand.yaml"), []byte(`
profiles:
  dev:
    env:
      files:
        - path: project.env
          scope: project
        - path: auth.env
          scope: auth
      vars:
        - name: PROJECT_TOKEN
          scope: project
        - name: AUTH_TOKEN
          scope: auth
`), 0o644); err != nil {
		t.Fatal(err)
	}

	env, err := plainCommandProjectEnv(&sandtypes.Box{
		HostOriginDir: project,
		ProfileName:   "dev",
	}, true)
	if err != nil {
		t.Fatalf("plainCommandProjectEnv: %v", err)
	}
	defer env.Cleanup()

	if env.EnvFile != projectFile {
		t.Fatalf("EnvFile = %q, want %q", env.EnvFile, projectFile)
	}
	if env.Env["PROJECT_TOKEN"] != "project-token" {
		t.Fatalf("PROJECT_TOKEN = %q, want project-token", env.Env["PROJECT_TOKEN"])
	}
	if _, ok := env.Env["AUTH_TOKEN"]; ok {
		t.Fatalf("AUTH_TOKEN unexpectedly exposed: %#v", env.Env)
	}
}

func TestPlainCommandProjectEnvMergesMultipleProjectEnvFiles(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.WriteFile(filepath.Join(project, "one.env"), []byte("ONE=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "two.env"), []byte("TWO=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".sand.yaml"), []byte(`
profiles:
  dev:
    env:
      files:
        - path: one.env
          scope: project
        - path: two.env
          scope: all
`), 0o644); err != nil {
		t.Fatal(err)
	}

	env, err := plainCommandProjectEnv(&sandtypes.Box{
		HostOriginDir: project,
		ProfileName:   "dev",
	}, true)
	if err != nil {
		t.Fatalf("plainCommandProjectEnv: %v", err)
	}
	defer env.Cleanup()

	if env.EnvFile == filepath.Join(project, "one.env") || env.EnvFile == filepath.Join(project, "two.env") || env.EnvFile == "" {
		t.Fatalf("EnvFile = %q, want generated merged file", env.EnvFile)
	}
	content, err := os.ReadFile(env.EnvFile)
	if err != nil {
		t.Fatalf("ReadFile merged env: %v", err)
	}
	if got := string(content); !strings.Contains(got, "ONE=1") || !strings.Contains(got, "TWO=2") {
		t.Fatalf("merged env content = %q, want both files", got)
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
