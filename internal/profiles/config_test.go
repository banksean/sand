package profiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/banksean/sand/internal/sandtypes"
)

func TestLoadConfigLoadsProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".sand.yaml")
	if err := os.WriteFile(path, []byte(`
profiles:
  default:
    dotfiles:
      mode: minimal
      files:
        - source: ~/.gitconfig
          target: ~/.gitconfig
          allowSymlink: true
    env:
      files:
        - path: .env
          scope: auth
      vars:
        - name: OPENAI_API_KEY
          scope: auth
    ssh:
      agentForwarding: opt-in
    git:
      config: sanitized
    network:
      allowedDomainsFile: allowed-domains.txt
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	profile, ok := cfg.Profiles["default"]
	if !ok {
		t.Fatalf("default profile missing: %#v", cfg.Profiles)
	}
	if profile.Name != "default" {
		t.Fatalf("profile name = %q, want default", profile.Name)
	}
	if profile.Dotfiles.Mode != sandtypes.DotfileModeMinimal {
		t.Fatalf("dotfiles mode = %q, want minimal", profile.Dotfiles.Mode)
	}
	if len(profile.Dotfiles.Files) != 1 || !profile.Dotfiles.Files[0].AllowSymlink {
		t.Fatalf("dotfile rules = %#v, want one symlink-allowed rule", profile.Dotfiles.Files)
	}
	if len(profile.Env.Files) != 1 || profile.Env.Files[0].Scope != sandtypes.EnvScopeAuth {
		t.Fatalf("env files = %#v, want one auth file", profile.Env.Files)
	}
	if len(profile.Env.Vars) != 1 || profile.Env.Vars[0].Name != "OPENAI_API_KEY" {
		t.Fatalf("env vars = %#v, want OPENAI_API_KEY", profile.Env.Vars)
	}
	if profile.SSH.AgentForwarding != sandtypes.SSHAgentModeOptIn {
		t.Fatalf("ssh agent mode = %q, want opt-in", profile.SSH.AgentForwarding)
	}
	if profile.Git.Config != sandtypes.GitConfigPolicySanitized {
		t.Fatalf("git config policy = %q, want sanitized", profile.Git.Config)
	}
	if profile.Network.AllowedDomainsFile != "allowed-domains.txt" {
		t.Fatalf("allowed domains file = %q, want allowed-domains.txt", profile.Network.AllowedDomainsFile)
	}
}

func TestLoadConfigMergesProfilesInPathOrder(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "user.yaml")
	overridePath := filepath.Join(dir, "project.yaml")
	if err := os.WriteFile(basePath, []byte(`
profiles:
  default:
    dotfiles:
      mode: minimal
  user-only:
    git:
      config: sanitized
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte(`
profiles:
  default:
    dotfiles:
      mode: allowlist
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(basePath, overridePath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if got := cfg.Profiles["default"].Dotfiles.Mode; got != sandtypes.DotfileModeAllowlist {
		t.Fatalf("default dotfiles mode = %q, want allowlist", got)
	}
	if _, ok := cfg.Profiles["user-only"]; !ok {
		t.Fatalf("user-only profile missing after merge: %#v", cfg.Profiles)
	}
}

func TestLoadConfigIgnoresMissingAndEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(filepath.Join(dir, "missing.yaml"), emptyPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Profiles) != 0 {
		t.Fatalf("profiles = %#v, want empty", cfg.Profiles)
	}
}
