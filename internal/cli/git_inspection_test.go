package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardenedGitEnvScrubsGitEnvironment(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/hostile.git")
	t.Setenv("GIT_CONFIG_GLOBAL", "/tmp/hostile-config")
	t.Setenv("GIT_EXTERNAL_DIFF", "cat")
	t.Setenv("PAGER", "less")

	env := strings.Join(hardenedGitEnv(), "\n")
	for _, forbidden := range []string{
		"GIT_DIR=/tmp/hostile.git",
		"GIT_CONFIG_GLOBAL=/tmp/hostile-config",
		"GIT_EXTERNAL_DIFF=cat",
		"PAGER=less",
	} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("hardened env contains %q:\n%s", forbidden, env)
		}
	}
	for _, want := range []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_EXTERNAL_DIFF=",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("hardened env missing %q:\n%s", want, env)
		}
	}
}

func TestSandboxWorktreeSnapshotIncludesUncommittedWithoutMovingHead(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "committed\n")
	git(t, repo, "add", "tracked.txt")
	git(t, repo, "-c", "user.name=Sand", "-c", "user.email=sand@example.com", "commit", "-q", "-m", "initial")
	before := gitOutput(t, repo, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(repo, "tracked.txt"), "modified\n")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "new\n")
	snapshot := t.TempDir()
	if err := sandboxWorktreeSnapshot(context.Background(), repo, snapshot); err != nil {
		t.Fatalf("sandboxWorktreeSnapshot: %v", err)
	}

	after := gitOutput(t, repo, "rev-parse", "HEAD")
	if after != before {
		t.Fatalf("HEAD changed from %s to %s", before, after)
	}
	if got := readFile(t, filepath.Join(snapshot, "tracked.txt")); got != "modified\n" {
		t.Fatalf("snapshot tracked.txt = %q", got)
	}
	if got := readFile(t, filepath.Join(snapshot, "untracked.txt")); got != "new\n" {
		t.Fatalf("snapshot untracked.txt = %q", got)
	}
}

func TestSandboxHasUncommittedChanges(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "committed\n")
	git(t, repo, "add", "tracked.txt")
	git(t, repo, "-c", "user.name=Sand", "-c", "user.email=sand@example.com", "commit", "-q", "-m", "initial")

	dirty, err := sandboxHasUncommittedChanges(context.Background(), repo)
	if err != nil {
		t.Fatalf("sandboxHasUncommittedChanges clean: %v", err)
	}
	if dirty {
		t.Fatal("clean repo reported dirty")
	}

	writeFile(t, filepath.Join(repo, "tracked.txt"), "modified\n")
	dirty, err = sandboxHasUncommittedChanges(context.Background(), repo)
	if err != nil {
		t.Fatalf("sandboxHasUncommittedChanges modified: %v", err)
	}
	if !dirty {
		t.Fatal("modified repo reported clean")
	}

	git(t, repo, "checkout", "-q", "--", "tracked.txt")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "new\n")
	dirty, err = sandboxHasUncommittedChanges(context.Background(), repo)
	if err != nil {
		t.Fatalf("sandboxHasUncommittedChanges untracked: %v", err)
	}
	if !dirty {
		t.Fatal("untracked file reported clean")
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
