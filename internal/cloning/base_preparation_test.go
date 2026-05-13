package cloning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
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

func TestBaseWorkspacePreparationDefaultProfileDoesNotCopyZshrc(t *testing.T) {
	ctx := context.Background()
	hostWorkDir := t.TempDir()
	cloneRoot := filepath.Join(t.TempDir(), "clones")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("secret shell hook\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prep := newDotfileTestPreparation(t, cloneRoot)
	artifacts, err := prep.Prepare(ctx, CloneRequest{
		ID:          "sandbox-default",
		Name:        "sandbox-default",
		HostWorkDir: hostWorkDir,
		Profile:     sandtypes.Profile{Name: sandtypes.DefaultProfileName},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(artifacts.PathRegistry.DotfilesDir(), ".zshrc")); !os.IsNotExist(err) {
		t.Fatalf("default profile copied .zshrc: err=%v", err)
	}
}

func TestBaseWorkspacePreparationCopiesAllowlistedDotfiles(t *testing.T) {
	ctx := context.Background()
	hostWorkDir := t.TempDir()
	cloneRoot := filepath.Join(t.TempDir(), "clones")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".zshrc.sand"), []byte("sandbox shell\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prep := newDotfileTestPreparation(t, cloneRoot)
	artifacts, err := prep.Prepare(ctx, CloneRequest{
		ID:          "sandbox-allowlist",
		Name:        "sandbox-allowlist",
		HostWorkDir: hostWorkDir,
		Profile: sandtypes.Profile{
			Name: sandtypes.DefaultProfileName,
			Dotfiles: sandtypes.DotfilePolicy{
				Mode: sandtypes.DotfileModeAllowlist,
				Files: []sandtypes.DotfileRule{{
					Source: "~/.zshrc.sand",
					Target: "~/.zshrc",
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(artifacts.PathRegistry.DotfilesDir(), ".zshrc"))
	if err != nil {
		t.Fatalf("ReadFile copied .zshrc: %v", err)
	}
	if string(got) != "sandbox shell\n" {
		t.Fatalf("copied .zshrc = %q, want sandbox shell", got)
	}
}

func TestBaseWorkspacePreparationRejectsSymlinkOutsideHomeByDefault(t *testing.T) {
	ctx := context.Background()
	hostWorkDir := t.TempDir()
	cloneRoot := filepath.Join(t.TempDir(), "clones")
	home := t.TempDir()
	outside := t.TempDir()
	t.Setenv("HOME", home)
	outsideTarget := filepath.Join(outside, "gitconfig")
	if err := os.WriteFile(outsideTarget, []byte("[credential]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideTarget, filepath.Join(home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}

	prep := newDotfileTestPreparation(t, cloneRoot)
	_, err := prep.Prepare(ctx, CloneRequest{
		ID:          "sandbox-symlink",
		Name:        "sandbox-symlink",
		HostWorkDir: hostWorkDir,
		Profile: sandtypes.Profile{
			Name: sandtypes.DefaultProfileName,
			Dotfiles: sandtypes.DotfilePolicy{
				Mode: sandtypes.DotfileModeAllowlist,
				Files: []sandtypes.DotfileRule{{
					Source:       "~/.gitconfig",
					Target:       "~/.gitconfig",
					AllowSymlink: true,
				}},
			},
		},
	})
	if err == nil {
		t.Fatal("Prepare() error = nil, want symlink outside home error")
	}
	if !strings.Contains(err.Error(), "outside $HOME") {
		t.Fatalf("Prepare() error = %v, want outside $HOME", err)
	}
}

func TestBaseWorkspacePreparationSanitizesGitConfig(t *testing.T) {
	ctx := context.Background()
	hostWorkDir := t.TempDir()
	cloneRoot := filepath.Join(t.TempDir(), "clones")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(`[user]
	name = Ada Lovelace
	email = ada@example.com
[credential]
	helper = osxkeychain
	username = ada
[alias]
	co = checkout
	leak = !cat ~/.ssh/id_ed25519
[core]
	editor = vim
	sshCommand = ssh -i ~/.ssh/id_ed25519
	hooksPath = ~/.githooks
[includeIf "gitdir:~/work/"]
	path = ~/.gitconfig-work
[color]
	ui = auto
`), 0o644); err != nil {
		t.Fatal(err)
	}

	prep := newDotfileTestPreparation(t, cloneRoot)
	artifacts, err := prep.Prepare(ctx, CloneRequest{
		ID:          "sandbox-gitconfig",
		Name:        "sandbox-gitconfig",
		HostWorkDir: hostWorkDir,
		Profile: sandtypes.Profile{
			Name: sandtypes.DefaultProfileName,
			Dotfiles: sandtypes.DotfilePolicy{
				Mode: sandtypes.DotfileModeAllowlist,
				Files: []sandtypes.DotfileRule{{
					Source: "~/.gitconfig",
					Target: "~/.gitconfig",
				}},
			},
			Git: sandtypes.GitPolicy{Config: sandtypes.GitConfigPolicySanitized},
		},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(artifacts.PathRegistry.DotfilesDir(), ".gitconfig"))
	if err != nil {
		t.Fatalf("ReadFile sanitized .gitconfig: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"name = Ada Lovelace",
		"email = ada@example.com",
		"co = checkout",
		"editor = vim",
		"ui = auto",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized .gitconfig missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{
		"[credential]",
		"helper = osxkeychain",
		"username = ada",
		"leak = !cat",
		"sshCommand",
		"hooksPath",
		"[includeIf",
		".gitconfig-work",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sanitized .gitconfig contains %q:\n%s", forbidden, got)
		}
	}
}

func newDotfileTestPreparation(t *testing.T, cloneRoot string) *BaseWorkspacePreparation {
	t.Helper()
	fileOps := &hostops.MockFileOps{
		MkdirAllFunc:  os.MkdirAll,
		StatFunc:      os.Stat,
		LstatFunc:     os.Lstat,
		ReadlinkFunc:  os.Readlink,
		CreateFunc:    os.Create,
		RemoveAllFunc: os.RemoveAll,
		WriteFileFunc: os.WriteFile,
		VolumeFunc: func(path string) (*hostops.VolumeInfo, error) {
			return &hostops.VolumeInfo{DeviceID: 1, MountPoint: "/"}, nil
		},
		CopyFunc: func(ctx context.Context, src, dst string) error {
			info, err := os.Stat(src)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return os.MkdirAll(dst, 0o750)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
				return err
			}
			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, info.Mode())
		},
	}
	return NewBaseWorkspacePreparation(cloneRoot, hostops.NewTerminalMessenger(nil), &hostops.MockGitOps{}, fileOps)
}
