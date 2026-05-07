package cloning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/banksean/sand/internal/hostops"
)

const SandboxSnapshotRefPrefix = "refs/sand/snapshots/"

var gitMirrorLocks sync.Map

// GitMirror manages the shared bare mirror for an original host git repository.
type GitMirror struct {
	root    string
	gitOps  hostops.GitOps
	fileOps hostops.FileOps
}

func NewGitMirror(root string, gitOps hostops.GitOps, fileOps hostops.FileOps) *GitMirror {
	return &GitMirror{
		root:    root,
		gitOps:  gitOps,
		fileOps: fileOps,
	}
}

func DefaultGitMirrorRoot(cloneRoot string) string {
	return filepath.Join(filepath.Dir(cloneRoot), "git-mirrors")
}

func (m *GitMirror) EnsureUpdated(ctx context.Context, hostGitTopLevel string) (string, error) {
	mirrorDir, err := m.MirrorDir(hostGitTopLevel)
	if err != nil {
		return "", err
	}
	if err := m.fileOps.MkdirAll(filepath.Dir(mirrorDir), 0o750); err != nil {
		return "", fmt.Errorf("create git mirror root %s: %w", filepath.Dir(mirrorDir), err)
	}

	lock, _ := gitMirrorLocks.LoadOrStore(mirrorDir, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	fi, err := m.fileOps.Stat(mirrorDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := m.gitOps.CloneMirror(ctx, hostGitTopLevel, mirrorDir); err != nil {
			return "", fmt.Errorf("create git mirror from %s at %s: %w", hostGitTopLevel, mirrorDir, err)
		}
	case err != nil:
		return "", fmt.Errorf("stat git mirror %s: %w", mirrorDir, err)
	case fi == nil || !fi.IsDir():
		return "", fmt.Errorf("git mirror path %s exists but is not a directory", mirrorDir)
	default:
		if err := m.gitOps.UpdateMirror(ctx, mirrorDir); err != nil {
			return "", fmt.Errorf("update git mirror %s from %s: %w", mirrorDir, hostGitTopLevel, err)
		}
	}

	return mirrorDir, nil
}

func (m *GitMirror) WriteSnapshotRef(ctx context.Context, mirrorDir, sandboxID, commit string) error {
	if mirrorDir == "" || sandboxID == "" || commit == "" {
		return nil
	}
	ref := SandboxSnapshotRefPrefix + sandboxID
	if err := m.gitOps.UpdateRef(ctx, mirrorDir, ref, commit); err != nil {
		return fmt.Errorf("write snapshot ref %s in %s: %w", ref, mirrorDir, err)
	}
	return nil
}

func (m *GitMirror) MirrorDir(hostGitTopLevel string) (string, error) {
	repoID, err := StableRepoID(m.fileOps, hostGitTopLevel)
	if err != nil {
		return "", err
	}
	return filepath.Join(m.root, repoID+".git"), nil
}

func StableRepoID(fileOps hostops.FileOps, hostGitTopLevel string) (string, error) {
	canonical, err := filepath.EvalSymlinks(hostGitTopLevel)
	if err != nil {
		return "", fmt.Errorf("canonicalize git top-level %s: %w", hostGitTopLevel, err)
	}
	fi, err := fileOps.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("stat git top-level %s: %w", canonical, err)
	}
	if fi == nil {
		return "", fmt.Errorf("stat git top-level %s returned no file info", canonical)
	}
	dev, ino, ok := fileIdentity(fi)
	if !ok {
		return "", fmt.Errorf("stat git top-level %s did not include device/inode metadata", canonical)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", canonical, dev, ino)))
	return hex.EncodeToString(sum[:16]), nil
}

func fileIdentity(fi os.FileInfo) (dev, ino uint64, ok bool) {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return uint64(stat.Dev), uint64(stat.Ino), true
}
