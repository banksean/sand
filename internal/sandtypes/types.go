// Package sandtypes contains shared types used across the sand codebase.
// It exists as a separate package to avoid import cycles between the main sand package and its subpackages.
package sandtypes

import (
	"context"
	"fmt"
	"io"
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

const (
	MountKindBind  = "bind"
	MountKindClone = "clone"
)

// MountRequest records a user-requested bind mount and the effective runtime
// mount argument passed to the container CLI.
type MountRequest struct {
	// Kind identifies how the mount source is handled. Supported values are
	// MountKindBind and MountKindClone.
	Kind string `json:"kind"`

	// Original is the exact user-facing mount flag value, before sand resolves
	// clone paths or rewrites anything for the container runtime.
	Original string `json:"original"`

	// Source is the host directory provided by the user. For MountKindClone,
	// sand copies this directory before rendering Runtime.
	Source string `json:"source,omitempty"`

	// Clone is the sand-managed CoW clone path used as the runtime host source
	// for MountKindClone. It is empty for MountKindBind.
	Clone string `json:"clone,omitempty"`

	// Target is the container path where the bind mount is attached.
	Target string `json:"target,omitempty"`

	// ReadOnly records whether the mount should be attached read-only.
	ReadOnly bool `json:"readOnly,omitempty"`

	// Runtime is the effective --mount argument passed to the container CLI. For
	// MountKindBind it uses Source; for MountKindClone it uses Clone.
	Runtime string `json:"runtime"`
}

func RuntimeMountRequests(requests []MountRequest) []string {
	if len(requests) == 0 {
		return nil
	}
	mounts := make([]string, 0, len(requests))
	for _, request := range requests {
		mounts = append(mounts, request.Runtime)
	}
	return mounts
}

type HookFunc func(ctx context.Context, shellCmd string, args ...string) (string, error)

type HookStreamer interface {
	Exec(ctx context.Context, shellCmd string, args ...string) (string, error)
	ExecStream(ctx context.Context, stdout, stderr io.Writer, shellCmd string, args ...string) error
	ExecStreamInput(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, shellCmd string, args ...string) error
}

// ContainerHook allows callers to inject container customisation step.
type ContainerHook interface {
	Name() string
	Run(ctx context.Context, ctr *Container, exec HookStreamer) error
}

type containerHook struct {
	name string
	fn   func(ctx context.Context, ctr *Container, exec HookStreamer) error
}

func (h containerHook) Name() string {
	return h.name
}

func (h containerHook) Run(ctx context.Context, ctr *Container, exec HookStreamer) error {
	return h.fn(ctx, ctr, exec)
}

// NewContainerHook helps callers construct hook instances without exporting internals.
func NewContainerHook(name string, fn func(ctx context.Context, ctr *Container, exec HookStreamer) error) ContainerHook {
	return containerHook{name: name, fn: fn}
}
