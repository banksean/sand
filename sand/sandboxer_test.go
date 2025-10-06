package sand

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveSandbox(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a SandBoxer
	sb := NewSandBoxer(tmpDir, nil)

	// Create a test sandbox
	testSandbox := &Sandbox{
		ID:             "test-id",
		ContainerID:    "container-123",
		HostOriginDir:  "/tmp/host-origin",
		SandboxWorkDir: filepath.Join(tmpDir, "test-id"),
		ImageName:      "test-image",
		DNSDomain:      "test.local",
		EnvFile:        "/tmp/.env",
	}

	// Create the sandbox directory
	if err := os.MkdirAll(testSandbox.SandboxWorkDir, 0755); err != nil {
		t.Fatalf("Failed to create sandbox work dir: %v", err)
	}

	// Save the sandbox
	ctx := context.Background()
	if err := sb.SaveSandbox(ctx, testSandbox); err != nil {
		t.Fatalf("Failed to save sandbox: %v", err)
	}

	// Verify the file exists
	sandboxPath := filepath.Join(tmpDir, "test-id", "sandbox.json")
	if _, err := os.Stat(sandboxPath); os.IsNotExist(err) {
		t.Fatalf("sandbox.json was not created")
	}

	// Read and verify the content
	data, err := os.ReadFile(sandboxPath)
	if err != nil {
		t.Fatalf("Failed to read sandbox.json: %v", err)
	}

	var loadedSandbox Sandbox
	if err := json.Unmarshal(data, &loadedSandbox); err != nil {
		t.Fatalf("Failed to unmarshal sandbox.json: %v", err)
	}

	// Verify all fields match
	if loadedSandbox.ID != testSandbox.ID {
		t.Errorf("ID mismatch: got %s, want %s", loadedSandbox.ID, testSandbox.ID)
	}
	if loadedSandbox.ContainerID != testSandbox.ContainerID {
		t.Errorf("ContainerID mismatch: got %s, want %s", loadedSandbox.ContainerID, testSandbox.ContainerID)
	}
	if loadedSandbox.HostOriginDir != testSandbox.HostOriginDir {
		t.Errorf("HostOriginDir mismatch: got %s, want %s", loadedSandbox.HostOriginDir, testSandbox.HostOriginDir)
	}
	if loadedSandbox.SandboxWorkDir != testSandbox.SandboxWorkDir {
		t.Errorf("SandboxWorkDir mismatch: got %s, want %s", loadedSandbox.SandboxWorkDir, testSandbox.SandboxWorkDir)
	}
	if loadedSandbox.ImageName != testSandbox.ImageName {
		t.Errorf("ImageName mismatch: got %s, want %s", loadedSandbox.ImageName, testSandbox.ImageName)
	}
	if loadedSandbox.DNSDomain != testSandbox.DNSDomain {
		t.Errorf("DNSDomain mismatch: got %s, want %s", loadedSandbox.DNSDomain, testSandbox.DNSDomain)
	}
	if loadedSandbox.EnvFile != testSandbox.EnvFile {
		t.Errorf("EnvFile mismatch: got %s, want %s", loadedSandbox.EnvFile, testSandbox.EnvFile)
	}
}

func TestLoadSandbox(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a SandBoxer
	sb := NewSandBoxer(tmpDir, nil)

	// Create a test sandbox directory
	testID := "test-load-id"
	sandboxDir := filepath.Join(tmpDir, testID)
	if err := os.MkdirAll(sandboxDir, 0755); err != nil {
		t.Fatalf("Failed to create sandbox dir: %v", err)
	}

	// Create a test sandbox struct
	testSandbox := &Sandbox{
		ID:             testID,
		ContainerID:    "container-456",
		HostOriginDir:  "/tmp/host-load",
		SandboxWorkDir: sandboxDir,
		ImageName:      "load-image",
		DNSDomain:      "load.local",
		EnvFile:        "/tmp/.env.load",
	}

	// Manually write the JSON file
	data, err := json.MarshalIndent(testSandbox, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal test sandbox: %v", err)
	}

	sandboxPath := filepath.Join(sandboxDir, "sandbox.json")
	if err := os.WriteFile(sandboxPath, data, 0644); err != nil {
		t.Fatalf("Failed to write test sandbox.json: %v", err)
	}

	// Load the sandbox
	ctx := context.Background()
	loadedSandbox, err := sb.loadSandbox(ctx, testID)
	if err != nil {
		t.Fatalf("Failed to load sandbox: %v", err)
	}

	// Verify all fields match
	if loadedSandbox.ID != testSandbox.ID {
		t.Errorf("ID mismatch: got %s, want %s", loadedSandbox.ID, testSandbox.ID)
	}
	if loadedSandbox.ContainerID != testSandbox.ContainerID {
		t.Errorf("ContainerID mismatch: got %s, want %s", loadedSandbox.ContainerID, testSandbox.ContainerID)
	}
	if loadedSandbox.HostOriginDir != testSandbox.HostOriginDir {
		t.Errorf("HostOriginDir mismatch: got %s, want %s", loadedSandbox.HostOriginDir, testSandbox.HostOriginDir)
	}
	if loadedSandbox.SandboxWorkDir != testSandbox.SandboxWorkDir {
		t.Errorf("SandboxWorkDir mismatch: got %s, want %s", loadedSandbox.SandboxWorkDir, testSandbox.SandboxWorkDir)
	}
	if loadedSandbox.ImageName != testSandbox.ImageName {
		t.Errorf("ImageName mismatch: got %s, want %s", loadedSandbox.ImageName, testSandbox.ImageName)
	}
	if loadedSandbox.DNSDomain != testSandbox.DNSDomain {
		t.Errorf("DNSDomain mismatch: got %s, want %s", loadedSandbox.DNSDomain, testSandbox.DNSDomain)
	}
	if loadedSandbox.EnvFile != testSandbox.EnvFile {
		t.Errorf("EnvFile mismatch: got %s, want %s", loadedSandbox.EnvFile, testSandbox.EnvFile)
	}
}

func TestLoadSandboxNotFound(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a SandBoxer
	sb := NewSandBoxer(tmpDir, nil)

	// Try to load a non-existent sandbox
	ctx := context.Background()
	_, err = sb.loadSandbox(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("Expected error when loading non-existent sandbox, got nil")
	}
}
