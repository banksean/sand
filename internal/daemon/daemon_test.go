package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/hostops"
)

// newDaemonForTest creates a Daemon with a cross-platform test Boxer pre-injected,
// avoiding the darwin-specific NewBoxer constructor.
func newDaemonForTest(t *testing.T, appDir string) *Daemon {
	t.Helper()
	b, err := boxer.NewBoxerWithDeps(appDir, boxer.BoxerDeps{
		ContainerService: &hostops.MockContainerOps{},
	})
	if err != nil {
		t.Fatalf("NewBoxerWithDeps: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return NewDaemonWithBoxer(appDir, "test", b)
}

func TestMuxHTTPPing(t *testing.T) {
	// Create a temporary directory for the mux
	tmpDir, err := os.MkdirTemp("", "mux-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create and start mux
	mux := newDaemonForTest(t, tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the mux server in a goroutine
	go func() {
		if err := mux.ServeUnixSocket(ctx); err != nil {
			t.Logf("Mux serve error: %v", err)
		}
	}()

	// Wait for the socket to be ready
	socketPath := filepath.Join(tmpDir, defaultSocketFile)
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

func TestMuxHTTPList(t *testing.T) {
	// Create a temporary directory for the mux
	tmpDir, err := os.MkdirTemp("", "mux-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create and start mux
	mux := newDaemonForTest(t, tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the mux server
	go func() {
		if err := mux.ServeUnixSocket(ctx); err != nil {
			t.Logf("Mux serve error: %v", err)
		}
	}()

	// Wait for the socket to be ready
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(mux.SocketPath); err == nil {
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

func TestMuxHTTPVersion(t *testing.T) {
	// Create a temporary directory for the mux
	tmpDir, err := os.MkdirTemp("", "mux-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create and start mux
	mux := newDaemonForTest(t, tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the mux server in a goroutine
	go func() {
		if err := mux.ServeUnixSocket(ctx); err != nil {
			t.Logf("Mux serve error: %v", err)
		}
	}()

	// Wait for the socket to be ready
	socketPath := filepath.Join(tmpDir, defaultSocketFile)
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

func TestMuxPingNotRunning(t *testing.T) {
	// Create a temporary directory for the mux
	tmpDir, err := os.MkdirTemp("", "mux-test-*")
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
