// Package sandtypes contains shared types used across the sand codebase.
// It exists as a separate package to avoid import cycles between the main sand package and its subpackages.
package sandtypes

import (
	"context"
	"fmt"
	"strings"

	"github.com/banksean/sand/applecontainer/types"
)

// // BoxOperations defines the operations that container startup hooks can perform on a sandbox.
// type BoxOperations interface {
// 	Exec(ctx context.Context, shellCmd string, args ...string) (string, error)
// 	GetContainer(ctx context.Context) (*types.Container, error)
// }

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

type StartupHook func(ctx context.Context, shellCmd string, args ...string) (string, error)

// ContainerStartupHook allows callers to inject container startup customisation.
type ContainerStartupHook interface {
	Name() string
	OnStart(ctx context.Context, ctr *types.Container, exec StartupHook) error
}

type containerHook struct {
	name string
	fn   func(ctx context.Context, ctr *types.Container, exec StartupHook) error
}

func (h containerHook) Name() string {
	return h.name
}

func (h containerHook) OnStart(ctx context.Context, ctr *types.Container, exec StartupHook) error {
	return h.fn(ctx, ctr, exec)
}

// NewContainerStartupHook helps callers construct hook instances without exporting internals.
func NewContainerStartupHook(name string, fn func(ctx context.Context, ctr *types.Container, exec StartupHook) error) ContainerStartupHook {
	return containerHook{name: name, fn: fn}
}
