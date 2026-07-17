package cloning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/hostops"
)

func TestCodexWorkspacePreparationWritesTelemetryConfigOnly(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	work := filepath.Join(dir, "work")
	cloneRoot := filepath.Join(dir, "clones")

	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o750); err != nil {
		t.Fatalf("MkdirAll .codex: %v", err)
	}
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatalf("MkdirAll work: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"secret":"token"}`), 0o600); err != nil {
		t.Fatalf("WriteFile auth.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"host-state\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile config.toml: %v", err)
	}
	t.Setenv("HOME", home)

	definition, ok := agentdefs.Lookup("codex")
	if !ok {
		t.Fatal("missing codex definition")
	}
	prep := NewDefinitionWorkspacePreparation(definition, cloneRoot, hostops.NewNullMessenger(), &hostops.MockGitOps{}, newPreparationTestFileOps(t))
	artifacts, err := prep.Prepare(context.Background(), CloneRequest{
		ID:          "box",
		HostWorkDir: work,
		Username:    "user",
		Uid:         "501",
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	codexDir := filepath.Join(artifacts.PathRegistry.DotfilesDir(), ".codex")
	if _, err := os.Stat(filepath.Join(codexDir, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("host Codex auth was copied, stat err = %v", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("generated Codex config missing: %v", err)
	}
	configText := string(config)
	for _, want := range []string{
		"[otel]",
		"exporter = { otlp-http = {",
		`endpoint = "http://otel-collector.dev.local:4318/v1/logs"`,
		"trace_exporter = { otlp-http = {",
		`endpoint = "http://otel-collector.dev.local:4318/v1/traces"`,
		`protocol = "binary"`,
	} {
		if !strings.Contains(configText, want) {
			t.Fatalf("generated Codex config missing %q:\n%s", want, configText)
		}
	}
	if strings.Contains(configText, "host-state") {
		t.Fatalf("host Codex config fields were copied: %s", configText)
	}
}
