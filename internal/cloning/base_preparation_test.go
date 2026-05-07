package cloning

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/banksean/sand/internal/hostops"
)

func TestBaseWorkspacePreparationUsesSharedGitMirror(t *testing.T) {
	ctx := context.Background()
	hostWorkDir := t.TempDir()
	cloneRoot := filepath.Join(t.TempDir(), "Sand", "clones")
	home := t.TempDir()
	t.Setenv("HOME", home)

	var clonedMirror string
	gitOps := &hostops.MockGitOps{
		TopLevelFunc: func(ctx context.Context, dir string) string {
			return hostWorkDir
		},
		CloneMirrorFunc: func(ctx context.Context, sourceDir, mirrorDir string) error {
			if sourceDir != hostWorkDir {
				t.Fatalf("mirror source = %q, want %q", sourceDir, hostWorkDir)
			}
			clonedMirror = mirrorDir
			return os.MkdirAll(mirrorDir, 0o750)
		},
		CommitFunc: func(ctx context.Context, dir string) string {
			return "abc123"
		},
	}
	fileOps := &hostops.MockFileOps{
		MkdirAllFunc: os.MkdirAll,
		StatFunc:     os.Stat,
		LstatFunc:    os.Lstat,
		CreateFunc:   os.Create,
		VolumeFunc: func(path string) (*hostops.VolumeInfo, error) {
			return &hostops.VolumeInfo{DeviceID: 1, MountPoint: "/"}, nil
		},
		CopyFunc: func(ctx context.Context, src, dst string) error {
			return os.MkdirAll(dst, 0o750)
		},
	}

	prep := NewBaseWorkspacePreparation(cloneRoot, hostops.NewTerminalMessenger(nil), gitOps, fileOps)
	artifacts, err := prep.Prepare(ctx, CloneRequest{ID: "sandbox-1", Name: "friendly", HostWorkDir: hostWorkDir})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if artifacts.HostWorkDir != hostWorkDir {
		t.Fatalf("HostWorkDir = %q, want %q", artifacts.HostWorkDir, hostWorkDir)
	}
	if artifacts.HostGitMirrorDir == "" {
		t.Fatal("HostGitMirrorDir is empty")
	}
	if artifacts.HostGitMirrorDir != clonedMirror {
		t.Fatalf("HostGitMirrorDir = %q, want cloned mirror %q", artifacts.HostGitMirrorDir, clonedMirror)
	}
	if wantRoot := filepath.Join(filepath.Dir(cloneRoot), "git-mirrors"); filepath.Dir(artifacts.HostGitMirrorDir) != wantRoot {
		t.Fatalf("mirror root = %q, want %q", filepath.Dir(artifacts.HostGitMirrorDir), wantRoot)
	}
}
