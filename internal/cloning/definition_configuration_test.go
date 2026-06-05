package cloning

import (
	"context"
	"reflect"
	"strings"
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

	exec := &fakeHookStreamer{}
	if err := hooks[1].Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("install hook Run() error = %v", err)
	}
	if len(exec.calls) != 1 || !strings.HasPrefix(exec.calls[0], "stream:sh -c set -eu") {
		t.Fatalf("install hook calls = %#v", exec.calls)
	}
	if !strings.Contains(exec.calls[0], "npm install -g @openai/codex") {
		t.Fatalf("install hook script missing codex install: %q", exec.calls[0])
	}
}

func TestDefinitionContainerConfigurationKeepsOpenCodeTunnelAsRecurringHook(t *testing.T) {
	definition, ok := agentdefs.Lookup("opencode")
	if !ok {
		t.Fatal("missing opencode definition")
	}
	cfg := NewDefinitionContainerConfiguration(definition)

	startHooks := cfg.GetStartHooks(CloneArtifacts{Username: "sean"})
	if got, want := hookNames(startHooks), []string{"open remote ssh tunnel for chrome-devtools mcp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start hook names = %#v, want %#v", got, want)
	}

	firstStartHooks := cfg.GetFirstStartHooks(CloneArtifacts{Username: "sean", Uid: "1000"})
	got := hookNames(firstStartHooks)
	want := []string{"default container bootstrap", "install opencode agent", "open remote ssh tunnel for chrome-devtools mcp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("first-start hook names = %#v, want %#v", got, want)
	}
}

func hookNames(hooks []sandtypes.ContainerHook) []string {
	names := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		names = append(names, hook.Name())
	}
	return names
}
