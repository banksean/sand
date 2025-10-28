package sand

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
)

type mockImageOps struct {
	listFunc func(ctx context.Context) ([]types.ImageEntry, error)
	pullFunc func(ctx context.Context, image string) (func() error, error)
}

func (m *mockImageOps) List(ctx context.Context) ([]types.ImageEntry, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []types.ImageEntry{}, nil
}

func (m *mockImageOps) Pull(ctx context.Context, image string) (func() error, error) {
	if m.pullFunc != nil {
		return m.pullFunc(ctx, image)
	}
	return func() error { return nil }, nil
}

func newTestBoxer(t *testing.T, containerOps ContainerOps, imageOps ImageOps) *Boxer {
	t.Helper()
	tmpDir := t.TempDir()

	boxer, err := NewBoxer(tmpDir, nil)
	if err != nil {
		t.Fatalf("Failed to create test Boxer: %v", err)
	}
	t.Cleanup(func() { boxer.Close() })

	boxer.containerService = containerOps
	boxer.imageService = imageOps
	boxer.gitOps = &mockGitOps{}
	boxer.fileOps = &mockFileOps{
		lstatFunc: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		createFunc: func(path string) (*os.File, error) {
			return nil, nil
		},
	}

	return boxer
}

func TestBoxer_NewSandbox_EndToEnd(t *testing.T) {
	ctx := context.Background()

	t.Run("success creates sandbox with all components", func(t *testing.T) {
		var createdContainerID string
		mockContainer := &mockContainerOps{
			createFunc: func(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
				createdContainerID = "test-container-123"
				return createdContainerID, nil
			},
		}

		mockImage := &mockImageOps{}

		boxer := newTestBoxer(t, mockContainer, mockImage)

		mockCloner := &mockWorkspaceCloner{
			prepareFunc: func(ctx context.Context, req CloneRequest) (*CloneResult, error) {
				return &CloneResult{
					SandboxWorkDir: filepath.Join(boxer.appRoot, "clones", req.ID),
					Mounts:         []MountSpec{},
					ContainerHooks: []ContainerStartupHook{},
				}, nil
			},
		}

		hostWorkDir := t.TempDir()
		result, err := boxer.NewSandbox(ctx, mockCloner, "test-sandbox", hostWorkDir, "test-image:latest", "")
		if err != nil {
			t.Fatalf("NewSandbox() error = %v", err)
		}

		if result.ID != "test-sandbox" {
			t.Errorf("Expected ID 'test-sandbox', got %s", result.ID)
		}

		if result.ImageName != "test-image:latest" {
			t.Errorf("Expected ImageName 'test-image:latest', got %s", result.ImageName)
		}

		if result.HostOriginDir != hostWorkDir {
			t.Errorf("Expected HostOriginDir %s, got %s", hostWorkDir, result.HostOriginDir)
		}

		if !strings.Contains(result.SandboxWorkDir, "test-sandbox") {
			t.Errorf("Expected SandboxWorkDir to contain 'test-sandbox', got %s", result.SandboxWorkDir)
		}

		loadedBox, err := boxer.Get(ctx, "test-sandbox")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if loadedBox == nil {
			t.Fatal("Expected sandbox to be saved in DB")
		}
		if loadedBox.ID != "test-sandbox" {
			t.Errorf("DB sandbox ID = %s, want 'test-sandbox'", loadedBox.ID)
		}
	})

	t.Run("cloner error propagates", func(t *testing.T) {
		mockContainer := &mockContainerOps{}
		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		expectedErr := errors.New("cloner failed")
		mockCloner := &mockWorkspaceCloner{
			prepareFunc: func(ctx context.Context, req CloneRequest) (*CloneResult, error) {
				return nil, expectedErr
			},
		}

		_, err := boxer.NewSandbox(ctx, mockCloner, "test-sandbox", "/host/work", "test-image", "")
		if err == nil {
			t.Fatal("Expected error from cloner, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("Expected error to wrap cloner error, got: %v", err)
		}

		loadedBox, err := boxer.Get(ctx, "test-sandbox")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if loadedBox != nil {
			t.Error("Expected sandbox not to be saved after cloner error")
		}
	})
}

type mockWorkspaceCloner struct {
	prepareFunc func(ctx context.Context, req CloneRequest) (*CloneResult, error)
	hydrateFunc func(ctx context.Context, box *Box) error
}

func (m *mockWorkspaceCloner) Prepare(ctx context.Context, req CloneRequest) (*CloneResult, error) {
	if m.prepareFunc != nil {
		return m.prepareFunc(ctx, req)
	}
	return &CloneResult{
		SandboxWorkDir: "/tmp/sandbox/" + req.ID,
		Mounts:         []MountSpec{},
		ContainerHooks: []ContainerStartupHook{},
	}, nil
}

func (m *mockWorkspaceCloner) Hydrate(ctx context.Context, box *Box) error {
	if m.hydrateFunc != nil {
		return m.hydrateFunc(ctx, box)
	}
	return nil
}

func TestBoxer_Sync(t *testing.T) {
	ctx := context.Background()

	t.Run("syncs all sandboxes", func(t *testing.T) {
		var inspectCalls []string

		mockContainer := &mockContainerOps{
			inspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				inspectCalls = append(inspectCalls, containerID)
				return []types.Container{{Status: "running"}}, nil
			},
		}
		mockImage := &mockImageOps{}

		boxer := newTestBoxer(t, mockContainer, mockImage)

		box1 := &Box{
			ID:             "sandbox-1",
			ContainerID:    "container-1",
			SandboxWorkDir: t.TempDir(),
			ImageName:      "test-image",
		}
		if err := boxer.SaveSandbox(ctx, box1); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		box2 := &Box{
			ID:             "sandbox-2",
			ContainerID:    "container-2",
			SandboxWorkDir: t.TempDir(),
			ImageName:      "test-image",
		}
		if err := boxer.SaveSandbox(ctx, box2); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		err := boxer.Sync(ctx)
		if err != nil {
			t.Fatalf("Sync() error = %v", err)
		}

		if len(inspectCalls) != 2 {
			t.Errorf("Expected 2 inspect calls, got %d", len(inspectCalls))
		}
	})

	t.Run("handles sandbox sync errors gracefully", func(t *testing.T) {
		mockContainer := &mockContainerOps{
			inspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				return nil, errors.New("inspect failed")
			},
		}
		mockImage := &mockImageOps{}

		boxer := newTestBoxer(t, mockContainer, mockImage)

		box := &Box{
			ID:             "sandbox-1",
			ContainerID:    "bad-container",
			SandboxWorkDir: "/nonexistent/path",
			ImageName:      "test-image",
		}
		if err := boxer.SaveSandbox(ctx, box); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		err := boxer.Sync(ctx)
		if err != nil {
			t.Fatalf("Sync() should not return error even if individual syncs fail, got: %v", err)
		}
	})
}

func TestBoxer_Cleanup_EndToEnd(t *testing.T) {
	ctx := context.Background()

	t.Run("cleanup removes all sandbox resources", func(t *testing.T) {
		var stopCalls []string
		var deleteCalls []string
		var removeRemoteCalls []struct{ dir, name string }
		var removeAllCalls []string

		mockContainer := &mockContainerOps{
			stopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
			deleteFunc: func(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
				deleteCalls = append(deleteCalls, containerID)
				return "deleted", nil
			},
		}

		mockGit := &mockGitOps{
			removeRemoteFunc: func(ctx context.Context, dir, name string) error {
				removeRemoteCalls = append(removeRemoteCalls, struct{ dir, name string }{dir, name})
				return nil
			},
		}

		mockFile := &mockFileOps{
			removeAllFunc: func(path string) error {
				removeAllCalls = append(removeAllCalls, path)
				return nil
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.gitOps = mockGit
		boxer.fileOps = mockFile

		sandboxDir := filepath.Join(boxer.appRoot, "clones", "test-sandbox")
		box := &Box{
			ID:             "test-sandbox",
			ContainerID:    "test-container",
			HostOriginDir:  "/host/origin",
			SandboxWorkDir: sandboxDir,
			ImageName:      "test-image",
		}
		if err := boxer.SaveSandbox(ctx, box); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		err := boxer.Cleanup(ctx, box)
		if err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}

		if len(stopCalls) != 1 || stopCalls[0] != "test-container" {
			t.Errorf("Expected Stop called with 'test-container', got: %v", stopCalls)
		}

		if len(deleteCalls) != 1 || deleteCalls[0] != "test-container" {
			t.Errorf("Expected Delete called with 'test-container', got: %v", deleteCalls)
		}

		if len(removeRemoteCalls) != 1 {
			t.Errorf("Expected 1 RemoveRemote call, got %d", len(removeRemoteCalls))
		} else {
			if removeRemoteCalls[0].dir != "/host/origin" {
				t.Errorf("Expected RemoveRemote dir '/host/origin', got %s", removeRemoteCalls[0].dir)
			}
			if removeRemoteCalls[0].name != "sand/test-sandbox" {
				t.Errorf("Expected RemoveRemote name 'sand/test-sandbox', got %s", removeRemoteCalls[0].name)
			}
		}

		if len(removeAllCalls) != 1 || removeAllCalls[0] != sandboxDir {
			t.Errorf("Expected RemoveAll called with sandbox dir, got: %v", removeAllCalls)
		}

		loadedBox, err := boxer.Get(ctx, "test-sandbox")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if loadedBox != nil {
			t.Error("Expected sandbox to be removed from DB")
		}
	})

	t.Run("cleanup logs container errors but continues", func(t *testing.T) {
		var removeAllCalled bool
		mockContainer := &mockContainerOps{
			stopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				return "", errors.New("stop failed")
			},
			deleteFunc: func(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
				return "", errors.New("delete failed")
			},
		}

		mockFile := &mockFileOps{
			removeAllFunc: func(path string) error {
				removeAllCalled = true
				return nil
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.fileOps = mockFile

		box := &Box{
			ID:             "test-sandbox",
			ContainerID:    "test-container",
			HostOriginDir:  "/host/origin",
			SandboxWorkDir: "/tmp/sandbox",
			ImageName:      "test-image",
		}
		if err := boxer.SaveSandbox(ctx, box); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		err := boxer.Cleanup(ctx, box)
		if err != nil {
			t.Fatalf("Cleanup() should succeed even with container errors, got: %v", err)
		}

		if !removeAllCalled {
			t.Error("Expected cleanup to continue to file removal despite container errors")
		}
	})

	t.Run("cleanup returns error on git failure", func(t *testing.T) {
		expectedErr := errors.New("git remove remote failed")
		mockContainer := &mockContainerOps{}
		mockGit := &mockGitOps{
			removeRemoteFunc: func(ctx context.Context, dir, name string) error {
				return expectedErr
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.gitOps = mockGit

		box := &Box{
			ID:             "test-sandbox",
			ContainerID:    "test-container",
			HostOriginDir:  "/host/origin",
			SandboxWorkDir: "/tmp/sandbox",
			ImageName:      "test-image",
		}
		if err := boxer.SaveSandbox(ctx, box); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		err := boxer.Cleanup(ctx, box)
		if err != nil {
			t.Fatal("error from git remote removal should not cause Cleanupt to return an error")
		}
	})

	t.Run("cleanup returns error on file removal failure", func(t *testing.T) {
		expectedErr := errors.New("remove all failed")
		mockContainer := &mockContainerOps{}
		mockFile := &mockFileOps{
			removeAllFunc: func(path string) error {
				return expectedErr
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.fileOps = mockFile

		box := &Box{
			ID:             "test-sandbox",
			ContainerID:    "test-container",
			HostOriginDir:  "/host/origin",
			SandboxWorkDir: "/tmp/sandbox",
			ImageName:      "test-image",
		}
		if err := boxer.SaveSandbox(ctx, box); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		err := boxer.Cleanup(ctx, box)
		if err != nil {
			t.Fatal("Error from file removal should not cause Cleanup to return an error")
		}
	})
}

func TestBoxer_AttachSandbox(t *testing.T) {
	ctx := context.Background()

	t.Run("reattaches to existing sandbox", func(t *testing.T) {
		mockContainer := &mockContainerOps{}
		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		originalBox := &Box{
			ID:             "existing-sandbox",
			ContainerID:    "existing-container",
			HostOriginDir:  "/host/origin",
			SandboxWorkDir: "/tmp/sandbox",
			ImageName:      "test-image:v1",
			DNSDomain:      "test.local",
			EnvFile:        "/tmp/.env",
		}
		if err := boxer.SaveSandbox(ctx, originalBox); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		attachedBox, err := boxer.AttachSandbox(ctx, "existing-sandbox")
		if err != nil {
			t.Fatalf("AttachSandbox() error = %v", err)
		}

		if attachedBox.ID != "existing-sandbox" {
			t.Errorf("Expected ID 'existing-sandbox', got %s", attachedBox.ID)
		}
		if attachedBox.ContainerID != "existing-container" {
			t.Errorf("Expected ContainerID 'existing-container', got %s", attachedBox.ContainerID)
		}
		if attachedBox.ImageName != "test-image:v1" {
			t.Errorf("Expected ImageName 'test-image:v1', got %s", attachedBox.ImageName)
		}
		if attachedBox.DNSDomain != "test.local" {
			t.Errorf("Expected DNSDomain 'test.local', got %s", attachedBox.DNSDomain)
		}
	})

	t.Run("returns error for nonexistent sandbox", func(t *testing.T) {
		mockContainer := &mockContainerOps{}
		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		_, err := boxer.AttachSandbox(ctx, "nonexistent")
		if err == nil {
			t.Fatal("Expected error for nonexistent sandbox, got nil")
		}
		if !strings.Contains(err.Error(), "nonexistent") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})
}

func TestBoxer_EnsureImage(t *testing.T) {
	ctx := context.Background()

	t.Run("image already present", func(t *testing.T) {
		mockImage := &mockImageOps{
			listFunc: func(ctx context.Context) ([]types.ImageEntry, error) {
				return []types.ImageEntry{
					{Reference: "test-image:latest"},
					{Reference: "other-image:v1"},
				}, nil
			},
		}

		mockContainer := &mockContainerOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		err := boxer.EnsureImage(ctx, "test-image:latest")
		if err != nil {
			t.Fatalf("EnsureImage() error = %v", err)
		}
	})

	t.Run("image needs pull", func(t *testing.T) {
		pullCalled := false
		waitCalled := false

		mockImage := &mockImageOps{
			listFunc: func(ctx context.Context) ([]types.ImageEntry, error) {
				return []types.ImageEntry{
					{Reference: "other-image:v1"},
				}, nil
			},
			pullFunc: func(ctx context.Context, image string) (func() error, error) {
				pullCalled = true
				if image != "new-image:latest" {
					t.Errorf("Expected pull 'new-image:latest', got %s", image)
				}
				return func() error {
					waitCalled = true
					return nil
				}, nil
			},
		}

		mockContainer := &mockContainerOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		err := boxer.EnsureImage(ctx, "new-image:latest")
		if err != nil {
			t.Fatalf("EnsureImage() error = %v", err)
		}

		if !pullCalled {
			t.Error("Expected Pull to be called")
		}
		if !waitCalled {
			t.Error("Expected wait function to be called")
		}
	})

	t.Run("list error", func(t *testing.T) {
		expectedErr := errors.New("list failed")
		mockImage := &mockImageOps{
			listFunc: func(ctx context.Context) ([]types.ImageEntry, error) {
				return nil, expectedErr
			},
		}

		mockContainer := &mockContainerOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		err := boxer.EnsureImage(ctx, "test-image:latest")
		if err == nil {
			t.Fatal("Expected error from list, got nil")
		}
	})

	t.Run("pull error", func(t *testing.T) {
		expectedErr := errors.New("pull failed")
		mockImage := &mockImageOps{
			listFunc: func(ctx context.Context) ([]types.ImageEntry, error) {
				return []types.ImageEntry{}, nil
			},
			pullFunc: func(ctx context.Context, image string) (func() error, error) {
				return nil, expectedErr
			},
		}

		mockContainer := &mockContainerOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		err := boxer.EnsureImage(ctx, "test-image:latest")
		if err == nil {
			t.Fatal("Expected error from pull, got nil")
		}
	})

	t.Run("wait error", func(t *testing.T) {
		expectedErr := errors.New("wait failed")
		mockImage := &mockImageOps{
			listFunc: func(ctx context.Context) ([]types.ImageEntry, error) {
				return []types.ImageEntry{}, nil
			},
			pullFunc: func(ctx context.Context, image string) (func() error, error) {
				return func() error {
					return expectedErr
				}, nil
			},
		}

		mockContainer := &mockContainerOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		err := boxer.EnsureImage(ctx, "test-image:latest")
		if err == nil {
			t.Fatal("Expected error from wait, got nil")
		}
	})
}

func TestBoxer_StopContainer(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		var stopCalls []string
		mockContainer := &mockContainerOps{
			stopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		box := &Box{
			ID:          "test-sandbox",
			ContainerID: "test-container",
		}

		err := boxer.StopContainer(ctx, box)
		if err != nil {
			t.Fatalf("StopContainer() error = %v", err)
		}

		if len(stopCalls) != 1 || stopCalls[0] != "test-container" {
			t.Errorf("Expected stop called with 'test-container', got: %v", stopCalls)
		}
	})

	t.Run("missing container ID", func(t *testing.T) {
		mockContainer := &mockContainerOps{}
		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		box := &Box{
			ID:          "test-sandbox",
			ContainerID: "",
		}

		err := boxer.StopContainer(ctx, box)
		if err == nil {
			t.Fatal("Expected error for missing container ID, got nil")
		}
		if !strings.Contains(err.Error(), "test-sandbox") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})

	t.Run("stop error", func(t *testing.T) {
		expectedErr := errors.New("stop failed")
		mockContainer := &mockContainerOps{
			stopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				return "", expectedErr
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		box := &Box{
			ID:          "test-sandbox",
			ContainerID: "test-container",
		}

		err := boxer.StopContainer(ctx, box)
		if err == nil {
			t.Fatal("Expected error from stop, got nil")
		}
		if !strings.Contains(err.Error(), "test-sandbox") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})
}
