//go:build darwin && destructive_smoke

package smoketest

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rsc.io/script"
)

const (
	destructiveSmokeGate = "SAND_DESTRUCTIVE_SMOKE_TEST"
	vscSmokeGate         = "SAND_DESTRUCTIVE_SMOKE_VSC"
	smokeTimeout         = 2 * time.Hour
)

func TestDestructiveSmoke(t *testing.T) {
	if os.Getenv(destructiveSmokeGate) != "1" {
		t.Skipf("set %s=1 to run this destructive smoke test", destructiveSmokeGate)
	}

	repoRoot := findRepoRoot(t)
	preflight(t)
	containerDNSDomain := containerDNSDomain(t)

	t.Cleanup(func() {
		cleanupSmokeState(t.Logf)
	})
	cleanupSmokeState(t.Logf)

	ctx, cancel := context.WithTimeout(context.Background(), smokeTimeout)
	defer cancel()

	state, err := script.NewState(ctx, repoRoot, os.Environ())
	if err != nil {
		t.Fatalf("create script state: %v", err)
	}
	defer func() {
		if err := state.CloseAndWait(io.Discard); err != nil {
			t.Logf("close script state: %v", err)
		}
	}()
	for key, value := range map[string]string{
		"CONTAINER_DNS_DOMAIN": containerDNSDomain,
		"SMOKE_TEST":           "TRUE",
	} {
		if err := state.Setenv(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	engine := script.NewEngine()
	var log bytes.Buffer
	body := destructiveSmokeScript(os.Getenv(vscSmokeGate) == "1")
	err = engine.Execute(state, "destructive-smoke.txt", bufio.NewReader(strings.NewReader(body)), io.MultiWriter(&log, testLogWriter{t: t}))
	if err != nil {
		t.Logf("script log:\n%s", log.String())
		dumpSmokeLogs(t)
		t.Fatalf("destructive smoke test failed: %v", err)
	}
}

func destructiveSmokeScript(includeVSC bool) string {
	var b strings.Builder
	b.WriteString(`
# Install sand and sandd from source.
exec task install

# Build base:local image used by the sandboxes.
exec make -C images base

# Confirm the newly installed commands are on PATH.
exec which sand
exec which sandd

# Basic host CLI commands should work.
exec sand --version
exec sand build-info
exec sand ls
exec sand config ls

# Create the first sandbox and verify it appears in listings.
exec zsh -lc 'printf "exit\n" | script -q /dev/null sand new -i base:local --tmux=false --atch=false --caches-mise --caches-apk --ssh-agent smoke'
exec sand ls
stdout smoke

# Exercise command execution in the sandbox.
exec sand exec smoke ls
exec sand exec smoke whoami
exec sand exec smoke zsh -c 'go test ./...'

# Create a second sandbox to exercise shared Go module and build caches.
exec zsh -lc 'printf "exit\n" | script -q /dev/null sand new -i base:local --tmux=false --caches-mise --caches-apk --ssh-agent smoke-2'
exec sand exec smoke-2 zsh -c 'go test ./...'

# Use the packaged sand innie binary from the base image.
exec sand exec smoke sand --version
exec sand exec smoke sand build-info

# Build and use the sand innie binary from this checkout.
exec sand exec smoke zsh -c 'go build ./cmd/...'
exec sand exec smoke ./sand --version
exec sand exec smoke ./sand build-info
`)
	if includeVSC {
		b.WriteString(`
# Launch VS Code only when explicitly requested.
exec sand vsc smoke
`)
	}
	b.WriteString(`
# SSH should work without TOFU prompts or warnings.
exec ssh -vvv smoke.$CONTAINER_DNS_DOMAIN whoami

# A stopped sandbox should restart automatically when shelling into it.
exec sand stop smoke
exec zsh -lc 'printf "exit\n" | script -q /dev/null sand shell smoke'

# Print logs before cleanup.
exec sand log smoke
exec sand log smoke-2
`)
	return strings.TrimSpace(b.String()) + "\n"
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func preflight(t *testing.T) {
	t.Helper()
	for _, name := range []string{"container", "task", "make", "script", "ssh", "tmq", "zsh"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("required command %q is not on PATH: %v", name, err)
		}
	}
}

func containerDNSDomain(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("zsh", "-lc", "container system property ls | tmq '.dns.domain'").CombinedOutput()
	if err != nil {
		t.Fatalf("read container system dns domain: %v\n%s", err, out)
	}
	domain := strings.TrimSpace(string(out))
	if domain == "" {
		t.Fatal("container system dns domain is empty")
	}
	return domain
}

func cleanupSmokeState(logf func(string, ...any)) {
	if _, err := exec.LookPath("sand"); err == nil {
		bestEffort(logf, "sand", "rm", "-af")
	}
	if _, err := exec.LookPath("sandd"); err == nil {
		bestEffort(logf, "sandd", "stop")
	}
	removeSandInstallation(logf)

	if home, err := os.UserHomeDir(); err == nil {
		bestEffortRemoveAll(logf, filepath.Join(home, ".config", "sand"))
		appSupport := filepath.Join(home, "Library", "Application Support", "Sand")
		if _, err := os.Stat(appSupport); err == nil {
			bestEffort(logf, "chmod", "-R", "u+w", appSupport)
		}
		bestEffortRemoveAll(logf, appSupport)
	}
	bestEffortRemoveAll(logf, "/tmp/sand")
}

func removeSandInstallation(logf func(string, ...any)) {
	if _, err := exec.LookPath("brew"); err == nil && commandSucceeds("brew", "list", "banksean/tap/sand") {
		bestEffort(logf, "brew", "uninstall", "banksean/tap/sand")
		return
	}
	for _, name := range []string{"sand", "sandd"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			logf("remove %s: %v", path, err)
		}
	}
}

func commandSucceeds(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func bestEffort(logf func(string, ...any), name string, args ...string) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func bestEffortRemoveAll(logf func(string, ...any), path string) {
	if err := os.RemoveAll(path); err != nil {
		logf("remove %s: %v", path, err)
	}
}

func dumpSmokeLogs(t *testing.T) {
	t.Helper()
	for _, sandbox := range []string{"smoke", "smoke-2"} {
		out, err := exec.Command("sand", "log", sandbox).CombinedOutput()
		if err != nil {
			t.Logf("sand log %s failed: %v\n%s", sandbox, err, out)
			continue
		}
		t.Logf("sand log %s:\n%s", sandbox, out)
	}
}

type testLogWriter struct {
	t *testing.T
}

func (w testLogWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			w.t.Log(line)
		}
	}
	return len(p), nil
}
