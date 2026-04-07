package cloning

import (
	"context"
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
	ID          string
	HostWorkDir string
	EnvFile     string
}

// CloneArtifacts describes the file system artifacts created during workspace preparation.
type CloneArtifacts struct {
	// SandboxWorkDir is the root directory on the host containing all sandbox files
	SandboxWorkDir string
	// PathRegistry provides structured access to all paths within the sandbox
	PathRegistry PathRegistry
	// Username is the host OS username of the user who invoked sand
	Username string
	Uid      string
}
