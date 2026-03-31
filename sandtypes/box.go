package sandtypes

import (
	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/sshimmer"
)

// Box is a "sandbox" - it represents the connection between
// - a local filesystem clone of a local dev workspace directory
// - a local container instance (whose state is managed by a separate container service)
//
// At startup, the sand/daemon#Daemon server will synchronize its internal database with the current
// observed state of the local filesystem clone root and the local container service.
//
// TODO: Move this struct to package sandtypes, but make sure all the instances of it are treated as dumb structs first.
type Box struct {
	// ID is an opaque identifier for the sandbox
	ID string
	// AgentType identifies which agent configuration to use (default, claude, opencode)
	AgentType string
	// ContainerID is the ID of the container
	ContainerID string
	// HostOriginDir is the origin of the sandbox, from which we clone its contents
	HostOriginDir string
	// SandboxWorkDir is the host OS filesystem path containing the sandbox's c-o-w clone of hostOriginDir.
	SandboxWorkDir string
	// ImageName is the name of the container image
	ImageName string
	// DNSDomain is the dns domain for the sandbox's network
	DNSDomain string
	// EnvFile is the host filesystem path to the env file to use when executing commands in the container
	EnvFile string
	// AllowedDomains is the list of domains the sandbox container is permitted to contact.
	// When non-empty, this overrides the default allowlist baked into the init image.
	AllowedDomains []string
	// Mounts defines bind mounts that should be attached when creating the container.
	Mounts []MountSpec
	// Volumes defines volume mounts in host:container format, passed directly to the container runtime.
	Volumes []string
	// SandboxWorkDirError and SandboxContainerError are the most recently updated error states of the sandbox
	// work dir and container instance. In-memory only. Updated once either at
	// server startup or sandbox creation time, and then updated periodically thereafter.
	// Empty string implies things are ok.
	// TODO: Make sandbox operations conditional on these values, so that e.g. you don't try to start
	// a sandbox container instance if the sandbox's work dir is not available.
	SandboxWorkDirError   string
	SandboxContainerError string
	// ContainerHooks run after the container has started to perform any bootstrap logic.
	ContainerHooks     []ContainerStartupHook `json:"-"`
	OriginalGitDetails *GitDetails
	CurrentGitDetails  *GitDetails
	Container          *types.Container
	Keys               *sshimmer.Keys
}

type GitDetails struct {
	RemoteOrigin string
	Branch       string
	Commit       string
	IsDirty      bool
}
