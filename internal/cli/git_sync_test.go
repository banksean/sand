package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/daemon/daemontest"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestSyncCmdCreatesHostBranchAndPullsSandboxBranch(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, hostDir)

	if err := (&SyncCmd{SandboxName: "box"}).Run(cctx); err != nil {
		t.Fatalf("SyncCmd.Run() error = %v", err)
	}

	if got := gitOutput(t, hostDir, "branch", "--show-current"); got != "box" {
		t.Fatalf("current branch = %q, want box", got)
	}
	if got := readFile(t, filepath.Join(hostDir, "tracked.txt")); got != "sandbox box\n" {
		t.Fatalf("tracked.txt = %q", got)
	}
	if got := gitOutput(t, hostDir, "config", "--get", "branch.box.remote"); got != "sand/box" {
		t.Fatalf("branch.box.remote = %q", got)
	}
	if got := gitOutput(t, hostDir, "config", "--get", "branch.box.merge"); got != "refs/heads/box" {
		t.Fatalf("branch.box.merge = %q", got)
	}
}

func TestSyncCmdSupportsDifferentHostAndSandboxBranches(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "sandbox-name")
	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, hostDir)

	cmd := &SyncCmd{SandboxName: "box", HostBranch: "host-name", SandboxBranch: "sandbox-name"}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("SyncCmd.Run() error = %v", err)
	}

	if got := gitOutput(t, hostDir, "branch", "--show-current"); got != "host-name" {
		t.Fatalf("current branch = %q, want host-name", got)
	}
	if got := readFile(t, filepath.Join(hostDir, "tracked.txt")); got != "sandbox sandbox-name\n" {
		t.Fatalf("tracked.txt = %q", got)
	}
	if got := gitOutput(t, hostDir, "config", "--get", "branch.host-name.merge"); got != "refs/heads/sandbox-name" {
		t.Fatalf("branch.host-name.merge = %q", got)
	}
}

func TestSyncCmdPullsExistingRelatedBranch(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	sandboxAppDir := filepath.Join(sandboxWorkDir, "app")
	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, hostDir)

	if err := (&SyncCmd{SandboxName: "box"}).Run(cctx); err != nil {
		t.Fatalf("initial SyncCmd.Run() error = %v", err)
	}

	writeFile(t, filepath.Join(sandboxAppDir, "tracked.txt"), "sandbox updated\n")
	git(t, sandboxAppDir, "add", "tracked.txt")
	git(t, sandboxAppDir, "-c", "user.name=Sand", "-c", "user.email=sand@example.com", "commit", "-q", "-m", "sandbox update")

	if err := (&SyncCmd{SandboxName: "box"}).Run(cctx); err != nil {
		t.Fatalf("second SyncCmd.Run() error = %v", err)
	}
	if got := readFile(t, filepath.Join(hostDir, "tracked.txt")); got != "sandbox updated\n" {
		t.Fatalf("tracked.txt = %q", got)
	}
}

func TestSyncCmdDoesNotUseRemoteUploadPackConfig(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	marker := filepath.Join(t.TempDir(), "uploadpack-ran")
	git(t, hostDir, "config", "remote.sand/box.uploadpack", "sh -c 'touch "+marker+"; exit 1'")
	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, hostDir)

	if err := (&SyncCmd{SandboxName: "box"}).Run(cctx); err != nil {
		t.Fatalf("SyncCmd.Run() error = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("remote uploadpack config appears to have run; stat err = %v", err)
	}
}

func TestSyncCmdBlocksURLRewriteToExtTransport(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	sandboxAppDir := filepath.Join(sandboxWorkDir, "app")
	sandboxFetchPath, err := syncCanonicalPath(sandboxAppDir)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "instead-of-ran")
	git(t, hostDir, "config", "url.ext::touch "+marker+";.insteadOf", sandboxFetchPath)
	git(t, hostDir, "config", "protocol.ext.allow", "always")
	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, hostDir)

	err = (&SyncCmd{SandboxName: "box"}).Run(cctx)
	if err == nil {
		t.Fatal("expected URL rewrite to disallowed ext transport to fail closed")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("url insteadOf ext transport appears to have run; stat err = %v", statErr)
	}
}

func TestSyncCmdRejectsExistingUnrelatedBranch(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, hostDir)
	git(t, hostDir, "switch", "-q", "-c", "box")
	git(t, hostDir, "switch", "-q", "main")

	err := (&SyncCmd{SandboxName: "box"}).Run(cctx)
	if err == nil {
		t.Fatal("expected error for existing unrelated host branch")
	}
	if !strings.Contains(err.Error(), "already exists but does not track sand/box/box") {
		t.Fatalf("error = %v", err)
	}
	if got := gitOutput(t, hostDir, "branch", "--show-current"); got != "main" {
		t.Fatalf("current branch = %q, want main", got)
	}
}

func TestSyncCmdRejectsMissingSandboxBranch(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, hostDir)

	err := (&SyncCmd{SandboxName: "box", SandboxBranch: "missing"}).Run(cctx)
	if err == nil {
		t.Fatal("expected error for missing sandbox branch")
	}
	if !strings.Contains(err.Error(), `sandbox branch "missing" was not found`) {
		t.Fatalf("error = %v", err)
	}
}

func TestSyncCmdRejectsWrongHostRepository(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	otherDir := t.TempDir()
	git(t, otherDir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(otherDir, "tracked.txt"), "other\n")
	git(t, otherDir, "add", "tracked.txt")
	git(t, otherDir, "-c", "user.name=Sand", "-c", "user.email=sand@example.com", "commit", "-q", "-m", "other")

	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, otherDir)

	err := (&SyncCmd{SandboxName: "box"}).Run(cctx)
	if err == nil {
		t.Fatal("expected error outside sandbox origin")
	}
	if !strings.Contains(err.Error(), "outside sandbox origin directory") {
		t.Fatalf("error = %v", err)
	}
}

func TestSyncCmdRejectsNestedGitRepository(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	nestedDir := filepath.Join(hostDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, nestedDir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(nestedDir, "nested.txt"), "nested\n")
	git(t, nestedDir, "add", "nested.txt")
	git(t, nestedDir, "-c", "user.name=Sand", "-c", "user.email=sand@example.com", "commit", "-q", "-m", "nested")

	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, nestedDir)

	err := (&SyncCmd{SandboxName: "box"}).Run(cctx)
	if err == nil {
		t.Fatal("expected error for nested git repository")
	}
	if !strings.Contains(err.Error(), "does not match sandbox origin directory") {
		t.Fatalf("error = %v", err)
	}
}

func TestSyncCmdFailsSafelyWhenHostWorktreeBlocksSwitch(t *testing.T) {
	hostDir, sandboxWorkDir := setupSyncRepos(t, "box")
	cctx := syncTestCLIContext(t, "box", hostDir, sandboxWorkDir)
	chdir(t, hostDir)
	writeFile(t, filepath.Join(hostDir, "tracked.txt"), "dirty host change\n")

	err := (&SyncCmd{SandboxName: "box"}).Run(cctx)
	if err == nil {
		t.Fatal("expected dirty host worktree error")
	}
	if !strings.Contains(err.Error(), "git switch failed; commit or stash host changes") {
		t.Fatalf("error = %v", err)
	}
	if got := readFile(t, filepath.Join(hostDir, "tracked.txt")); got != "dirty host change\n" {
		t.Fatalf("tracked.txt = %q", got)
	}
}

func TestSyncCmdRejectsInvalidBranchNames(t *testing.T) {
	ctx := context.Background()
	for _, branch := range []string{"-bad", "bad..branch", "bad@{upstream}", "bad branch"} {
		if err := validateSyncBranch(ctx, branch, "host branch"); err == nil {
			t.Fatalf("validateSyncBranch(%q) succeeded, want error", branch)
		}
	}
	if err := validateSyncBranch(ctx, "feature/good.branch", "host branch"); err != nil {
		t.Fatalf("validateSyncBranch() error = %v", err)
	}
}

func TestSyncCmdRejectsInvalidSandboxNames(t *testing.T) {
	for _, name := range []string{"-bad", "bad/name", "bad..name", "bad name"} {
		if err := validateSyncSandboxName(name); err == nil {
			t.Fatalf("validateSyncSandboxName(%q) succeeded, want error", name)
		}
	}
	if err := validateSyncSandboxName("good-name_1"); err != nil {
		t.Fatalf("validateSyncSandboxName() error = %v", err)
	}
}

func TestSyncGitCommandsScrubGitEnvironment(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "tracked\n")
	git(t, repo, "add", "tracked.txt")
	git(t, repo, "-c", "user.name=Sand", "-c", "user.email=sand@example.com", "commit", "-q", "-m", "initial")

	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "hostile.git"))
	if _, err := gitCommandOutput(context.Background(), repo, "rev-parse", "--show-toplevel"); err != nil {
		t.Fatalf("gitCommandOutput() with hostile GIT_DIR error = %v", err)
	}
}

func TestRequireInsideSandboxOriginHandlesSymlinkedTempPaths(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "private", "var")
	if err := os.MkdirAll(filepath.Join(realParent, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(root, "var")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	origin := filepath.Join(aliasParent, "project")
	path := filepath.Join(realParent, "project")
	if err := requireInsideSandboxOrigin(path, origin); err != nil {
		t.Fatalf("requireInsideSandboxOrigin() error = %v", err)
	}
}

func setupSyncRepos(t *testing.T, sandboxBranch string) (hostDir, sandboxWorkDir string) {
	t.Helper()
	root := t.TempDir()
	hostDir = filepath.Join(root, "host")
	sandboxWorkDir = filepath.Join(root, "sandbox")
	sandboxAppDir := filepath.Join(sandboxWorkDir, "app")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sandboxWorkDir, 0o755); err != nil {
		t.Fatal(err)
	}

	git(t, hostDir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(hostDir, "tracked.txt"), "host main\n")
	git(t, hostDir, "add", "tracked.txt")
	git(t, hostDir, "-c", "user.name=Sand", "-c", "user.email=sand@example.com", "commit", "-q", "-m", "initial")

	git(t, root, "clone", "-q", hostDir, sandboxAppDir)
	git(t, sandboxAppDir, "switch", "-q", "-c", sandboxBranch)
	writeFile(t, filepath.Join(sandboxAppDir, "tracked.txt"), "sandbox "+sandboxBranch+"\n")
	git(t, sandboxAppDir, "add", "tracked.txt")
	git(t, sandboxAppDir, "-c", "user.name=Sand", "-c", "user.email=sand@example.com", "commit", "-q", "-m", "sandbox work")

	git(t, hostDir, "remote", "add", "sand/box", sandboxAppDir)
	return hostDir, sandboxWorkDir
}

func syncTestCLIContext(t *testing.T, sandboxName, hostDir, sandboxWorkDir string) *CLIContext {
	t.Helper()
	client := daemontest.StartDaemon(t, daemontest.Deps{}, func(ctx context.Context, s daemontest.SandboxStore) {
		if err := s.SaveSandbox(ctx, &sandtypes.Box{
			ID:             "id-" + sandboxName,
			Name:           sandboxName,
			ContainerID:    "ctr-" + sandboxName,
			HostOriginDir:  hostDir,
			SandboxWorkDir: sandboxWorkDir,
			ImageName:      "test-image:latest",
			AgentType:      "default",
		}); err != nil {
			t.Fatalf("SaveSandbox: %v", err)
		}
	})
	return &CLIContext{
		Context: context.Background(),
		Daemon:  client,
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	})
}
