package cloning

import "github.com/banksean/sand/internal/sandtypes"

// ContainerConfiguration handles container runtime configuration such as
// mount specifications and startup hooks.
type ContainerConfiguration interface {
	// GetMounts returns the mount specifications for the container based on the artifacts.
	GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec

	// GetStartupHooks returns hooks that should run after the container starts.
	GetStartupHooks(artifacts CloneArtifacts) []sandtypes.ContainerStartupHook
}
