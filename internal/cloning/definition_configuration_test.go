package cloning

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestDefinitionContainerConfigurationAddsInstallHookAfterBaseHook(t *testing.T) {
	definition, ok := agentdefs.Lookup("codex")
	if !ok {
		t.Fatal("missing codex definition")
	}
	cfg := NewDefinitionContainerConfiguration(definition)

	hooks := cfg.GetFirstStartHooks(CloneArtifacts{Username: "sean", Uid: "1000"})
	gotNames := hookNames(hooks)
	wantNames := []string{"default container bootstrap", "install codex agent"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("hook names = %#v, want %#v", gotNames, wantNames)
	}

	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("which", "codex"):   {err: errors.New("codex not found")},
			commandKey("which", "apk"):     {err: errors.New("apk not found")},
			commandKey("which", "apt-get"): {out: "/usr/bin/apt-get"},
		},
	}
	if err := hooks[1].Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("install hook Run() error = %v", err)
	}
	wantCalls := []string{
		"exec:which codex",
		"exec:which apk",
		"exec:which apt-get",
		"stream:apt-get update",
		"stream:apt-get install -y --no-install-recommends nodejs npm",
		"stream:npm install -g @openai/codex@0.137.0",
	}
	if !reflect.DeepEqual(exec.calls, wantCalls) {
		t.Fatalf("install hook calls = %#v, want %#v", exec.calls, wantCalls)
	}
}

func TestDefinitionContainerConfigurationKeepsOpenCodeTunnelAsRecurringHook(t *testing.T) {
	definition, ok := agentdefs.Lookup("opencode")
	if !ok {
		t.Fatal("missing opencode definition")
	}
	cfg := NewDefinitionContainerConfiguration(definition)

	startHooks := cfg.GetStartHooks(CloneArtifacts{Username: "sean"})
	if got, want := hookNames(startHooks), []string{"start sshd", "open remote ssh tunnel for chrome-devtools mcp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start hook names = %#v, want %#v", got, want)
	}

	firstStartHooks := cfg.GetFirstStartHooks(CloneArtifacts{Username: "sean", Uid: "1000"})
	got := hookNames(firstStartHooks)
	want := []string{"default container bootstrap", "install opencode agent", "open remote ssh tunnel for chrome-devtools mcp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("first-start hook names = %#v, want %#v", got, want)
	}
}

func TestAgentInstallHookScriptUsesOpenCodeCommand(t *testing.T) {
	definition, ok := agentdefs.Lookup("opencode")
	if !ok {
		t.Fatal("missing opencode definition")
	}
	script, err := agentInstallHookScript(definition.Name, *definition.Install)
	if err != nil {
		t.Fatalf("agentInstallHookScript() error = %v", err)
	}
	want := "install-opencode-agent opencode 1.14.48\n"
	if script != want {
		t.Fatalf("opencode install script = %q, want %q", script, want)
	}
}

func TestAgentInstallScriptRejectsUnsafeSpec(t *testing.T) {
	_, err := agentInstallHookScript("bad agent", agentdefs.InstallSpec{
		Kind:    agentdefs.InstallerNPM,
		Package: "@openai/codex",
		Version: "0.137.0",
		Command: "codex",
	})
	if err == nil {
		t.Fatal("agentInstallHookScript() error = nil, want unsafe shell token error")
	}
}

func hookNames(hooks []sandtypes.ContainerHook) []string {
	names := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		names = append(names, hook.Name())
	}
	return names
}
