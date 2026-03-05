package box

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/cloning"
	"github.com/banksean/sand/sandtypes"
	"github.com/banksean/sand/sshimmer"
)

const (
	DefaultImageName = "ghcr.io/banksean/sand/default:latest"
)

// Box is a "sandbox" - it represents the connection between
// - a local filesystem clone of a local dev workspace directory
// - a local container instance (whose state is managed by a separate container service)
//
// At startup, the sand.Mux server will synchronize its internal database with the current
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
	// Mounts defines bind mounts that should be attached when creating the container.
	Mounts []sandtypes.MountSpec
	// SandboxWorkDirError and SandboxContainerError are the most recently updated error states of the sandbox
	// work dir and container instance. In-memory only. Updated once either at
	// server startup or sandbox creation time, and then updated periodically thereafter.
	// Empty string implies things are ok.
	// TODO: Make sandbox operations conditional on these values, so that e.g. you don't try to start
	// a sandbox container instance if the sandbox's work dir is not available.
	SandboxWorkDirError   string
	SandboxContainerError string
	// ContainerHooks run after the container has started to perform any bootstrap logic.
	ContainerHooks []sandtypes.ContainerStartupHook `json:"-"`
	Container      *types.Container
	Keys           *sshimmer.Keys
}

// Sync checks to see if the SandboxWorkDir exists, an d sets SandboxWorkDirError if not.
// TODO: Move this method to something in mux/internal/boxer/...
func (sb *Box) Sync(ctx context.Context) error {
	fi, err := os.Stat(sb.SandboxWorkDir)
	if err != nil || !fi.IsDir() {
		slog.ErrorContext(ctx, "Boxer.Sync SandboxWorkDir stat", "sandbox", sb.ID, "workdir", sb.SandboxWorkDir, "fi", fi, "error", err)
		sb.SandboxWorkDirError = "NO CLONE DIR"
	}

	return nil
}

// Shell executes a command in the container. The container must be in state "running".
// TODO: Remove this method.
func (sb *Box) Shell(ctx context.Context, env map[string]string, shellCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	slog.InfoContext(ctx, "Sandbox.Shell", "sandbox", sb.ID, "shellCmd", shellCmd)
	return fmt.Errorf("Don't call Box.Shell. Use e.g. applecontainer.NewContainerService().ExecStream(...) on the host OS")
}

// Exec executes a command in the container. The container must be in state "running".
// TODO: Remove this method.
func (sb *Box) Exec(ctx context.Context, shellCmd string, args ...string) (string, error) {
	return "", fmt.Errorf("Don't call Box.Exec. Use e.g. applecontainer.NewContainerService().Exec(...) on the host OS.  ")
}

func (sb *Box) EffectiveMounts() []sandtypes.MountSpec {
	if len(sb.Mounts) > 0 {
		return sb.Mounts
	}
	if sb.SandboxWorkDir == "" {
		return nil
	}

	// Fallback: reconstruct mounts from PathRegistry
	pathRegistry := cloning.NewStandardPathRegistry(sb.SandboxWorkDir)
	baseConfig := cloning.NewBaseContainerConfiguration()
	return baseConfig.GetMounts(cloning.CloneArtifacts{
		SandboxWorkDir: sb.SandboxWorkDir,
		PathRegistry:   pathRegistry,
	})
}
