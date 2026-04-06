// Package daemontest provides helpers for starting an in-memory daemon with
// injectable dependencies, for use in integration tests.
package daemontest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
)

// Deps holds the injectable dependencies for a test daemon.
// Any nil field is replaced with a no-op mock before startup.
type Deps struct {
	ContainerService hostops.ContainerOps
	GitOps           hostops.GitOps
	FileOps          hostops.FileOps
}

// SandboxStore allows pre-populating the daemon's sandbox database before tests run.
type SandboxStore interface {
	SaveSandbox(ctx context.Context, box *sandtypes.Box) error
}

// StartDaemon starts an in-memory daemon with the given Deps and returns a connected Client.
// configure, if non-nil, is called with the sandbox store before the daemon begins serving,
// allowing callers to seed the database with test fixtures.
// The daemon is shut down when the test ends.
func StartDaemon(t testing.TB, deps Deps, configure func(context.Context, SandboxStore)) daemon.Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tmpDir := t.TempDir()

	if deps.ContainerService == nil {
		deps.ContainerService = &hostops.MockContainerOps{}
	}
	if deps.GitOps == nil {
		deps.GitOps = &hostops.MockGitOps{}
	}
	if deps.FileOps == nil {
		deps.FileOps = &hostops.MockFileOps{}
	}

	b, err := boxer.NewBoxerWithDeps(tmpDir, boxer.BoxerDeps{
		ContainerService: deps.ContainerService,
		GitOps:           deps.GitOps,
		FileOps:          deps.FileOps,
		AgentRegistry:    defaultRegistry(),
	})
	if err != nil {
		t.Fatalf("daemontest: NewBoxerWithDeps: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	if configure != nil {
		configure(ctx, b)
	}

	d := daemon.NewDaemonWithBoxer(tmpDir, "test", b)
	// ServeUnixSocket blocks until Shutdown is called, so run it in a goroutine.
	go func() {
		if err := d.ServeUnixSocket(ctx); err != nil {
			t.Logf("daemontest: ServeUnixSocket: %v", err)
		}
	}()
	// Shut down the daemon when the test ends. Use a fresh context since the
	// test context may already be cancelled by the time cleanup runs.
	t.Cleanup(func() { d.Shutdown(context.Background()) })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(d.SocketPath); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	client, err := daemon.NewUnixSocketClient(ctx, tmpDir)
	if err != nil {
		t.Fatalf("daemontest: NewUnixSocketClient: %v", err)
	}
	return client
}

// defaultRegistry returns an AgentRegistry with the "default" agent pre-registered
// using the base container configuration (no mounts from scratch, no-op exec hooks).
func defaultRegistry() *cloning.AgentRegistry {
	r := cloning.NewAgentRegistry()
	r.Register(&cloning.AgentConfig{
		Name:          "default",
		Configuration: cloning.NewBaseContainerConfiguration(),
	})
	return r
}
