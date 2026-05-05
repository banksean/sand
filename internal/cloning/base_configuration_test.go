package cloning

import (
	"context"
	"errors"
	"io"
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
			commandKey("apk", "--version"): {out: "apk-tools 2.14"},
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
		"exec:apk --version",
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
		"exec:git remote remove origin", "exec:git remote add origin /run/git-origin-ro",
	}

	if !reflect.DeepEqual(exec.calls, wantCalls) {
		t.Fatalf("hook.Run() calls mismatch\n got: %#v\nwant: %#v", exec.calls, wantCalls)
	}
}

func TestDefaultContainerHook_UsesUbuntuFlavorWhenAPKUnavailable(t *testing.T) {
	cfg := NewBaseContainerConfiguration()
	exec := &fakeHookStreamer{
		execResults: map[string]fakeExecResult{
			commandKey("apk", "--version"): {err: errors.New("apk not found")},
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
		"exec:apk --version",
		"exec:groupadd -g 1000 sean",
		"exec:useradd -u 1000 -g sean -s /bin/zsh sean",
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
		"exec:git remote remove origin", "exec:git remote add origin /run/git-origin-ro",
	}

	if !reflect.DeepEqual(exec.calls, wantCalls) {
		t.Fatalf("hook.Run() calls mismatch\n got: %#v\nwant: %#v", exec.calls, wantCalls)
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
