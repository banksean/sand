package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/sandtypes"
)

const (
	inspectionHeadRef   = "refs/sand/HEAD"
	inspectionRemoteRef = "refs/remotes/sandbox/"
)

type hardenedGit struct {
	ctx context.Context
}

func newHardenedGit(ctx context.Context) hardenedGit {
	return hardenedGit{ctx: ctx}
}

func (g hardenedGit) command(dir string, args ...string) *exec.Cmd {
	base := []string{
		"-c", "core.pager=cat",
		"-c", "pager.status=false",
		"-c", "pager.log=false",
		"-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.attributesFile=/dev/null",
		"-c", "diff.external=",
	}
	cmd := exec.CommandContext(g.ctx, "git", append(base, args...)...)
	cmd.Dir = dir
	cmd.Env = hardenedGitEnv()
	return cmd
}

func hardenedGitEnv() []string {
	env := make([]string, 0, len(os.Environ())+12)
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "GIT_") || key == "PAGER" || key == "SSH_ASKPASS" {
			continue
		}
		env = append(env, kv)
	}
	return append(
		env,
		"HOME=/dev/null",
		"XDG_CONFIG_HOME=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"PAGER=cat",
		"GIT_EXTERNAL_DIFF=",
		"GIT_ASKPASS=/bin/false",
		"SSH_ASKPASS=/bin/false",
	)
}

type gitInspectionCache struct {
	appBaseDir string
	sandbox    *sandtypes.Box
	git        hardenedGit
}

func newGitInspectionCache(ctx context.Context, appBaseDir string, sandbox *sandtypes.Box) gitInspectionCache {
	return gitInspectionCache{
		appBaseDir: appBaseDir,
		sandbox:    sandbox,
		git:        newHardenedGit(ctx),
	}
}

func (c gitInspectionCache) dir() (string, error) {
	if c.appBaseDir == "" {
		return "", fmt.Errorf("app base dir is required for git inspection cache")
	}
	if c.sandbox == nil || c.sandbox.ID == "" {
		return "", fmt.Errorf("sandbox id is required for git inspection cache")
	}
	return filepath.Join(c.appBaseDir, "git-inspection", c.sandbox.ID+".git"), nil
}

func (c gitInspectionCache) ensureUpdated() (string, error) {
	cacheDir, err := c.dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o750); err != nil {
		return "", fmt.Errorf("create git inspection cache root: %w", err)
	}
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		cmd := c.git.command("", "init", "--bare", cacheDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("initialize git inspection cache: %w (output: %s)", err, output)
		}
	} else if err != nil {
		return "", fmt.Errorf("stat git inspection cache: %w", err)
	}

	sandboxAppDir := filepath.Join(c.sandbox.SandboxWorkDir, "app")
	cmd := c.git.command(
		"", "--git-dir", cacheDir, "fetch", "--prune", sandboxAppDir,
		"+refs/heads/*:"+inspectionRemoteRef+"*",
		"+HEAD:"+inspectionHeadRef,
	)
	slog.InfoContext(c.git.ctx, "git inspection cache fetch", "cache", cacheDir, "sandbox", sandboxAppDir, "cmd", strings.Join(cmd.Args, " "))
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("fetch sandbox refs into git inspection cache: %w (output: %s)", err, output)
	}
	return cacheDir, nil
}

func (c gitInspectionCache) refForBranch(branch string) string {
	if branch == "" {
		return inspectionHeadRef
	}
	return inspectionRemoteRef + branch
}

func (c gitInspectionCache) exportRef(cacheDir, ref, dst string) error {
	cmd := c.git.command("", "--git-dir", cacheDir, "archive", "--format=tar", ref)
	var tarBytes bytes.Buffer
	cmd.Stdout = &tarBytes
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("archive sandbox ref %s: %w", ref, err)
	}
	return untar(&tarBytes, dst)
}

func sandboxWorktreeSnapshot(ctx context.Context, sandboxAppDir, dst string) error {
	git := newHardenedGit(ctx)
	head := git.command(sandboxAppDir, "rev-parse", "--verify", "HEAD")
	if err := head.Run(); err == nil {
		cmd := git.command(sandboxAppDir, "archive", "--format=tar", "HEAD")
		var tarBytes bytes.Buffer
		cmd.Stdout = &tarBytes
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("archive sandbox HEAD: %w", err)
		}
		if err := untar(&tarBytes, dst); err != nil {
			return err
		}
	}

	deleted, err := gitOutputList(git, sandboxAppDir, "ls-files", "-z", "--deleted")
	if err != nil {
		return err
	}
	for _, rel := range deleted {
		if err := removeSnapshotPath(dst, rel); err != nil {
			return err
		}
	}

	files, err := gitOutputList(git, sandboxAppDir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	for _, rel := range files {
		if err := copySnapshotPath(sandboxAppDir, dst, rel); err != nil {
			return err
		}
	}
	return nil
}

func sandboxHasUncommittedChanges(ctx context.Context, sandboxAppDir string) (bool, error) {
	git := newHardenedGit(ctx)
	cmd := git.command(sandboxAppDir, "status", "--porcelain", "--untracked-files=all")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status --porcelain failed: %w", err)
	}
	return len(bytes.TrimSpace(output)) > 0, nil
}

func hostWorktreeSnapshot(ctx context.Context, hostDir, dst string) error {
	git := newHardenedGit(ctx)
	files, err := gitOutputList(git, hostDir, "ls-files", "-z", "--cached")
	if err != nil {
		return err
	}
	for _, rel := range files {
		if err := copySnapshotPath(hostDir, dst, rel); err != nil {
			return err
		}
	}
	return nil
}

func gitOutputList(git hardenedGit, dir string, args ...string) ([]string, error) {
	cmd := git.command(dir, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}
	parts := bytes.Split(output, []byte{0})
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		rel := string(part)
		if err := validateSnapshotRelPath(rel); err != nil {
			return nil, err
		}
		files = append(files, rel)
	}
	return files, nil
}

func validateSnapshotRelPath(rel string) error {
	if rel == "" || filepath.IsAbs(rel) || rel == "." {
		return fmt.Errorf("invalid git path %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("git path escapes snapshot root: %q", rel)
	}
	return nil
}

func copySnapshotPath(srcRoot, dstRoot, rel string) error {
	if err := validateSnapshotRelPath(rel); err != nil {
		return err
	}
	src := filepath.Join(srcRoot, rel)
	dst := filepath.Join(dstRoot, rel)
	info, err := os.Lstat(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat snapshot path %s: %w", rel, err)
	}
	if info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("read symlink %s: %w", rel, err)
		}
		_ = os.Remove(dst)
		return os.Symlink(target, dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open snapshot source %s: %w", rel, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create snapshot target %s: %w", rel, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy snapshot path %s: %w", rel, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close snapshot target %s: %w", rel, err)
	}
	return nil
}

func removeSnapshotPath(root, rel string) error {
	if err := validateSnapshotRelPath(rel); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, rel)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func untar(r io.Reader, dst string) error {
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read git archive: %w", err)
		}
		if err := validateSnapshotRelPath(header.Name); err != nil {
			return err
		}
		path := filepath.Join(dst, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(header.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode).Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, path); err != nil {
				return err
			}
		}
	}
}
