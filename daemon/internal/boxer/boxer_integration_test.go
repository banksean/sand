package boxer

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/cloning"
	"github.com/banksean/sand/hostops"
	"github.com/banksean/sand/sandtypes"
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

type mockGitOps struct {
	addRemoteFunc    func(ctx context.Context, dir, name, url string) error
	removeRemoteFunc func(ctx context.Context, dir, name string) error
	fetchFunc        func(ctx context.Context, dir, remote string) error
	topLevelFunc     func(ctx context.Context, dir string) string
	remoteURLFunc    func(ctx context.Context, dir, name string) string
	branchFunc       func(ctx context.Context, dir string) string
	commitFunc       func(ctx context.Context, dir string) string
	isDirtyFunc      func(ctx context.Context, dir string) bool
}

func (m *mockGitOps) AddRemote(ctx context.Context, dir, name, url string) error {
	if m.addRemoteFunc != nil {
		return m.addRemoteFunc(ctx, dir, name, url)
	}
	return nil
}

func (m *mockGitOps) RemoveRemote(ctx context.Context, dir, name string) error {
	if m.removeRemoteFunc != nil {
		return m.removeRemoteFunc(ctx, dir, name)
	}
	return nil
}

func (m *mockGitOps) Fetch(ctx context.Context, dir, remote string) error {
	if m.fetchFunc != nil {
		return m.fetchFunc(ctx, dir, remote)
	}
	return nil
}

func (m *mockGitOps) TopLevel(ctx context.Context, dir string) string {
	if m.topLevelFunc != nil {
		return m.topLevelFunc(ctx, dir)
	}
	return ""
}

func (m *mockGitOps) RemoteURL(ctx context.Context, dir, name string) string {
	if m.remoteURLFunc != nil {
		return m.remoteURLFunc(ctx, dir, name)
	}
	return ""
}

func (m *mockGitOps) Branch(ctx context.Context, dir string) string {
	if m.branchFunc != nil {
		return m.branchFunc(ctx, dir)
	}
	return ""
}

func (m *mockGitOps) Commit(ctx context.Context, dir string) string {
	if m.commitFunc != nil {
		return m.commitFunc(ctx, dir)
	}
	return ""
}

func (m *mockGitOps) IsDirty(ctx context.Context, dir string) bool {
	if m.isDirtyFunc != nil {
		return m.isDirtyFunc(ctx, dir)
	}
	return false
}

type mockFileOps struct {
	mkdirAllFunc  func(path string, perm os.FileMode) error
	copyFunc      func(ctx context.Context, src, dst string) error
	statFunc      func(path string) (os.FileInfo, error)
	lstatFunc     func(path string) (os.FileInfo, error)
	readlinkFunc  func(path string) (string, error)
	createFunc    func(path string) (*os.File, error)
	removeAllFunc func(path string) error
	writeFileFunc func(path string, data []byte, perm os.FileMode) error
	volumeFunc    func(path string) (*hostops.VolumeInfo, error)
}

func (m *mockFileOps) MkdirAll(path string, perm os.FileMode) error {
	if m.mkdirAllFunc != nil {
		return m.mkdirAllFunc(path, perm)
	}
	return nil
}

func (m *mockFileOps) Copy(ctx context.Context, src, dst string) error {
	if m.copyFunc != nil {
		return m.copyFunc(ctx, src, dst)
	}
	return nil
}

func (m *mockFileOps) Stat(path string) (os.FileInfo, error) {
	if m.statFunc != nil {
		return m.statFunc(path)
	}
	return nil, nil
}

func (m *mockFileOps) Lstat(path string) (os.FileInfo, error) {
	if m.lstatFunc != nil {
		return m.lstatFunc(path)
	}
	return nil, nil
}

func (m *mockFileOps) Readlink(path string) (string, error) {
	if m.readlinkFunc != nil {
		return m.readlinkFunc(path)
	}
	return "", nil
}

func (m *mockFileOps) Create(path string) (*os.File, error) {
	if m.createFunc != nil {
		return m.createFunc(path)
	}
	return nil, nil
}

func (m *mockFileOps) RemoveAll(path string) error {
	if m.removeAllFunc != nil {
		return m.removeAllFunc(path)
	}
	return nil
}

func (m *mockFileOps) WriteFile(path string, data []byte, perm os.FileMode) error {
	if m.writeFileFunc != nil {
		return m.writeFileFunc(path, data, perm)
	}
	return nil
}

func (m *mockFileOps) Volume(path string) (*hostops.VolumeInfo, error) {
	if m.volumeFunc != nil {
		return m.volumeFunc(path)
	}
	return nil, nil
}

func newTestBoxer(t *testing.T, containerOps hostops.ContainerOps, imageOps hostops.ImageOps) *Boxer {
	t.Helper()
	tmpDir := path.Join(t.TempDir(), "Application Support", "Sand")
	boxer, err := NewBoxer(tmpDir, "test", nil)
	if err != nil {
		t.Fatalf("Failed to create test Boxer: %v", err)
	}
	t.Cleanup(func() { boxer.Close() })

	boxer.ContainerService = containerOps
	boxer.imageService = imageOps
	boxer.gitOps = &mockGitOps{}
	boxer.fileOps = &mockFileOps{
		lstatFunc: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		createFunc: func(path string) (*os.File, error) {
			return nil, nil
		},
		mkdirAllFunc: func(path string, perm os.FileMode) error {
			return nil
		},
	}

	return boxer
}

func TestBoxer_NewSandbox_EndToEnd(t *testing.T) {
	ctx := context.Background()

	t.Run("success creates sandbox with all components", func(t *testing.T) {
		var createdContainerID string
		mockContainer := &hostops.MockContainerOps{
			CreateFunc: func(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
				createdContainerID = "test-container-123"
				return createdContainerID, nil
			},
		}

		mockImage := &mockImageOps{}

		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.fileOps = hostops.NewDefaultFileOps()

		// Register a test agent in the registry
		testPrep := &mockWorkspacePreparation{
			prepareFunc: func(ctx context.Context, req cloning.CloneRequest) (*cloning.CloneArtifacts, error) {
				sandboxRoot := filepath.Join(boxer.appRoot, "clones", req.ID)
				return &cloning.CloneArtifacts{
					SandboxWorkDir: sandboxRoot,
					PathRegistry:   cloning.NewStandardPathRegistry(sandboxRoot),
				}, nil
			},
		}
		testConfig := &mockContainerConfiguration{}
		boxer.agentRegistry.Register(&cloning.AgentConfig{
			Name:          "test-agent",
			Preparation:   testPrep,
			Configuration: testConfig,
		})

		hostWorkDir := t.TempDir()
		result, err := boxer.NewSandbox(ctx, "test-agent", "test-sandbox", hostWorkDir, "test-image:latest", "", nil, nil)
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

	t.Run("preparation error propagates", func(t *testing.T) {
		mockContainer := &hostops.MockContainerOps{}
		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		expectedErr := errors.New("preparation failed")
		testPrep := &mockWorkspacePreparation{
			prepareFunc: func(ctx context.Context, req cloning.CloneRequest) (*cloning.CloneArtifacts, error) {
				return nil, expectedErr
			},
		}
		testConfig := &mockContainerConfiguration{}
		boxer.agentRegistry.Register(&cloning.AgentConfig{
			Name:          "test-error-agent",
			Preparation:   testPrep,
			Configuration: testConfig,
		})

		_, err := boxer.NewSandbox(ctx, "test-error-agent", "test-sandbox", "/host/work", "test-image", "", nil, nil)
		if err == nil {
			t.Fatal("Expected error from preparation, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("Expected error to wrap preparation error, got: %v", err)
		}

		loadedBox, err := boxer.Get(ctx, "test-sandbox")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if loadedBox != nil {
			t.Error("Expected sandbox not to be saved after preparation error")
		}
	})
}

type mockWorkspacePreparation struct {
	prepareFunc func(ctx context.Context, req cloning.CloneRequest) (*cloning.CloneArtifacts, error)
}

func (m *mockWorkspacePreparation) Prepare(ctx context.Context, req cloning.CloneRequest) (*cloning.CloneArtifacts, error) {
	if m.prepareFunc != nil {
		return m.prepareFunc(ctx, req)
	}
	sandboxRoot := "/tmp/sandbox/" + req.ID
	return &cloning.CloneArtifacts{
		SandboxWorkDir: sandboxRoot,
		PathRegistry:   cloning.NewStandardPathRegistry(sandboxRoot),
	}, nil
}

type mockContainerConfiguration struct {
	getMountsFunc       func(artifacts cloning.CloneArtifacts) []sandtypes.MountSpec
	getStartupHooksFunc func(artifacts cloning.CloneArtifacts) []sandtypes.ContainerStartupHook
}

func (m *mockContainerConfiguration) GetMounts(artifacts cloning.CloneArtifacts) []sandtypes.MountSpec {
	if m.getMountsFunc != nil {
		return m.getMountsFunc(artifacts)
	}
	return []sandtypes.MountSpec{}
}

func (m *mockContainerConfiguration) GetStartupHooks(artifacts cloning.CloneArtifacts) []sandtypes.ContainerStartupHook {
	if m.getStartupHooksFunc != nil {
		return m.getStartupHooksFunc(artifacts)
	}
	return []sandtypes.ContainerStartupHook{}
}

func TestBoxer_Sync(t *testing.T) {
	ctx := context.Background()

	t.Run("syncs all sandboxes", func(t *testing.T) {
		var inspectCalls []string

		mockContainer := &hostops.MockContainerOps{
			InspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				inspectCalls = append(inspectCalls, containerID)
				return []types.Container{{Status: "running"}}, nil
			},
		}
		mockImage := &mockImageOps{}

		boxer := newTestBoxer(t, mockContainer, mockImage)

		box1 := &sandtypes.Box{
			ID:             "sandbox-1",
			ContainerID:    "container-1",
			SandboxWorkDir: t.TempDir(),
			ImageName:      "test-image",
		}
		if err := boxer.SaveSandbox(ctx, box1); err != nil {
			t.Fatalf("SaveSandbox() error = %v", err)
		}

		box2 := &sandtypes.Box{
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
		mockContainer := &hostops.MockContainerOps{
			InspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				return nil, errors.New("inspect failed")
			},
		}
		mockImage := &mockImageOps{}

		boxer := newTestBoxer(t, mockContainer, mockImage)

		box := &sandtypes.Box{
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

		mockContainer := &hostops.MockContainerOps{
			StopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
			DeleteFunc: func(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
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
		box := &sandtypes.Box{
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
		mockContainer := &hostops.MockContainerOps{
			StopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				return "", errors.New("stop failed")
			},
			DeleteFunc: func(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
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

		box := &sandtypes.Box{
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
		mockContainer := &hostops.MockContainerOps{}
		mockGit := &mockGitOps{
			removeRemoteFunc: func(ctx context.Context, dir, name string) error {
				return expectedErr
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.gitOps = mockGit

		box := &sandtypes.Box{
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
		mockContainer := &hostops.MockContainerOps{}
		mockFile := &mockFileOps{
			removeAllFunc: func(path string) error {
				return expectedErr
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.fileOps = mockFile

		box := &sandtypes.Box{
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
		mockContainer := &hostops.MockContainerOps{}
		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		originalBox := &sandtypes.Box{
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
		mockContainer := &hostops.MockContainerOps{}
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

		mockContainer := &hostops.MockContainerOps{}
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

		mockContainer := &hostops.MockContainerOps{}
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

		mockContainer := &hostops.MockContainerOps{}
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

		mockContainer := &hostops.MockContainerOps{}
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

		mockContainer := &hostops.MockContainerOps{}
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
		mockContainer := &hostops.MockContainerOps{
			StopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		box := &sandtypes.Box{
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
		mockContainer := &hostops.MockContainerOps{}
		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		box := &sandtypes.Box{
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
		mockContainer := &hostops.MockContainerOps{
			StopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				return "", expectedErr
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		box := &sandtypes.Box{
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
