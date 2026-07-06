package hostops

import (
	"context"
	"io"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/imageprogress"
)

type ContainerOps interface {
	Create(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error)
	Start(ctx context.Context, opts *options.StartContainer, containerID string) (string, error)
	Stop(ctx context.Context, opts *options.StopContainer, containerID string) (string, error)
	Delete(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error)
	Exec(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error)
	ExecStream(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer, cmdArgs ...string) (func() error, error)
	Inspect(ctx context.Context, containerID string) ([]types.Container, error)
	Stats(ctx context.Context, containerID ...string) ([]types.ContainerStats, error)
	Export(ctx context.Context, opts *options.ExportContainer, imageName string) (string, error)
}

type ImageOps interface {
	List(ctx context.Context) ([]types.ImageEntry, error)
	Pull(ctx context.Context, image string, progress imageprogress.Sink) (func() error, error)
	Inspect(ctx context.Context, name string) ([]*types.ImageManifest, error)
}

func NewAppleContainerOps() (ContainerOps, error) {
	return newAppleContainerOps()
}

func NewAppleImageOps() (ImageOps, error) {
	return newAppleImageOps()
}
