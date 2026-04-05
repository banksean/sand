package hostops

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

type FileOps interface {
	MkdirAll(path string, perm os.FileMode) error
	Copy(ctx context.Context, src, dst string) error
	Stat(path string) (os.FileInfo, error)
	Lstat(path string) (os.FileInfo, error)
	Readlink(path string) (string, error)
	Create(path string) (*os.File, error)
	RemoveAll(path string) error
	WriteFile(path string, data []byte, perm os.FileMode) error
	Volume(path string) (*VolumeInfo, error)
}

type defaultFileOps struct{}

func (f *defaultFileOps) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (f *defaultFileOps) Copy(ctx context.Context, src, dst string) error {
	cmd := exec.CommandContext(ctx, "cp", "-Rc", src, dst)
	slog.InfoContext(ctx, "FileOps.Copy", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "FileOps.Copy", "error", err, "output", string(output))
		return fmt.Errorf("copy failed: %w (output: %s)", err, output)
	}
	return nil
}

type VolumeInfo struct {
	Path       string // The original path checked
	MountPoint string // Where the volume is mounted (e.g., /Volumes/Backup)
	DeviceID   int32  // The unique integer ID for this volume
	DeviceName string // The BSD node (e.g., /dev/disk4s2)
}

func (f *defaultFileOps) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (f *defaultFileOps) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func (f *defaultFileOps) Readlink(path string) (string, error) {
	return os.Readlink(path)
}

func (f *defaultFileOps) Create(path string) (*os.File, error) {
	ret, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (f *defaultFileOps) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (f *defaultFileOps) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
