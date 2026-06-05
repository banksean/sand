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
	for _, want := range []string{
		"AGENT=codex",
		"VERSION=0.137.0",
		"CACHE_ROOT=/opt/sand-agent-cache",
		"LOCK_DIR=\"$CACHE_DIR.lock\"",
		"INSTALL_TGZ=\"$CACHE_DIR/install.tgz\"",
		"npm install -g --prefix \"$TMP_DIR/prefix\" @openai/codex@0.137.0",
		"tar -C /usr/local -xzf \"$INSTALL_TGZ\"",
	} {
		if !strings.Contains(exec.calls[0], want) {
			t.Fatalf("install hook script missing %q: %q", want, exec.calls[0])
		}
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

func TestAgentInstallScriptUsesOpenCodeBinaryCache(t *testing.T) {
	definition, ok := agentdefs.Lookup("opencode")
	if !ok {
		t.Fatal("missing opencode definition")
	}
	script, err := agentInstallScript(definition.Name, *definition.Install)
	if err != nil {
		t.Fatalf("agentInstallScript() error = %v", err)
	}
	for _, want := range []string{
		"AGENT=opencode",
		"VERSION=1.14.48",
		"CACHED_BIN=\"$CACHE_DIR/opencode\"",
		"https://opencode.ai/install",
		"cp \"$CACHED_BIN\" /usr/local/bin/opencode",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("opencode install script missing %q: %s", want, script)
		}
	}
}

func TestAgentInstallScriptRejectsUnsafeSpec(t *testing.T) {
	_, err := agentInstallScript("bad agent", agentdefs.InstallSpec{
		Kind:    agentdefs.InstallerNPM,
		Package: "@openai/codex",
		Version: "0.137.0",
		Command: "codex",
	})
	if err == nil {
		t.Fatal("agentInstallScript() error = nil, want unsafe shell token error")
	}
}

func hookNames(hooks []sandtypes.ContainerHook) []string {
	names := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		names = append(names, hook.Name())
	}
	return names
}
