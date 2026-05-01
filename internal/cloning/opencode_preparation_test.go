package cloning

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/banksean/sand/internal/hostops"
)

func TestOpenCodeWorkspacePreparationDoesNotCopyHostAuthState(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	work := filepath.Join(dir, "work")
	cloneRoot := filepath.Join(dir, "clones")
	hostOpenCode := filepath.Join(home, ".local", "share", "opencode")

	if err := os.MkdirAll(filepath.Join(hostOpenCode, "storage"), 0o750); err != nil {
		t.Fatalf("MkdirAll storage: %v", err)
	}
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatalf("MkdirAll work: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hostOpenCode, "auth.json"), []byte(`{"secret":"token"}`), 0o600); err != nil {
		t.Fatalf("WriteFile auth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hostOpenCode, "storage", "state.json"), []byte(`{"secret":"state"}`), 0o600); err != nil {
		t.Fatalf("WriteFile storage: %v", err)
	}
	t.Setenv("HOME", home)

	prep := NewOpenCodeWorkspacePreparation(cloneRoot, hostops.NewNullMessenger(), &hostops.MockGitOps{}, newPreparationTestFileOps(t))
	artifacts, err := prep.Prepare(context.Background(), CloneRequest{
		ID:          "box",
		HostWorkDir: work,
		Username:    "user",
		Uid:         "501",
	})
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	opencodeShare := filepath.Join(artifacts.PathRegistry.DotfilesDir(), ".local", "share", "opencode")
	if _, err := os.Stat(filepath.Join(opencodeShare, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("host auth was copied to %s, stat err = %v", opencodeShare, err)
	}
	if _, err := os.Stat(filepath.Join(opencodeShare, "storage")); !os.IsNotExist(err) {
		t.Fatalf("host storage was copied to %s, stat err = %v", opencodeShare, err)
	}
	configPath := filepath.Join(artifacts.PathRegistry.DotfilesDir(), ".config", "opencode", "opencode.json")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("generated opencode config missing: %v", err)
	}
	if !json.Valid(config) {
		t.Fatalf("generated opencode config is not valid JSON: %s", config)
	}
}

func newPreparationTestFileOps(t *testing.T) hostops.FileOps {
	t.Helper()
	return &hostops.MockFileOps{
		MkdirAllFunc: os.MkdirAll,
		CopyFunc: func(ctx context.Context, src, dst string) error {
			return copyPathForTest(src, dst)
		},
		LstatFunc:  os.Lstat,
		CreateFunc: os.Create,
		VolumeFunc: func(path string) (*hostops.VolumeInfo, error) {
			return &hostops.VolumeInfo{Path: path, MountPoint: filepath.VolumeName(path), DeviceID: 1}, nil
		},
	}
}

func copyPathForTest(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFileForTest(src, dst, info.Mode())
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFileForTest(path, target, info.Mode())
	})
}

func copyFileForTest(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}
