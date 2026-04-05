package hostops

import (
	"context"
	"io"

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
