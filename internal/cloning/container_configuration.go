package cloning

import "github.com/banksean/sand/internal/sandtypes"

// ContainerConfiguration handles container runtime configuration such as
// mount specifications and first-time startup hooks.
type ContainerConfiguration interface {
	// GetMounts returns the mount specifications for the container based on the artifacts.
	GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec

	// GetFirstStartHooks returns hooks that should run after the first time the container starts.
	GetFirstStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook

	// GetStartHooks returns hooks that should run after starting the container any time after the first start.
	GetStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook
}
