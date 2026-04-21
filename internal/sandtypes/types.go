// Package sandtypes contains shared types used across the sand codebase.
// It exists as a separate package to avoid import cycles between the main sand package and its subpackages.
package sandtypes

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/banksean/sand/internal/applecontainer/types"
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

type HookFunc func(ctx context.Context, shellCmd string, args ...string) (string, error)

type HookStreamer interface {
	Exec(ctx context.Context, shellCmd string, args ...string) (string, error)
	ExecStream(ctx context.Context, stdout, stderr io.Writer, shellCmd string, args ...string) error
}

// ContainerHook allows callers to inject container customisation step.
type ContainerHook interface {
	Name() string
	Run(ctx context.Context, ctr *types.Container, exec HookStreamer) error
}

type containerHook struct {
	name string
	fn   func(ctx context.Context, ctr *types.Container, exec HookStreamer) error
}

func (h containerHook) Name() string {
	return h.name
}

func (h containerHook) Run(ctx context.Context, ctr *types.Container, exec HookStreamer) error {
	return h.fn(ctx, ctr, exec)
}

// NewContainerHook helps callers construct hook instances without exporting internals.
func NewContainerHook(name string, fn func(ctx context.Context, ctr *types.Container, exec HookStreamer) error) ContainerHook {
	return containerHook{name: name, fn: fn}
}
