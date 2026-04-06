package hostops

import (
	"context"
	"io"
	"os"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
)

type MockContainerOps struct {
	CreateFunc     func(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error)
	StartFunc      func(ctx context.Context, opts *options.StartContainer, containerID string) (string, error)
	StopFunc       func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error)
	DeleteFunc     func(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error)
	ExecFunc       func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error)
	ExecStreamFunc func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer, cmdArgs ...string) (func() error, error)
	InspectFunc    func(ctx context.Context, containerID string) ([]types.Container, error)
	StatsFunc      func(ctx context.Context, containerID ...string) ([]types.ContainerStats, error)
	ExportFunc     func(ctx context.Context, containerID, image string) (string, error)
}

// Export implements [ContainerOps].
func (m *MockContainerOps) Export(ctx context.Context, opts *options.ExportContainer, imageName string) (string, error) {
	panic("unimplemented")
}

func (m *MockContainerOps) Create(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, opts, image, args)
	}
	return "mock-container-id", nil
}

func (m *MockContainerOps) Start(ctx context.Context, opts *options.StartContainer, containerID string) (string, error) {
	if m.StartFunc != nil {
		return m.StartFunc(ctx, opts, containerID)
	}
	return "started", nil
}

func (m *MockContainerOps) Stop(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
	if m.StopFunc != nil {
		return m.StopFunc(ctx, opts, containerID)
	}
	return "stopped", nil
}

func (m *MockContainerOps) Delete(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, opts, containerID)
	}
	return "deleted", nil
}

func (m *MockContainerOps) Exec(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
	if m.ExecFunc != nil {
		return m.ExecFunc(ctx, opts, containerID, cmd, env, args...)
	}
	return "exec output", nil
}

func (m *MockContainerOps) ExecStream(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer, cmdArgs ...string) (func() error, error) {
	if m.ExecStreamFunc != nil {
		return m.ExecStreamFunc(ctx, opts, containerID, cmd, env, stdin, stdout, stderr, cmdArgs...)
	}
	return func() error { return nil }, nil
}

func (m *MockContainerOps) Inspect(ctx context.Context, containerID string) ([]types.Container, error) {
	if m.InspectFunc != nil {
		return m.InspectFunc(ctx, containerID)
	}
	return []types.Container{{Status: "running"}}, nil
}

func (m *MockContainerOps) Stats(ctx context.Context, containerID ...string) ([]types.ContainerStats, error) {
	if m.StatsFunc != nil {
		return m.StatsFunc(ctx, containerID...)
	}
	return nil, nil
}

type MockGitOps struct {
	AddRemoteFunc    func(ctx context.Context, dir, name, url string) error
	RemoveRemoteFunc func(ctx context.Context, dir, name string) error
	FetchFunc        func(ctx context.Context, dir, remote string) error
	TopLevelFunc     func(ctx context.Context, dir string) string
	RemoteURLFunc    func(ctx context.Context, dir, name string) string
	BranchFunc       func(ctx context.Context, dir string) string
	CommitFunc       func(ctx context.Context, dir string) string
	IsDirtyFunc      func(ctx context.Context, dir string) bool
}

func (m *MockGitOps) AddRemote(ctx context.Context, dir, name, url string) error {
	if m.AddRemoteFunc != nil {
		return m.AddRemoteFunc(ctx, dir, name, url)
	}
	return nil
}

func (m *MockGitOps) RemoveRemote(ctx context.Context, dir, name string) error {
	if m.RemoveRemoteFunc != nil {
		return m.RemoveRemoteFunc(ctx, dir, name)
	}
	return nil
}

func (m *MockGitOps) Fetch(ctx context.Context, dir, remote string) error {
	if m.FetchFunc != nil {
		return m.FetchFunc(ctx, dir, remote)
	}
	return nil
}

func (m *MockGitOps) TopLevel(ctx context.Context, dir string) string {
	if m.TopLevelFunc != nil {
		return m.TopLevelFunc(ctx, dir)
	}
	return ""
}

func (m *MockGitOps) RemoteURL(ctx context.Context, dir, name string) string {
	if m.RemoteURLFunc != nil {
		return m.RemoteURLFunc(ctx, dir, name)
	}
	return ""
}

func (m *MockGitOps) Branch(ctx context.Context, dir string) string {
	if m.BranchFunc != nil {
		return m.BranchFunc(ctx, dir)
	}
	return ""
}

func (m *MockGitOps) Commit(ctx context.Context, dir string) string {
	if m.CommitFunc != nil {
		return m.CommitFunc(ctx, dir)
	}
	return ""
}

func (m *MockGitOps) IsDirty(ctx context.Context, dir string) bool {
	if m.IsDirtyFunc != nil {
		return m.IsDirtyFunc(ctx, dir)
	}
	return false
}

type MockFileOps struct {
	MkdirAllFunc  func(path string, perm os.FileMode) error
	CopyFunc      func(ctx context.Context, src, dst string) error
	StatFunc      func(path string) (os.FileInfo, error)
	LstatFunc     func(path string) (os.FileInfo, error)
	ReadlinkFunc  func(path string) (string, error)
	CreateFunc    func(path string) (*os.File, error)
	RemoveAllFunc func(path string) error
	WriteFileFunc func(path string, data []byte, perm os.FileMode) error
	VolumeFunc    func(path string) (*VolumeInfo, error)
}

func (m *MockFileOps) MkdirAll(path string, perm os.FileMode) error {
	if m.MkdirAllFunc != nil {
		return m.MkdirAllFunc(path, perm)
	}
	return nil
}

func (m *MockFileOps) Copy(ctx context.Context, src, dst string) error {
	if m.CopyFunc != nil {
		return m.CopyFunc(ctx, src, dst)
	}
	return nil
}

func (m *MockFileOps) Stat(path string) (os.FileInfo, error) {
	if m.StatFunc != nil {
		return m.StatFunc(path)
	}
	return nil, nil
}

func (m *MockFileOps) Lstat(path string) (os.FileInfo, error) {
	if m.LstatFunc != nil {
		return m.LstatFunc(path)
	}
	return nil, nil
}

func (m *MockFileOps) Readlink(path string) (string, error) {
	if m.ReadlinkFunc != nil {
		return m.ReadlinkFunc(path)
	}
	return "", nil
}

func (m *MockFileOps) Create(path string) (*os.File, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(path)
	}
	return nil, nil
}

func (m *MockFileOps) RemoveAll(path string) error {
	if m.RemoveAllFunc != nil {
		return m.RemoveAllFunc(path)
	}
	return nil
}

func (m *MockFileOps) WriteFile(path string, data []byte, perm os.FileMode) error {
	if m.WriteFileFunc != nil {
		return m.WriteFileFunc(path, data, perm)
	}
	return nil
}

func (m *MockFileOps) Volume(path string) (*VolumeInfo, error) {
	if m.VolumeFunc != nil {
		return m.VolumeFunc(path)
	}
	return nil, nil
}
