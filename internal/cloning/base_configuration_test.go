package cloning

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/sandtypes"
)

type fakeExecResult struct {
	out string
	err error
}

type fakeHookStreamer struct {
	execResults   map[string]fakeExecResult
	streamResults map[string]fakeExecResult
	calls         []string
	streamInputs  map[string]string
}

func (f *fakeHookStreamer) Exec(ctx context.Context, shellCmd string, args ...string) (string, error) {
	f.calls = append(f.calls, "exec:"+renderCommand(shellCmd, args...))
	if res, ok := f.execResults[commandKey(shellCmd, args...)]; ok {
		return res.out, res.err
	}
	return "", nil
}

func (f *fakeHookStreamer) ExecStream(ctx context.Context, stdout, stderr io.Writer, shellCmd string, args ...string) error {
	f.calls = append(f.calls, "stream:"+renderCommand(shellCmd, args...))
	if res, ok := f.streamResults[commandKey(shellCmd, args...)]; ok {
		if res.out != "" {
			_, _ = io.WriteString(stdout, res.out)
		}
		return res.err
	}
	return nil
}

func (f *fakeHookStreamer) ExecStreamInput(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, shellCmd string, args ...string) error {
	f.calls = append(f.calls, "stream-input:"+renderCommand(shellCmd, args...))
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		if f.streamInputs == nil {
			f.streamInputs = make(map[string]string)
		}
		f.streamInputs[commandKey(shellCmd, args...)] = string(data)
	}
	if res, ok := f.streamResults[commandKey(shellCmd, args...)]; ok {
		if res.out != "" {
			_, _ = io.WriteString(stdout, res.out)
		}
		return res.err
	}
	return nil
}

func commandKey(shellCmd string, args ...string) string {
	return strings.Join(append([]string{shellCmd}, args...), "\x00")
}

func renderCommand(shellCmd string, args ...string) string {
	return strings.Join(append([]string{shellCmd}, args...), " ")
}

func TestDefaultContainerHook_UsesAlpineFlavorWhenAPKAvailable(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("which", "apk"):     {out: "apk-tools 2.14"},
			commandKey("stat", "/etc/apk"): {out: "ok"},
			commandKey("which", "mise.sh"): {out: "/usr/local/bin/mise.sh"},
		},
		streamResults: map[string]fakeExecResult{
			commandKey("mise.sh"): {out: "mise ok"},
		},
	}

	hook := cfg.defaultContainerHook("sean", "1000", sandtypes.SharedCacheMounts{
		MiseCacheHostDir: "/host/mise",
		APKCacheHostDir:  "/host/apk",
	})

	if err := hook.Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("hook.Run() error = %v", err)
	}

	wantCalls := []string{
		"exec:which apk",
		"exec:addgroup -g 1000 sean",
		"exec:adduser -u 1000 -D -G sean -s /bin/zsh sean",
		"exec:passwd -u sean",
		"exec:addgroup sean wheel",
		"exec:cp -r /dotfiles/. /home/sean/.",
		"exec:cp -r /root/.ssh /home/sean/.ssh",
		"exec:mkdir -p /home/sean/go/pkg",
		"exec:mkdir -p /home/sean/.cache",
		"exec:chown -R sean:sean /home/sean",
		"exec:stat /etc/apk",
		"exec:ln -s /var/cache/apk /etc/apk/cache",
		"exec:ln -sfn /opt/tool-cache/mise/go/mod /home/sean/go/pkg/mod",
		"exec:ln -sfn /opt/tool-cache/mise/go/build /home/sean/.cache/go-build",
		"exec:cp -r /sshkeys/. /etc/ssh/.",
		"exec:chmod 600 /etc/ssh/ssh_host_key /etc/ssh/ssh_host_key.pub /etc/ssh/ssh_host_key.pub-cert /etc/ssh/user_ca.pub",
		"exec:/usr/sbin/sshd -f /etc/ssh/sshd_config",
		"exec:which mise.sh",
		"stream:mise.sh",
		"exec:git remote remove origin",
		"exec:git remote add origin /run/git-origin-ro",
		"exec:git remote set-url --push origin DISABLED",
	}

	if !reflect.DeepEqual(exec.calls, wantCalls) {
		t.Fatalf("hook.Run() calls mismatch\n got: %#v\nwant: %#v", exec.calls, wantCalls)
	}
}

func TestBaseContainerConfigurationMountsGitMirrorAsOrigin(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	mounts := cfg.GetMounts(CloneArtifacts{
		HostGitMirrorDir: "/host/mirrors/repo.git",
		SandboxWorkDir:   "/host/sandboxes/one",
		PathRegistry:     NewStandardPathRegistry("/host/sandboxes/one"),
	})

	var found bool
	for _, mount := range mounts {
		if mount.Target == ContainerSideGitOrigin {
			found = true
			if mount.Source != "/host/mirrors/repo.git" {
				t.Fatalf("origin mount source = %q, want mirror", mount.Source)
			}
			if !mount.ReadOnly {
				t.Fatal("origin mirror mount is not read-only")
			}
		}
	}
	if !found {
		t.Fatalf("missing %s mount in %#v", ContainerSideGitOrigin, mounts)
	}
}

func TestBaseContainerConfigurationSkipsOriginMountWithoutGitMirror(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	mounts := cfg.GetMounts(CloneArtifacts{
		SandboxWorkDir: "/host/sandboxes/one",
		PathRegistry:   NewStandardPathRegistry("/host/sandboxes/one"),
	})

	for _, mount := range mounts {
		if mount.Target == ContainerSideGitOrigin {
			t.Fatalf("unexpected origin mount without mirror: %#v", mount)
		}
	}
}

func TestBaseContainerConfigurationMountsAgentCache(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	mounts := cfg.GetMounts(CloneArtifacts{
		SandboxWorkDir: "/host/sandboxes/one",
		PathRegistry:   NewStandardPathRegistry("/host/sandboxes/one"),
		SharedCacheMounts: sandtypes.SharedCacheMounts{
			AgentCacheHostDir: "/host/caches/agents",
		},
	})

	var found bool
	for _, mount := range mounts {
		if mount.Target == agentCachePath {
			found = true
			if mount.Source != "/host/caches/agents" {
				t.Fatalf("agent cache source = %q", mount.Source)
			}
			if mount.ReadOnly {
				t.Fatal("agent cache mount is read-only")
			}
		}
	}
	if !found {
		t.Fatalf("missing %s mount in %#v", agentCachePath, mounts)
	}
}

func TestStartHook_PreparesSSHDForUbuntuFlavor(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("which", "apk"): {err: errors.New("apk not found")},
		},
	}

	hooks := cfg.GetStartHooks(CloneArtifacts{Uid: "1000"})
	if len(hooks) != 1 {
		t.Fatalf("GetStartHooks() len = %d, want 1", len(hooks))
	}
	if err := hooks[0].Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("start hook error = %v", err)
	}

	wantCalls := []string{
		"exec:which apk",
		"exec:mkdir -p /run/sshd",
		"exec:/usr/sbin/sshd -f /etc/ssh/sshd_config",
	}
	if !reflect.DeepEqual(exec.calls, wantCalls) {
		t.Fatalf("start hook calls mismatch\n got: %#v\nwant: %#v", exec.calls, wantCalls)
	}
}

func TestStartHook_SkipsSSHDPrepareForAlpineFlavor(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("which", "apk"): {out: "apk-tools 2.14"},
		},
	}

	hooks := cfg.GetStartHooks(CloneArtifacts{Uid: "1000"})
	if len(hooks) != 1 {
		t.Fatalf("GetStartHooks() len = %d, want 1", len(hooks))
	}
	if err := hooks[0].Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("start hook error = %v", err)
	}

	wantCalls := []string{
		"exec:which apk",
		"exec:/usr/sbin/sshd -f /etc/ssh/sshd_config",
	}
	if !reflect.DeepEqual(exec.calls, wantCalls) {
		t.Fatalf("start hook calls mismatch\n got: %#v\nwant: %#v", exec.calls, wantCalls)
	}
}

func TestDefaultContainerHook_UsesUbuntuFlavorWhenAPKUnavailable(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("which", "apk"):     {err: errors.New("apk not found")},
			commandKey("which", "mise.sh"): {out: "/usr/local/bin/mise.sh"},
		},
		streamResults: map[string]fakeExecResult{
			commandKey("mise.sh"): {out: "mise ok"},
		},
	}

	hook := cfg.defaultContainerHook("sean", "1000", sandtypes.SharedCacheMounts{
		MiseCacheHostDir: "/host/mise",
	})

	if err := hook.Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("hook.Run() error = %v", err)
	}

	wantCalls := []string{
		"exec:which apk",
		"exec:groupadd -g 1000 sean",
		"exec:useradd -u 1000 -g sean -s /bin/zsh sean",
		"exec:passwd -d sean",
		"exec:usermod -a -G sudo sean",
		"exec:mkdir -p /home/sean",
		"exec:cp -r /dotfiles/. /home/sean/.",
		"exec:cp -r /root/.ssh /home/sean/.ssh",
		"exec:mkdir -p /home/sean/go/pkg",
		"exec:mkdir -p /home/sean/.cache",
		"exec:chown -R sean:sean /home/sean",
		"exec:ln -sfn /opt/tool-cache/mise/go/mod /home/sean/go/pkg/mod",
		"exec:ln -sfn /opt/tool-cache/mise/go/build /home/sean/.cache/go-build",
		"exec:cp -r /sshkeys/. /etc/ssh/.",
		"exec:chmod 600 /etc/ssh/ssh_host_key /etc/ssh/ssh_host_key.pub /etc/ssh/ssh_host_key.pub-cert /etc/ssh/user_ca.pub",
		"exec:mkdir -p /run/sshd",
		"exec:/usr/sbin/sshd -f /etc/ssh/sshd_config",
		"exec:which mise.sh",
		"stream:mise.sh",
		"exec:git remote remove origin",
		"exec:git remote add origin /run/git-origin-ro",
		"exec:git remote set-url --push origin DISABLED",
	}

	if !reflect.DeepEqual(exec.calls, wantCalls) {
		t.Fatalf("hook.Run() calls mismatch\n got: %#v\nwant: %#v", exec.calls, wantCalls)
	}
}

func TestDefaultContainerHook_ConfiguresBazelRemoteCacheWhenEnabled(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("which", "apk"):     {err: errors.New("apk not found")},
			commandKey("which", "mise.sh"): {out: "/usr/local/bin/mise.sh"},
		},
		streamResults: map[string]fakeExecResult{
			commandKey("mise.sh"): {out: "mise ok"},
		},
	}

	hook := cfg.defaultContainerHook("sean", "1000", sandtypes.SharedCacheMounts{
		BazelRemoteCacheURL: "http://sand-bazel-cache.test.local:8080",
	})

	if err := hook.Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("hook.Run() error = %v", err)
	}

	for _, call := range exec.calls {
		if strings.Contains(call, "/app/.bazelrc") {
			t.Fatalf("hook touched project bazelrc: %s", call)
		}
	}
	root := exec.streamInputs[commandKey("tee", "/root/.bazelrc.sand.tmp")]
	if !strings.Contains(root, "build --remote_cache=http://sand-bazel-cache.test.local:8080") {
		t.Fatalf("root .bazelrc content missing remote cache: %q", root)
	}
	user := exec.streamInputs[commandKey("tee", "/home/sean/.bazelrc.sand.tmp")]
	if !strings.Contains(user, "build --experimental_guard_against_concurrent_changes") {
		t.Fatalf("user .bazelrc content missing guard setting: %q", user)
	}
}

func TestDefaultContainerHook_ConfiguresSharedHTTPProxyWhenEnabled(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("which", "apk"):     {err: errors.New("apk not found")},
			commandKey("which", "mise.sh"): {out: "/usr/local/bin/mise.sh"},
		},
		streamResults: map[string]fakeExecResult{
			commandKey("mise.sh"): {out: "mise ok"},
		},
	}

	hook := cfg.defaultContainerHook("sean", "1000", sandtypes.SharedCacheMounts{
		HTTPProxyURL: "http://sand-http-cache.test.local:3142",
	})

	if err := hook.Run(context.Background(), nil, exec); err != nil {
		t.Fatalf("hook.Run() error = %v", err)
	}

	var proxyConfigured bool
	for _, call := range exec.calls {
		if !strings.Contains(call, "sand-http-cache.sh") {
			continue
		}
		proxyConfigured = true
		for _, want := range []string{
			"profile=/etc/profile.d/sand-http-cache.sh",
			"envfile=/etc/environment",
			"export http_proxy='http://sand-http-cache.test.local:3142'",
			"export HTTP_PROXY='http://sand-http-cache.test.local:3142'",
			"export https_proxy='http://sand-http-cache.test.local:3142'",
			"export HTTPS_PROXY='http://sand-http-cache.test.local:3142'",
			"http_proxy=http://sand-http-cache.test.local:3142",
			"HTTP_PROXY=http://sand-http-cache.test.local:3142",
			"https_proxy=http://sand-http-cache.test.local:3142",
			"HTTPS_PROXY=http://sand-http-cache.test.local:3142",
			"no_proxy=localhost,127.0.0.1,::1,.local,.test.local",
			"NO_PROXY=localhost,127.0.0.1,::1,.local,.test.local",
		} {
			if !strings.Contains(call, want) {
				t.Fatalf("shared HTTP proxy script missing %q in call:\n%s", want, call)
			}
		}
	}
	if !proxyConfigured {
		t.Fatalf("shared HTTP proxy was not configured; calls: %#v", exec.calls)
	}
}

func TestBaseMiseScriptPersistsMiseEnvironment(t *testing.T) {
	script, err := os.ReadFile("../../images/base/mise.sh")
	if err != nil {
		t.Fatalf("ReadFile(mise.sh): %v", err)
	}
	body := string(script)

	for _, want := range []string{
		`echo "export MISE_DATA_DIR=\"$MISE_DATA_DIR\""`,
		`echo "export MISE_CONFIG_DIR=\"$MISE_CONFIG_DIR\""`,
		`echo "export MISE_CACHE_DIR=\"$MISE_CACHE_DIR\""`,
		`echo "export MISE_STATE_DIR=\"$MISE_STATE_DIR\""`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("mise.sh missing shared env export %q", want)
		}
	}
}

func TestRunDefaultContainerHook_JoinsStepErrors(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	addGroupErr := errors.New("addgroup failed")
	copyDotfilesErr := errors.New("cp failed")
	miseErr := errors.New("mise failed")

	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("addgroup", "-g", "1000", "sean"):          {err: addGroupErr},
			commandKey("cp", "-r", "/dotfiles/.", "/home/sean/."): {err: copyDotfilesErr},
			commandKey("which", "mise.sh"):                        {out: "/usr/local/bin/mise.sh"},
		},
		streamResults: map[string]fakeExecResult{
			commandKey("mise.sh"): {out: "mise startup failed", err: miseErr},
		},
	}

	err := cfg.runDefaultContainerHook(context.Background(), nil, exec, alpineBootstrapFlavor, "sean", "1000", sandtypes.SharedCacheMounts{
		MiseCacheHostDir: "/host/mise",
	})
	if err == nil {
		t.Fatal("runDefaultContainerHook() error = nil, want joined error")
	}
	if !errors.Is(err, addGroupErr) {
		t.Fatalf("runDefaultContainerHook() missing addgroup error: %v", err)
	}
	if !errors.Is(err, copyDotfilesErr) {
		t.Fatalf("runDefaultContainerHook() missing copy dotfiles error: %v", err)
	}
	if !errors.Is(err, miseErr) {
		t.Fatalf("runDefaultContainerHook() missing mise.sh error: %v", err)
	}
}
