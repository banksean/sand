package sand

import (
	"context"
	"io"

	ac "github.com/banksean/sand/applecontainer"
	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
)

type ContainerService interface {
	Create(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error)
	Start(ctx context.Context, opts *options.StartContainer, containerID string) (string, error)
	Stop(ctx context.Context, opts *options.StopContainer, containerID string) (string, error)
	Delete(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error)
	Exec(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error)
	ExecStream(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer) (func() error, error)
	Inspect(ctx context.Context, containerID string) ([]types.Container, error)
}

type ImageService interface {
	List(ctx context.Context) ([]types.ImageEntry, error)
	Pull(ctx context.Context, image string) (func() error, error)
}

type appleContainerService struct{}

func NewAppleContainerService() ContainerService {
	return &appleContainerService{}
}

func (a *appleContainerService) Create(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
	return ac.Containers.Create(ctx, opts, image, args)
}

func (a *appleContainerService) Start(ctx context.Context, opts *options.StartContainer, containerID string) (string, error) {
	return ac.Containers.Start(ctx, opts, containerID)
}

func (a *appleContainerService) Stop(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
	return ac.Containers.Stop(ctx, opts, containerID)
}

func (a *appleContainerService) Delete(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
	return ac.Containers.Delete(ctx, opts, containerID)
}

func (a *appleContainerService) Exec(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
	return ac.Containers.Exec(ctx, opts, containerID, cmd, env, args...)
}

func (a *appleContainerService) ExecStream(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer) (func() error, error) {
	return ac.Containers.ExecStream(ctx, opts, containerID, cmd, env, stdin, stdout, stderr)
}

func (a *appleContainerService) Inspect(ctx context.Context, containerID string) ([]types.Container, error) {
	return ac.Containers.Inspect(ctx, containerID)
}

type appleImageService struct{}

func NewAppleImageService() ImageService {
	return &appleImageService{}
}

func (a *appleImageService) List(ctx context.Context) ([]types.ImageEntry, error) {
	return ac.Images.List(ctx)
}

func (a *appleImageService) Pull(ctx context.Context, image string) (func() error, error) {
	return ac.Images.Pull(ctx, image)
}
