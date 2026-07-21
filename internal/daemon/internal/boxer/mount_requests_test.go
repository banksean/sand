package boxer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestParseBindMountRequestAcceptsReadOnly(t *testing.T) {
	got, err := parseBindMountRequest("source=/host/path,target=/container/path,readonly")
	if err != nil {
		t.Fatalf("parseBindMountRequest() error = %v", err)
	}
	if got.Source != "/host/path" || got.Target != "/container/path" || !got.ReadOnly {
		t.Fatalf("parsed mount = %+v", got)
	}
}

func TestParseBindMountRequestAcceptsExplicitBindType(t *testing.T) {
	got, err := parseBindMountRequest("type=bind,source=/host/path,target=/container/path")
	if err != nil {
		t.Fatalf("parseBindMountRequest() error = %v", err)
	}
	if got.Source != "/host/path" || got.Target != "/container/path" || got.ReadOnly {
		t.Fatalf("parsed mount = %+v", got)
	}
}

func TestParseBindMountRequestRejectsRelativeSource(t *testing.T) {
	if _, err := parseBindMountRequest("source=host/path,target=/container/path"); err == nil {
		t.Fatal("parseBindMountRequest() error = nil, want relative source error")
	}
}

func TestPrepareMountRequestsClonesDirectories(t *testing.T) {
	ctx := context.Background()
	sandboxRoot := filepath.Join(t.TempDir(), "sandbox")
	sourceDir := filepath.Join(t.TempDir(), "source")
	sourceInfo := mkdirAndStat(t, sourceDir)

	var copiedFrom, copiedTo string
	b := &Boxer{
		FileOps: &hostops.MockFileOps{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
			StatFunc: func(path string) (os.FileInfo, error) {
				if path == sourceDir {
					return sourceInfo, nil
				}
				return sourceInfo, nil
			},
			VolumeFunc: func(path string) (*hostops.VolumeInfo, error) {
				return &hostops.VolumeInfo{Path: path, MountPoint: "/", DeviceID: 1}, nil
			},
			CopyFunc: func(ctx context.Context, src, dst string) error {
				copiedFrom = src
				copiedTo = dst
				return nil
			},
		},
	}

	requests, err := b.prepareMountRequests(ctx,
		cloning.NewStandardPathRegistry(sandboxRoot),
		[]string{"source=/direct,target=/direct-target,readonly"},
		[]string{"source=" + sourceDir + ",target=/data,readonly"})
	if err != nil {
		t.Fatalf("prepareMountRequests() error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("mount requests len = %d, want 2", len(requests))
	}
	if requests[0].Kind != sandtypes.MountKindBind || requests[0].Runtime != "type=bind,source=/direct,target=/direct-target,readonly" {
		t.Fatalf("direct request = %+v", requests[0])
	}
	wantClone := filepath.Join(sandboxRoot, "bind-mounts", "000-source")
	if copiedFrom != sourceDir || copiedTo != wantClone {
		t.Fatalf("copy = %q -> %q, want %q -> %q", copiedFrom, copiedTo, sourceDir, wantClone)
	}
	if requests[1].Kind != sandtypes.MountKindClone || requests[1].Clone != wantClone || requests[1].Runtime != "type=bind,source="+wantClone+",target=/data,readonly" {
		t.Fatalf("clone request = %+v", requests[1])
	}
}

func TestRuntimeMountRequestsUsesPersistedRuntimeMounts(t *testing.T) {
	requests := []sandtypes.MountRequest{{
		Kind:    sandtypes.MountKindClone,
		Runtime: "type=bind,source=/clone,target=/target,readonly",
	}}

	got := sandtypes.RuntimeMountRequests(requests)
	if len(got) != 1 || got[0] != "type=bind,source=/clone,target=/target,readonly" {
		t.Fatalf("RuntimeMountRequests() = %+v", got)
	}
}

func mkdirAndStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
