package hookscript

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/sandtypes"
)

type fakeStreamer struct {
	execResults map[string]fakeResult
	calls       []string
	inputs      map[string]string
}

type fakeResult struct {
	out string
	err error
}

func (f *fakeStreamer) Exec(ctx context.Context, cmd string, args ...string) (string, error) {
	key := commandKey(cmd, args...)
	f.calls = append(f.calls, key)
	if res, ok := f.execResults[key]; ok {
		return res.out, res.err
	}
	return "", nil
}

func (f *fakeStreamer) ExecStream(ctx context.Context, stdout, stderr io.Writer, cmd string, args ...string) error {
	key := "stream:" + commandKey(cmd, args...)
	f.calls = append(f.calls, key)
	return nil
}

func (f *fakeStreamer) ExecStreamInput(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, cmd string, args ...string) error {
	key := "stream-input:" + commandKey(cmd, args...)
	f.calls = append(f.calls, key)
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		if f.inputs == nil {
			f.inputs = make(map[string]string)
		}
		f.inputs[key] = string(data)
	}
	return nil
}

func TestExecuteUsesContainerCommandsAndConditions(t *testing.T) {
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"which git":           {out: "/usr/bin/git"},
			"which missing":       {err: errors.New("not found")},
			"test -e /tmp/socket": {out: "ok"},
		},
	}

	err := Execute(context.Background(), exec, "test.txt", `
[cmd:git] exec git status
[!cmd:missing] exec echo missing
[exists:/tmp/socket] exec chmod 666 /tmp/socket
`, io.Discard)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := []string{
		"which git",
		"git status",
		"which missing",
		"echo missing",
		"test -e /tmp/socket",
		"chmod 666 /tmp/socket",
	}
	if !reflect.DeepEqual(exec.calls, want) {
		t.Fatalf("calls = %#v, want %#v", exec.calls, want)
	}
}

func TestExecuteRejectsDefaultHostCommands(t *testing.T) {
	exec := &fakeStreamer{}

	err := Execute(context.Background(), exec, "test.txt", "mkdir host-dir\n", io.Discard)
	if err == nil {
		t.Fatal("Execute() error = nil, want unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Execute() error = %v, want unknown command", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("host command reached streamer: %#v", exec.calls)
	}
}

func TestWriteManagedBazelrcReplacesManagedBlock(t *testing.T) {
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"cat /home/user/.bazelrc": {out: "build --keep=1\n" +
				bazelrcManagedStart + "\n" +
				"build --remote_cache=http://old\n" +
				bazelrcManagedEnd + "\n" +
				"common --keep=2\n"},
		},
	}

	err := Execute(context.Background(), exec, "bazelrc.txt", "write-managed-bazelrc /home/user/.bazelrc http://new-cache:8080\n", io.Discard)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := exec.inputs["stream-input:tee /home/user/.bazelrc.sand.tmp"]
	for _, want := range []string{
		"build --keep=1\n",
		"common --keep=2\n",
		"build --remote_cache=http://new-cache:8080\n",
		"build --experimental_guard_against_concurrent_changes\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("written bazelrc missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "http://old") {
		t.Fatalf("written bazelrc kept old managed block: %q", got)
	}
}

func TestExecuteErrorIncludesScriptLine(t *testing.T) {
	expected := errors.New("git failed")
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"git status": {err: expected},
		},
	}

	err := Execute(context.Background(), exec, "hooks.txt", "exec git status\n", io.Discard)
	if !errors.Is(err, expected) {
		t.Fatalf("Execute() error = %v, want %v", err, expected)
	}
	if !strings.Contains(err.Error(), "hooks.txt:1:") {
		t.Fatalf("Execute() error = %v, want script line", err)
	}
}

func TestWriteHTTPProxyEnvInstallsExistingCAStore(t *testing.T) {
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"test -f /usr/local/share/ca-certificates/sand-http-cache.crt": {out: "ok"},
			"which update-ca-certificates":                                 {out: "/usr/sbin/update-ca-certificates"},
		},
	}

	err := Execute(context.Background(), exec, "http-proxy.txt",
		"write-http-proxy-env http://sand-http-cache.test.local:3128 /usr/local/share/ca-certificates/sand-http-cache.crt\n", io.Discard)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !containsCall(exec.calls, "stream:update-ca-certificates") {
		t.Fatalf("calls = %#v, want update-ca-certificates", exec.calls)
	}
}

func TestWriteHTTPProxyEnvInstallsCAStoreWithAPK(t *testing.T) {
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"test -f /usr/local/share/ca-certificates/sand-http-cache.crt": {out: "ok"},
			"which update-ca-certificates":                                 {err: errors.New("missing")},
			"which apk":                                                    {out: "/sbin/apk"},
		},
	}

	err := Execute(context.Background(), exec, "http-proxy.txt",
		"write-http-proxy-env http://sand-http-cache.test.local:3128 /usr/local/share/ca-certificates/sand-http-cache.crt\n", io.Discard)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{
		"stream:apk --no-check-certificate add --no-cache ca-certificates",
		"stream:update-ca-certificates",
	} {
		if !containsCall(exec.calls, want) {
			t.Fatalf("calls = %#v, want %q", exec.calls, want)
		}
	}
}

func TestWriteHTTPProxyEnvInstallsCAStoreWithAPT(t *testing.T) {
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"test -f /usr/local/share/ca-certificates/sand-http-cache.crt": {out: "ok"},
			"which update-ca-certificates":                                 {err: errors.New("missing")},
			"which apk":                                                    {err: errors.New("missing")},
			"which apt-get":                                                {out: "/usr/bin/apt-get"},
		},
	}

	err := Execute(context.Background(), exec, "http-proxy.txt",
		"write-http-proxy-env http://sand-http-cache.test.local:3128 /usr/local/share/ca-certificates/sand-http-cache.crt\n", io.Discard)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{
		"stream:apt-get -o Acquire::https::Verify-Peer=false -o Acquire::https::Verify-Host=false update",
		"stream:apt-get -o Acquire::https::Verify-Peer=false -o Acquire::https::Verify-Host=false install -y --no-install-recommends ca-certificates",
		"stream:update-ca-certificates",
	} {
		if !containsCall(exec.calls, want) {
			t.Fatalf("calls = %#v, want %q", exec.calls, want)
		}
	}
}

func TestWriteHTTPProxyEnvFailsWithoutCAStoreSupport(t *testing.T) {
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"test -f /usr/local/share/ca-certificates/sand-http-cache.crt": {out: "ok"},
			"which update-ca-certificates":                                 {err: errors.New("missing")},
			"which apk":                                                    {err: errors.New("missing")},
			"which apt-get":                                                {err: errors.New("missing")},
		},
	}

	err := Execute(context.Background(), exec, "http-proxy.txt",
		"write-http-proxy-env http://sand-http-cache.test.local:3128 /usr/local/share/ca-certificates/sand-http-cache.crt\n", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "requires ca-certificates support") {
		t.Fatalf("Execute() error = %v, want ca-certificates support error", err)
	}
}

func TestInstallNPMAgentUsesCachedPinnedNodeAndWritesWrapper(t *testing.T) {
	const nodeDir = "/opt/sand-agent-cache/node/22.23.1/linux-x64"
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"which claude":                  {err: errors.New("missing")},
			"uname -m":                      {out: "x86_64\n"},
			nodeDir + "/bin/node --version": {out: "v22.23.1\n"},
			"head -c 4 /usr/local/lib/sand-npm-agents/claude/bin/claude": {out: "\x7fELF"},
		},
	}

	err := installNPMAgent(context.Background(), exec, "claude", "@anthropic-ai/claude-code", "2.1.217")
	if err != nil {
		t.Fatalf("installNPMAgent() error = %v", err)
	}

	npmCall := "stream:env PATH=" + nodeDir + "/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin " +
		"NODE_EXTRA_CA_CERTS=/usr/local/share/ca-certificates/sand-http-cache.crt " + nodeDir +
		"/bin/npm install -g --prefix /usr/local/lib/sand-npm-agents/claude @anthropic-ai/claude-code@2.1.217"
	if !containsCall(exec.calls, npmCall) {
		t.Fatalf("calls = %#v, want %q", exec.calls, npmCall)
	}
	wrapper := exec.inputs["stream-input:tee /usr/local/bin/claude.sand.tmp"]
	wantWrapper := "#!/bin/sh\nexport NODE_EXTRA_CA_CERTS=/usr/local/share/ca-certificates/sand-http-cache.crt\n" +
		"exec /usr/local/lib/sand-npm-agents/claude/bin/claude \"$@\"\n"
	if wrapper != wantWrapper {
		t.Fatalf("wrapper = %q, want %q", wrapper, wantWrapper)
	}
	for _, call := range exec.calls {
		if strings.Contains(call, "apt-get") || strings.Contains(call, "apk add") {
			t.Fatalf("installer used distribution Node packages: %q", call)
		}
	}
}

func TestInstallNPMAgentOmitsExtraCAWhenProxyCertificateIsAbsent(t *testing.T) {
	const nodeDir = "/opt/sand-agent-cache/node/22.23.1/linux-x64"
	exec := &fakeStreamer{execResults: map[string]fakeResult{
		"which codex":                   {err: errors.New("missing")},
		"uname -m":                      {out: "x86_64"},
		nodeDir + "/bin/node --version": {out: "v22.23.1"},
		"test -f " + sandtypes.HTTPProxyCACertContainerPath: {err: errors.New("missing")},
	}}

	if err := installNPMAgent(context.Background(), exec, "codex", "@openai/codex", "0.145.0"); err != nil {
		t.Fatalf("installNPMAgent() error = %v", err)
	}
	wrapper := exec.inputs["stream-input:tee /usr/local/bin/codex.sand.tmp"]
	if strings.Contains(wrapper, "NODE_EXTRA_CA_CERTS") {
		t.Fatalf("wrapper unexpectedly configures a missing proxy CA: %q", wrapper)
	}
	wantExec := "exec " + nodeDir + "/bin/node /usr/local/lib/sand-npm-agents/codex/bin/codex \"$@\""
	if !strings.Contains(wrapper, wantExec) {
		t.Fatalf("JavaScript agent wrapper = %q, want %q", wrapper, wantExec)
	}
}

func TestEnsureNPMAgentNodeDownloadsAndVerifiesArchive(t *testing.T) {
	const (
		checksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		archive  = "node-v22.23.1-linux-arm64.tar.gz"
		stage    = "/tmp/node-download/runtime"
	)
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"uname -m": {out: "aarch64\n"},
			"test -x /opt/sand-agent-cache/node/22.23.1/linux-arm64/bin/node": {err: errors.New("missing")},
			"test -e /opt/sand-agent-cache/node/22.23.1/linux-arm64":          {err: errors.New("missing")},
			"mktemp -d": {out: "/tmp/node-download\n"},
			"curl -fsSL https://nodejs.org/download/release/v22.23.1/SHASUMS256.txt": {out: checksum + "  " + archive + "\n"},
			"sha256sum /tmp/node-download/" + archive:                                {out: checksum + "  /tmp/node-download/" + archive + "\n"},
			stage + "/bin/node --version":                                            {out: "v22.23.1\n"},
		},
	}

	got, err := ensureNPMAgentNode(context.Background(), exec)
	if err != nil {
		t.Fatalf("ensureNPMAgentNode() error = %v", err)
	}
	want := "/opt/sand-agent-cache/node/22.23.1/linux-arm64"
	if got != want {
		t.Fatalf("ensureNPMAgentNode() = %q, want %q", got, want)
	}
	for _, call := range []string{
		"stream:curl -fsSL -o /tmp/node-download/" + archive + " https://nodejs.org/download/release/v22.23.1/" + archive,
		"stream:tar -xzf /tmp/node-download/" + archive + " -C " + stage + " --strip-components=1",
		"mv " + stage + " " + want,
	} {
		if !containsCall(exec.calls, call) {
			t.Fatalf("calls = %#v, want %q", exec.calls, call)
		}
	}
}

func TestEnsureNPMAgentNodeRejectsChecksumMismatch(t *testing.T) {
	const archive = "node-v22.23.1-linux-x64.tar.gz"
	exec := &fakeStreamer{
		execResults: map[string]fakeResult{
			"uname -m": {out: "x86_64"},
			"test -x /opt/sand-agent-cache/node/22.23.1/linux-x64/bin/node": {err: errors.New("missing")},
			"test -e /opt/sand-agent-cache/node/22.23.1/linux-x64":          {err: errors.New("missing")},
			"mktemp -d": {out: "/tmp/node-download"},
			"curl -fsSL https://nodejs.org/download/release/v22.23.1/SHASUMS256.txt": {out: strings.Repeat("a", 64) + "  " + archive},
			"sha256sum /tmp/node-download/" + archive:                                {out: strings.Repeat("b", 64) + "  archive"},
		},
	}

	_, err := ensureNPMAgentNode(context.Background(), exec)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("ensureNPMAgentNode() error = %v, want checksum mismatch", err)
	}
}

func TestNodeArchiveArchRejectsUnsupportedArchitecture(t *testing.T) {
	_, err := nodeArchiveArch("riscv64")
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("nodeArchiveArch() error = %v, want unavailable", err)
	}
}

func TestAgentCacheRootFallsBackWhenSharedCacheIsUnavailable(t *testing.T) {
	exec := &fakeStreamer{execResults: map[string]fakeResult{
		"test -d /opt/sand-agent-cache": {err: errors.New("missing")},
	}}
	if got := agentCacheRoot(context.Background(), exec); got != "/tmp/sand-agent-cache" {
		t.Fatalf("agentCacheRoot() = %q, want /tmp/sand-agent-cache", got)
	}
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func commandKey(cmd string, args ...string) string {
	return strings.Join(append([]string{cmd}, args...), " ")
}
