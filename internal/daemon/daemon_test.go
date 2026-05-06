package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimepaths"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/version"
)

// newDaemonForTest creates a Daemon with a cross-platform test Boxer pre-injected,
// avoiding the darwin-specific NewBoxer constructor.
func newDaemonForTest(t *testing.T, appDir string) *Daemon {
	t.Helper()
	b, err := boxer.NewBoxerWithDeps(appDir, boxer.BoxerDeps{
		ContainerService: &hostops.MockContainerOps{},
		ImageService: &testImageOps{
			ListFunc: func(context.Context) ([]types.ImageEntry, error) {
				return []types.ImageEntry{{Reference: "test-image:latest"}}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBoxerWithDeps: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return NewDaemonWithBoxer(appDir, "test", b)
}

type testImageOps struct {
	ListFunc    func(context.Context) ([]types.ImageEntry, error)
	PullFunc    func(context.Context, string, io.Writer) (func() error, error)
	InspectFunc func(context.Context, string) ([]*types.ImageManifest, error)
}

func (m *testImageOps) List(ctx context.Context) ([]types.ImageEntry, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx)
	}
	return nil, nil
}

func (m *testImageOps) Pull(ctx context.Context, image string, w io.Writer) (func() error, error) {
	if m.PullFunc != nil {
		return m.PullFunc(ctx, image, w)
	}
	return func() error { return nil }, nil
}

func (m *testImageOps) Inspect(ctx context.Context, name string) ([]*types.ImageManifest, error) {
	if m.InspectFunc != nil {
		return m.InspectFunc(ctx, name)
	}
	return nil, nil
}

func TestDaemonStartsGRPCSocketOnlyForHostIPC(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dmn-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dmn := newDaemonForTest(t, tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		if err := dmn.ServeUnixSocket(ctx); err != nil {
			t.Logf("Mux serve error: %v", err)
		}
	}()

	waitForSocket(t, filepath.Join(tmpDir, DefaultGRPCSocketFile))
	assertSocketMode(t, filepath.Join(tmpDir, DefaultGRPCSocketFile), socketFileMode)
	if _, err := os.Stat(filepath.Join(tmpDir, defaultHTTPSocketFile)); !os.IsNotExist(err) {
		t.Fatalf("host HTTP socket exists after startup, stat err = %v", err)
	}

	client, err := NewUnixSocketGRPCClient(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("gRPC Ping() failed: %v", err)
	}

	versionInfo, err := client.Version(ctx)
	if err != nil {
		t.Fatalf("gRPC Version() failed: %v", err)
	}
	if !versionInfo.Equal(version.Get()) {
		t.Fatalf("gRPC Version() = %+v, want current version info", versionInfo)
	}
	t.Logf("gRPC version info: %+v", versionInfo)

	dmn.Shutdown(ctx)
}

func TestDaemonGRPCEnsureImage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dmn-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dmn := newDaemonForTest(t, tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		if err := dmn.ServeUnixSocket(ctx); err != nil {
			t.Logf("Mux serve error: %v", err)
		}
	}()
	waitForSocket(t, filepath.Join(tmpDir, DefaultGRPCSocketFile))

	client, err := NewUnixSocketGRPCClient(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer client.Close()

	if err := client.EnsureImage(ctx, "test-image:latest", io.Discard); err != nil {
		t.Fatalf("gRPC EnsureImage() failed: %v", err)
	}

	dmn.Shutdown(ctx)
}

func TestDaemonGRPCCreateSandboxStreamsError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dmn-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dmn := newDaemonForTest(t, tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		if err := dmn.ServeUnixSocket(ctx); err != nil {
			t.Logf("Mux serve error: %v", err)
		}
	}()
	waitForSocket(t, filepath.Join(tmpDir, DefaultGRPCSocketFile))

	client, err := NewUnixSocketGRPCClient(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer client.Close()

	box, err := client.CreateSandbox(ctx, CreateSandboxOpts{
		ID:    "test-sandbox",
		Agent: "missing-agent",
	}, io.Discard)
	if err == nil {
		t.Fatal("gRPC CreateSandbox() error = nil, want error")
	}
	if box != nil {
		t.Fatalf("gRPC CreateSandbox() box = %#v, want nil", box)
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("gRPC CreateSandbox() error = %q, want unknown agent", err)
	}

	dmn.Shutdown(ctx)
}

func TestDaemonCreatesContainerHTTPAndGRPCSockets(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dmn-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dmn := newDaemonForTest(t, tmpDir)
	httpListener, grpcListener, err := dmn.createContainerSockets(context.Background(), "test-sandbox")
	if err != nil {
		t.Fatalf("createContainerSockets() error = %v", err)
	}
	defer httpListener.Close()
	defer grpcListener.Close()

	httpSocketPath := runtimepaths.ContainerHTTPSocketPath("test-sandbox")
	grpcSocketPath := runtimepaths.ContainerGRPCSocketPath("test-sandbox")
	t.Cleanup(func() {
		_ = os.Remove(httpSocketPath)
		_ = os.Remove(grpcSocketPath)
	})
	if _, err := os.Stat(httpSocketPath); err != nil {
		t.Fatalf("HTTP container socket was not created: %v", err)
	}
	if _, err := os.Stat(grpcSocketPath); err != nil {
		t.Fatalf("gRPC container socket was not created: %v", err)
	}
	assertSocketMode(t, httpSocketPath, socketFileMode)
	assertSocketMode(t, grpcSocketPath, socketFileMode)
}

func TestStartSandboxRecreatesStoppedContainerAfterSocketCreation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sdt-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	ctx := context.Background()
	oldContainerID := "old-container"
	newContainerID := "new-container"
	sandboxID := "rb"
	httpSocketPath := runtimepaths.ContainerHTTPSocketPath(sandboxID)
	grpcSocketPath := runtimepaths.ContainerGRPCSocketPath(sandboxID)
	t.Cleanup(func() {
		_ = os.Remove(httpSocketPath)
		_ = os.Remove(grpcSocketPath)
	})
	var stopCalls []string
	var deleteCalls []string
	var startCalls []string

	containerSvc := &hostops.MockContainerOps{
		InspectFunc: func(_ context.Context, containerID string) ([]types.Container, error) {
			switch containerID {
			case oldContainerID:
				return []types.Container{{
					Status: "stopped",
					Configuration: types.ContainerConfig{
						SSH: false,
					},
				}}, nil
			case newContainerID:
				return []types.Container{{
					Status: "stopped",
					Configuration: types.ContainerConfig{
						SSH: true,
					},
				}}, nil
			default:
				return nil, nil
			}
		},
		CreateFunc: func(_ context.Context, opts *options.CreateContainer, _ string, _ []string) (string, error) {
			if !opts.ManagementOptions.SSH {
				t.Fatal("recreated container did not enable ssh-agent forwarding")
			}
			if _, err := os.Stat(httpSocketPath); err != nil {
				t.Fatalf("HTTP socket did not exist before Create: %v", err)
			}
			if _, err := os.Stat(grpcSocketPath); err != nil {
				t.Fatalf("gRPC socket did not exist before Create: %v", err)
			}
			return newContainerID, nil
		},
		StopFunc: func(_ context.Context, _ *options.StopContainer, containerID string) (string, error) {
			stopCalls = append(stopCalls, containerID)
			return "stopped", nil
		},
		DeleteFunc: func(_ context.Context, _ *options.DeleteContainer, containerID string) (string, error) {
			deleteCalls = append(deleteCalls, containerID)
			return "deleted", nil
		},
		StartFunc: func(_ context.Context, _ *options.StartContainer, containerID string) (string, error) {
			startCalls = append(startCalls, containerID)
			return "started", nil
		},
	}

	registry := cloning.NewAgentRegistry()
	registry.Register(&cloning.AgentConfig{
		Name:          "default",
		Configuration: noHookContainerConfig{},
	})
	b, err := boxer.NewBoxerWithDeps(tmpDir, boxer.BoxerDeps{
		ContainerService: containerSvc,
		ImageService:     &testImageOps{},
		GitOps:           &hostops.MockGitOps{},
		AgentRegistry:    registry,
	})
	if err != nil {
		t.Fatalf("NewBoxerWithDeps: %v", err)
	}
	defer b.Close()

	if err := b.SaveSandbox(ctx, &sandtypes.Box{
		ID:             sandboxID,
		AgentType:      "default",
		ContainerID:    oldContainerID,
		HostOriginDir:  t.TempDir(),
		SandboxWorkDir: t.TempDir(),
		ImageName:      "test-image:latest",
	}); err != nil {
		t.Fatalf("SaveSandbox: %v", err)
	}

	dmn := NewDaemonWithBoxer(tmpDir, "test", b)
	if err := dmn.StartSandbox(ctx, StartSandboxOpts{ID: sandboxID, SSHAgent: true}); err != nil {
		t.Fatalf("StartSandbox() error = %v", err)
	}

	if len(stopCalls) != 1 || stopCalls[0] != oldContainerID {
		t.Fatalf("Stop calls = %v, want [%s]", stopCalls, oldContainerID)
	}
	if len(deleteCalls) != 1 || deleteCalls[0] != oldContainerID {
		t.Fatalf("Delete calls = %v, want [%s]", deleteCalls, oldContainerID)
	}
	if len(startCalls) != 1 || startCalls[0] != newContainerID {
		t.Fatalf("Start calls = %v, want [%s]", startCalls, newContainerID)
	}
	loaded, err := b.Get(ctx, sandboxID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.ContainerID != newContainerID {
		t.Fatalf("ContainerID = %q, want %q", loaded.ContainerID, newContainerID)
	}
}

type noHookContainerConfig struct{}

func (noHookContainerConfig) GetMounts(cloning.CloneArtifacts) []sandtypes.MountSpec {
	return nil
}

func (noHookContainerConfig) GetFirstStartHooks(cloning.CloneArtifacts) []sandtypes.ContainerHook {
	return nil
}

func (noHookContainerConfig) GetStartHooks(cloning.CloneArtifacts) []sandtypes.ContainerHook {
	return nil
}

func TestDaemonGRPCList(t *testing.T) {
	// Create a temporary directory for the dmn
	tmpDir, err := os.MkdirTemp("", "dmn-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create and start dmn
	dmn := newDaemonForTest(t, tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the dmn server
	go func() {
		if err := dmn.ServeUnixSocket(ctx); err != nil {
			t.Logf("Mux serve error: %v", err)
		}
	}()

	waitForSocket(t, filepath.Join(tmpDir, DefaultGRPCSocketFile))

	// Create a client
	client, err := NewUnixSocketClient(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test list (should be empty initially)
	boxes, err := client.ListSandboxes(ctx)
	if err != nil {
		t.Fatalf("List request failed: %v", err)
	}

	if boxes == nil {
		t.Errorf("Expected empty list, got nil")
	}

	// Cleanup
	if err := client.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}

func waitForSocket(t *testing.T, socketPath string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created", socketPath)
}

func assertSocketMode(t *testing.T, socketPath string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket %s: %v", socketPath, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("socket %s mode = %o, want %o", socketPath, got, want)
	}
}

func TestDaemonGRPCVersion(t *testing.T) {
	// Create a temporary directory for the dmn
	tmpDir, err := os.MkdirTemp("", "dmn-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create and start dmn
	dmn := newDaemonForTest(t, tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the dmn server in a goroutine
	go func() {
		if err := dmn.ServeUnixSocket(ctx); err != nil {
			t.Logf("Mux serve error: %v", err)
		}
	}()

	waitForSocket(t, filepath.Join(tmpDir, DefaultGRPCSocketFile))

	// Create a client
	client, err := NewUnixSocketClient(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test version endpoint
	versionInfo, err := client.Version(ctx)
	if err != nil {
		t.Fatalf("Version request failed: %v", err)
	}

	// Version info should at least be present (may be empty in tests)
	t.Logf("Version info: %+v", versionInfo)

	// Test shutdown
	if err := client.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}

func TestDaemonPingNotRunning(t *testing.T) {
	// Create a temporary directory for the dmn
	tmpDir, err := os.MkdirTemp("", "dmn-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Try to create a client when daemon is not running
	client, err := NewUnixSocketClient(ctx, tmpDir)
	if err != nil {
		// Creating client itself might succeed, but ping should fail
		t.Logf("NewClient returned error (expected): %v", err)
		return
	}

	// Ping should fail when daemon is not running
	err = client.Ping(ctx)
	if err == nil {
		t.Fatalf("Expected Ping to fail when daemon is not running")
	}
	t.Logf("Ping failed as expected: %v", err)
}
