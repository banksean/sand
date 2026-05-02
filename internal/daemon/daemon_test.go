package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/hostops"
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

func TestDaemonHTTPPing(t *testing.T) {
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

	// Wait for the socket to be ready
	socketPath := filepath.Join(tmpDir, DefaultSocketFile)
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Create a client
	client, err := NewUnixSocketClient(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test ping via HTTP
	var resp map[string]string
	if err := client.(*defaultClient).doRequest(ctx, "GET", "/ping", nil, &resp); err != nil {
		t.Fatalf("Ping request failed: %v", err)
	}

	if resp["status"] != "pong" {
		t.Errorf("Expected pong response, got: %v", resp)
	}

	// Test Ping() client method
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping() method failed: %v", err)
	}

	// Test shutdown
	if err := client.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}

func TestDaemonStartsHTTPAndGRPCSockets(t *testing.T) {
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

	waitForSocket(t, filepath.Join(tmpDir, DefaultSocketFile))
	waitForSocket(t, filepath.Join(tmpDir, DefaultGRPCSocketFile))
	assertSocketMode(t, filepath.Join(tmpDir, DefaultSocketFile), socketFileMode)
	assertSocketMode(t, filepath.Join(tmpDir, DefaultGRPCSocketFile), socketFileMode)

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

	httpSocketPath := filepath.Join(tmpDir, "containersockets", "test-sandbox")
	grpcSocketPath := filepath.Join(tmpDir, "containergrpc", "test-sandbox")
	if _, err := os.Stat(httpSocketPath); err != nil {
		t.Fatalf("HTTP container socket was not created: %v", err)
	}
	if _, err := os.Stat(grpcSocketPath); err != nil {
		t.Fatalf("gRPC container socket was not created: %v", err)
	}
	assertSocketMode(t, httpSocketPath, socketFileMode)
	assertSocketMode(t, grpcSocketPath, socketFileMode)
}

func TestDaemonHTTPList(t *testing.T) {
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

	// Wait for the socket to be ready
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(dmn.SocketPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

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

func TestDaemonHTTPVersion(t *testing.T) {
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

	// Wait for the socket to be ready
	socketPath := filepath.Join(tmpDir, DefaultSocketFile)
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

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
