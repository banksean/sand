package sand

import (
	"context"
	"fmt"
	"strings"
)

// MountSpec describes a bind mount that should be attached to a container.
type MountSpec struct {
	Source   string
	Target   string
	ReadOnly bool
}

// String renders the mount specification into the container runtime format.
func (m MountSpec) String() string {
	parts := []string{
		"type=bind",
		fmt.Sprintf("source=%s", m.Source),
		fmt.Sprintf("target=%s", m.Target),
	}
	if m.ReadOnly {
		parts = append(parts, "readonly")
	}
	return strings.Join(parts, ",")
}

// ContainerStartupHook allows callers to inject container startup customisation.
type ContainerStartupHook interface {
	Name() string
	OnStart(ctx context.Context, b *Box) error
}

type containerHook struct {
	name string
	fn   func(ctx context.Context, b *Box) error
}

func (h containerHook) Name() string {
	return h.name
}

func (h containerHook) OnStart(ctx context.Context, b *Box) error {
	return h.fn(ctx, b)
}

// NewContainerStartupHook helpers callers construct hook instances without exporting internals.
func NewContainerStartupHook(name string, fn func(ctx context.Context, b *Box) error) ContainerStartupHook {
	return containerHook{name: name, fn: fn}
}

// CloneRequest captures the inputs necessary to prepare a sandbox workspace.
type CloneRequest struct {
	ID          string
	HostWorkDir string
	EnvFile     string
}

// CloneResult describes the assets created for a sandbox and how to mount/configure them.
type CloneResult struct {
	SandboxWorkDir string
	Mounts         []MountSpec
	ContainerHooks []ContainerStartupHook
}

// WorkspaceCloner abstracts the steps for preparing sandbox host resources.
type WorkspaceCloner interface {
	Prepare(ctx context.Context, req CloneRequest) (*CloneResult, error)
	// BUG: Hydrate isn't getting called anywhere, so extra container startup hooks aren't getting registered or invoked.
	Hydrate(ctx context.Context, box *Box) error
}
