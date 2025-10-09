package sand

import (
	"context"
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
	sb, err := NewSandBoxer(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to create SandBoxer: %v", err)
	}
	defer sb.Close()

	// Create a test sandbox
	testSandbox := &Box{
		ID:             "test-id",
		ContainerID:    "container-123",
		HostOriginDir:  "/tmp/host-origin",
		SandboxWorkDir: filepath.Join(tmpDir, "test-id"),
		ImageName:      "test-image",
		DNSDomain:      "test.local",
		EnvFile:        "/tmp/.env",
	}

	// Create the sandbox directory
	if err := os.MkdirAll(testSandbox.SandboxWorkDir, 0o755); err != nil {
		t.Fatalf("Failed to create sandbox work dir: %v", err)
	}

	// Save the sandbox
	ctx := context.Background()
	if err := sb.SaveSandbox(ctx, testSandbox); err != nil {
		t.Fatalf("Failed to save sandbox: %v", err)
	}

	// Verify it was saved by retrieving it
	loadedSandbox, err := sb.Get(ctx, "test-id")
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	if loadedSandbox == nil {
		t.Fatalf("Sandbox was not saved")
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

	// Test that UpsertSandbox works (update existing)
	testSandbox.ContainerID = "updated-container-999"
	if err := sb.SaveSandbox(ctx, testSandbox); err != nil {
		t.Fatalf("Failed to update sandbox: %v", err)
	}

	updatedSandbox, err := sb.Get(ctx, "test-id")
	if err != nil {
		t.Fatalf("Failed to get updated sandbox: %v", err)
	}
	if updatedSandbox.ContainerID != "updated-container-999" {
		t.Errorf("ContainerID was not updated: got %s, want %s", updatedSandbox.ContainerID, "updated-container-999")
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
	sb, err := NewSandBoxer(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to create SandBoxer: %v", err)
	}
	defer sb.Close()

	// Create a test sandbox directory
	testID := "test-load-id"
	sandboxDir := filepath.Join(tmpDir, testID)
	if err := os.MkdirAll(sandboxDir, 0o755); err != nil {
		t.Fatalf("Failed to create sandbox dir: %v", err)
	}

	// Create a test sandbox struct
	testSandbox := &Box{
		ID:             testID,
		ContainerID:    "container-456",
		HostOriginDir:  "/tmp/host-load",
		SandboxWorkDir: sandboxDir,
		ImageName:      "load-image",
		DNSDomain:      "load.local",
		EnvFile:        "/tmp/.env.load",
	}

	// Save the sandbox using the database
	ctx := context.Background()
	if err := sb.SaveSandbox(ctx, testSandbox); err != nil {
		t.Fatalf("Failed to save test sandbox: %v", err)
	}

	// Load the sandbox
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
	sb, err := NewSandBoxer(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to create SandBoxer: %v", err)
	}
	defer sb.Close()

	// Try to load a non-existent sandbox
	ctx := context.Background()
	_, err = sb.loadSandbox(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("Expected error when loading non-existent sandbox, got nil")
	}
}

func TestListSandboxes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sb, err := NewSandBoxer(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to create SandBoxer: %v", err)
	}
	defer sb.Close()

	ctx := context.Background()

	// Initially should be empty
	boxes, err := sb.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list sandboxes: %v", err)
	}
	if len(boxes) != 0 {
		t.Errorf("Expected 0 sandboxes, got %d", len(boxes))
	}

	// Create test sandboxes
	for i := 1; i <= 3; i++ {
		sandboxDir := filepath.Join(tmpDir, "test-"+string(rune('0'+i)))
		os.MkdirAll(sandboxDir, 0o755)

		testBox := &Box{
			ID:             "test-" + string(rune('0'+i)),
			ContainerID:    "container-" + string(rune('0'+i)),
			HostOriginDir:  "/tmp/host",
			SandboxWorkDir: sandboxDir,
			ImageName:      "test-image",
		}

		if err := sb.SaveSandbox(ctx, testBox); err != nil {
			t.Fatalf("Failed to save sandbox: %v", err)
		}
	}

	// List should return 3
	boxes, err = sb.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list sandboxes: %v", err)
	}
	if len(boxes) != 3 {
		t.Errorf("Expected 3 sandboxes, got %d", len(boxes))
	}
}

func TestUpdateContainerID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sb, err := NewSandBoxer(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to create SandBoxer: %v", err)
	}
	defer sb.Close()

	ctx := context.Background()
	sandboxDir := filepath.Join(tmpDir, "test-update")
	os.MkdirAll(sandboxDir, 0o755)

	testBox := &Box{
		ID:             "test-update",
		ContainerID:    "original-container",
		HostOriginDir:  "/tmp/host",
		SandboxWorkDir: sandboxDir,
		ImageName:      "test-image",
	}

	if err := sb.SaveSandbox(ctx, testBox); err != nil {
		t.Fatalf("Failed to save sandbox: %v", err)
	}

	// Update container ID
	if err := sb.UpdateContainerID(ctx, testBox, "new-container-id"); err != nil {
		t.Fatalf("Failed to update container ID: %v", err)
	}

	// Verify the update
	loadedBox, err := sb.Get(ctx, "test-update")
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}

	if loadedBox.ContainerID != "new-container-id" {
		t.Errorf("ContainerID not updated: got %s, want %s", loadedBox.ContainerID, "new-container-id")
	}
}

func TestGetSandboxesByImage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sb, err := NewSandBoxer(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to create SandBoxer: %v", err)
	}
	defer sb.Close()

	ctx := context.Background()

	// Create sandboxes with different images
	for i := 1; i <= 5; i++ {
		sandboxDir := filepath.Join(tmpDir, "test-"+string(rune('0'+i)))
		os.MkdirAll(sandboxDir, 0o755)

		imageName := "image-a"
		if i > 2 {
			imageName = "image-b"
		}

		testBox := &Box{
			ID:             "test-" + string(rune('0'+i)),
			ContainerID:    "container-" + string(rune('0'+i)),
			HostOriginDir:  "/tmp/host",
			SandboxWorkDir: sandboxDir,
			ImageName:      imageName,
		}

		if err := sb.SaveSandbox(ctx, testBox); err != nil {
			t.Fatalf("Failed to save sandbox: %v", err)
		}
	}

	// Query by image
	sandboxes, err := sb.queries.GetSandboxesByImage(ctx, "image-a")
	if err != nil {
		t.Fatalf("Failed to get sandboxes by image: %v", err)
	}

	if len(sandboxes) != 2 {
		t.Errorf("Expected 2 sandboxes with image-a, got %d", len(sandboxes))
	}

	// Verify they're the right ones
	for _, s := range sandboxes {
		if s.ImageName != "image-a" {
			t.Errorf("Expected image-a, got %s", s.ImageName)
		}
	}
}
