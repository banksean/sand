package containerruntime

import "github.com/banksean/sand/internal/sandtypes"

// Artifacts describes the host-side paths and metadata needed to configure a
// sandbox container.
type Artifacts struct {
	SandboxWorkDir    string
	WorkDir           string
	DotfilesDir       string
	SSHKeysDir        string
	HostGitMirrorDir  string
	Username          string
	Uid               string
	SharedCacheMounts sandtypes.SharedCacheMounts
}

// ContainerConfiguration handles container runtime configuration such as
// mount specifications and first-time startup hooks.
type ContainerConfiguration interface {
	// GetMounts returns the mount specifications for the container based on the artifacts.
	GetMounts(artifacts Artifacts) []sandtypes.MountSpec

	// GetFirstStartHooks returns hooks that should run after the first time the container starts.
	GetFirstStartHooks(artifacts Artifacts) []sandtypes.ContainerHook

	// GetStartHooks returns hooks that should run after starting the container any time after the first start.
	GetStartHooks(artifacts Artifacts) []sandtypes.ContainerHook
}
