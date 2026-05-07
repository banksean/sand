package daemon

import (
	"context"
	"io"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/version"
)

// Client invokes methods on the sandd process via gRPC over Unix sockets,
// whether the client is running on the host or inside a sandbox.
type Client interface {
	Ping(ctx context.Context) error
	Version(ctx context.Context) (version.Info, error)
	Shutdown(ctx context.Context) error
	LogSandbox(ctx context.Context, name string, w io.Writer) error
	ListSandboxes(ctx context.Context) ([]sandtypes.Box, error)
	GetSandbox(ctx context.Context, name string) (*sandtypes.Box, error)
	RemoveSandbox(ctx context.Context, name string) error
	StopSandbox(ctx context.Context, name string) error
	StartSandbox(ctx context.Context, opts StartSandboxOpts) error
	SyncHostGitMirror(ctx context.Context, name string) (string, error)
	ResolveAgentLaunchEnv(ctx context.Context, agent, envFile string) (map[string]string, error)
	ExportImage(ctx context.Context, name, imageName string) error
	Stats(ctx context.Context, name ...string) ([]types.ContainerStats, error)
	VSC(ctx context.Context, name string) error
	CreateSandbox(ctx context.Context, opts CreateSandboxOpts, w io.Writer) (*sandtypes.Box, error)
	// EnsureImage ensures imageName is present locally and up to date, pulling if needed.
	// Progress lines from the daemon are written to w as they arrive.
	EnsureImage(ctx context.Context, imageName string, w io.Writer) error
}

func NewUnixSocketClient(ctx context.Context, appBaseDir string) (Client, error) {
	return NewUnixSocketGRPCClient(ctx, appBaseDir)
}
