package sand

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMuxHTTPPing(t *testing.T) {
	// Create a temporary directory for the mux
	tmpDir, err := os.MkdirTemp("", "mux-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a sandboxer (can be minimal for ping test)
	sber := NewSandBoxer(tmpDir, nil)

	// Create and start mux
	mux := NewMuxServer(tmpDir, sber)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the mux server in a goroutine
	go func() {
		if err := mux.ServeUnix(ctx); err != nil {
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
	client, err := mux.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test ping via HTTP
	var resp map[string]string
	if err := client.doRequest(ctx, "GET", "/ping", nil, &resp); err != nil {
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

	// Create a sandboxer
	sber := NewSandBoxer(tmpDir, nil)

	// Create and start mux
	mux := NewMuxServer(tmpDir, sber)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the mux server
	go func() {
		if err := mux.ServeUnix(ctx); err != nil {
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
	client, err := mux.NewClient(ctx)
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

func TestMuxPingNotRunning(t *testing.T) {
	// Create a temporary directory for the mux
	tmpDir, err := os.MkdirTemp("", "mux-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a sandboxer
	sber := NewSandBoxer(tmpDir, nil)

	// Create mux but don't start it
	mux := NewMuxServer(tmpDir, sber)
	ctx := context.Background()

	// Try to create a client when daemon is not running
	client, err := mux.NewClient(ctx)
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
