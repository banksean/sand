package cloning

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/banksean/sand/internal/hostops"
)

func TestClaudeWorkspacePreparationWritesOnlyNonSecretStartupState(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	work := filepath.Join(dir, "work")
	cloneRoot := filepath.Join(dir, "clones")

	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o750); err != nil {
		t.Fatalf("MkdirAll .claude: %v", err)
	}
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatalf("MkdirAll work: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(`{"secret":"token"}`), 0o600); err != nil {
		t.Fatalf("WriteFile credentials: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"secret":"host-state"}`), 0o600); err != nil {
		t.Fatalf("WriteFile .claude.json: %v", err)
	}
	t.Setenv("HOME", home)

	prep := NewClaudeWorkspacePreparation(cloneRoot, hostops.NewNullMessenger(), &hostops.MockGitOps{}, newPreparationTestFileOps(t))
	artifacts, err := prep.Prepare(context.Background(), CloneRequest{
		ID:          "box",
		HostWorkDir: work,
		Username:    "user",
		Uid:         "501",
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(artifacts.PathRegistry.DotfilesDir(), ".claude", ".credentials.json")); !os.IsNotExist(err) {
		t.Fatalf("host Claude credentials were copied, stat err = %v", err)
	}

	configPath := filepath.Join(artifacts.PathRegistry.DotfilesDir(), ".claude.json")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("generated .claude.json missing: %v", err)
	}
	if !json.Valid(config) {
		t.Fatalf("generated .claude.json is not valid JSON: %s", config)
	}

	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatalf("Unmarshal generated .claude.json: %v", err)
	}
	if decoded["hasCompletedOnboarding"] != true {
		t.Fatalf("hasCompletedOnboarding = %v, want true", decoded["hasCompletedOnboarding"])
	}
	if _, ok := decoded["secret"]; ok {
		t.Fatalf("host .claude.json fields were copied: %s", config)
	}
}
