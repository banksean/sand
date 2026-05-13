package cloning

import (
	"context"

	"github.com/banksean/sand/internal/sandtypes"
)

// WorkspacePreparation handles the file system operations needed to prepare
// a sandbox workspace by cloning files from the host into the sandbox directory.
type WorkspacePreparation interface {
	// Prepare clones the necessary files from the host to create a sandbox workspace.
	// Returns CloneArtifacts containing paths and metadata about what was created.
	Prepare(ctx context.Context, req CloneRequest) (*CloneArtifacts, error)
}

// CloneRequest captures the inputs necessary to prepare a sandbox workspace.
type CloneRequest struct {
	ID                string
	Name              string
	HostWorkDir       string
	ProfileName       string
	Profile           sandtypes.Profile
	EnvFile           string
	Username          string
	Uid               string
	SharedCacheMounts sandtypes.SharedCacheMounts
}

// CloneArtifacts describes the file system artifacts created during workspace preparation.
type CloneArtifacts struct {
	HostWorkDir string
	// HostGitMirrorDir is the shared bare mirror for HostWorkDir when HostWorkDir is a git repository.
	HostGitMirrorDir string
	// SandboxWorkDir is the root directory on the host containing all sandbox files
	SandboxWorkDir string
	// PathRegistry provides structured access to all paths within the sandbox
	PathRegistry PathRegistry
	// Username is the host OS username of the user who invoked sand
	Username string
	Uid      string
	// SharedCacheMounts carries host-managed caches that should be mounted into the container.
	SharedCacheMounts sandtypes.SharedCacheMounts
}
