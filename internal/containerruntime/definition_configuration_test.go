package containerruntime

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

	hooks := cfg.GetFirstStartHooks(Artifacts{Username: "sean", Uid: "1000"})
	gotNames := hookNames(hooks)
	wantNames := []string{"default container bootstrap", "install codex agent"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("hook names = %#v, want %#v", gotNames, wantNames)
	}

	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("which", "codex"): {err: errors.New("codex not found")},
			commandKey("uname", "-m"):    {out: "x86_64\n"},
			commandKey("/opt/sand-agent-cache/node/22.23.1/linux-x64/bin/node", "--version"): {out: "v22.23.1\n"},
		},
	}
	if err := hooks[1].Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("install hook Run() error = %v", err)
	}
	wantCalls := []string{
		"exec:which codex",
		"exec:uname -m",
		"exec:test -d /opt/sand-agent-cache",
		"exec:test -w /opt/sand-agent-cache",
		"exec:test -x /opt/sand-agent-cache/node/22.23.1/linux-x64/bin/node",
		"exec:/opt/sand-agent-cache/node/22.23.1/linux-x64/bin/node --version",
		"exec:mkdir -p /usr/local/lib/sand-npm-agents/codex",
		"stream:env PATH=/opt/sand-agent-cache/node/22.23.1/linux-x64/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin /opt/sand-agent-cache/node/22.23.1/linux-x64/bin/npm install -g --prefix /usr/local/lib/sand-npm-agents/codex @openai/codex@0.145.0",
		"stream-input:tee /usr/local/bin/codex.sand.tmp",
		"exec:chmod 0755 /usr/local/bin/codex.sand.tmp",
		"exec:mv /usr/local/bin/codex.sand.tmp /usr/local/bin/codex",
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

	startHooks := cfg.GetStartHooks(Artifacts{Username: "sean"})
	if got, want := hookNames(startHooks), []string{"start sshd", "open remote ssh tunnel for chrome-devtools mcp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start hook names = %#v, want %#v", got, want)
	}

	firstStartHooks := cfg.GetFirstStartHooks(Artifacts{Username: "sean", Uid: "1000"})
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
	want := "install-opencode-agent opencode 1.18.4\n"
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
