package sand

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/banksean/sand/applecontainer/options"
)

type mockGitOps struct {
	addRemoteFunc    func(ctx context.Context, dir, name, url string) error
	removeRemoteFunc func(ctx context.Context, dir, name string) error
	fetchFunc        func(ctx context.Context, dir, remote string) error
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

type mockFileOps struct {
	mkdirAllFunc  func(path string, perm os.FileMode) error
	copyFunc      func(ctx context.Context, src, dst string) error
	statFunc      func(path string) (os.FileInfo, error)
	lstatFunc     func(path string) (os.FileInfo, error)
	readlinkFunc  func(path string) (string, error)
	createFunc    func(path string) (*os.File, error)
	removeAllFunc func(path string) error
	writeFileFunc func(path string, data []byte, perm os.FileMode) error
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

type mockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime int64
	isDir   bool
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() interface{}   { return nil }

func TestMountSpec_String(t *testing.T) {
	tests := []struct {
		name string
		spec MountSpec
		want string
	}{
		{
			name: "readonly mount",
			spec: MountSpec{Source: "/host/path", Target: "/container/path", ReadOnly: true},
			want: "type=bind,source=/host/path,target=/container/path,readonly",
		},
		{
			name: "readwrite mount",
			spec: MountSpec{Source: "/host/rw", Target: "/container/rw", ReadOnly: false},
			want: "type=bind,source=/host/rw,target=/container/rw",
		},
		{
			name: "mount with spaces",
			spec: MountSpec{Source: "/host/path with spaces", Target: "/container/target", ReadOnly: false},
			want: "type=bind,source=/host/path with spaces,target=/container/target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.String(); got != tt.want {
				t.Errorf("MountSpec.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainerHook(t *testing.T) {
	t.Run("hook name and execution", func(t *testing.T) {
		called := false
		var capturedBox *Box

		hook := NewContainerStartupHook("test-hook", func(ctx context.Context, b *Box) error {
			called = true
			capturedBox = b
			return nil
		})

		if hook.Name() != "test-hook" {
			t.Errorf("Expected name 'test-hook', got %s", hook.Name())
		}

		ctx := context.Background()
		box := &Box{ID: "test"}
		if err := hook.OnStart(ctx, box); err != nil {
			t.Errorf("OnStart() error = %v", err)
		}

		if !called {
			t.Error("Hook function was not called")
		}
		if capturedBox != box {
			t.Error("Hook received wrong box")
		}
	})

	t.Run("hook returns error", func(t *testing.T) {
		expectedErr := errors.New("hook error")
		hook := NewContainerStartupHook("failing-hook", func(ctx context.Context, b *Box) error {
			return expectedErr
		})

		ctx := context.Background()
		box := &Box{ID: "test"}
		err := hook.OnStart(ctx, box)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("Expected error %v, got %v", expectedErr, err)
		}
	})
}

func TestDefaultWorkspaceCloner_MountPlanFor(t *testing.T) {
	cloner := &DefaultWorkspaceCloner{}
	mounts := cloner.mountPlanFor("/tmp/sandbox")

	if len(mounts) != 3 {
		t.Fatalf("Expected 3 mounts, got %d", len(mounts))
	}

	if mounts[0].Source != "/tmp/sandbox/hostkeys" || mounts[0].Target != "/hostkeys" || !mounts[0].ReadOnly {
		t.Errorf("Invalid hostkeys mount: %+v", mounts[0])
	}

	if mounts[1].Source != "/tmp/sandbox/dotfiles" || mounts[1].Target != "/dotfiles" || !mounts[1].ReadOnly {
		t.Errorf("Invalid dotfiles mount: %+v", mounts[1])
	}

	if mounts[2].Source != "/tmp/sandbox/app" || mounts[2].Target != "/app" || mounts[2].ReadOnly {
		t.Errorf("Invalid app mount: %+v", mounts[2])
	}
}

func TestDefaultWorkspaceCloner_Hydrate(t *testing.T) {
	ctx := context.Background()

	t.Run("nil box", func(t *testing.T) {
		cloner := &DefaultWorkspaceCloner{}
		err := cloner.Hydrate(ctx, nil)
		if err == nil {
			t.Fatal("Expected error for nil box, got nil")
		}
		if !strings.Contains(err.Error(), "nil box") {
			t.Errorf("Error should mention 'nil box', got: %v", err)
		}
	})

	t.Run("missing workdir", func(t *testing.T) {
		cloner := &DefaultWorkspaceCloner{}
		box := &Box{ID: "test-box", SandboxWorkDir: ""}
		err := cloner.Hydrate(ctx, box)
		if err == nil {
			t.Fatal("Expected error for missing workdir, got nil")
		}
		if !strings.Contains(err.Error(), "missing workdir") {
			t.Errorf("Error should mention 'missing workdir', got: %v", err)
		}
		if !strings.Contains(err.Error(), "test-box") {
			t.Errorf("Error should contain sandbox ID 'test-box', got: %v", err)
		}
	})

	t.Run("valid box", func(t *testing.T) {
		cloner := &DefaultWorkspaceCloner{}
		box := &Box{
			ID:             "test-box",
			SandboxWorkDir: "/tmp/sandbox",
		}

		err := cloner.Hydrate(ctx, box)
		if err != nil {
			t.Fatalf("Hydrate() error = %v", err)
		}

		if len(box.Mounts) != 3 {
			t.Errorf("Expected 3 mounts, got %d", len(box.Mounts))
		}

		if len(box.ContainerHooks) != 1 {
			t.Errorf("Expected 1 hook, got %d", len(box.ContainerHooks))
		}

		if box.ContainerHooks[0].Name() != "default container bootstrap" {
			t.Errorf("Expected default container bootstrap hook, got: %s", box.ContainerHooks[0].Name())
		}
	})

	t.Run("appends to existing hooks", func(t *testing.T) {
		cloner := &DefaultWorkspaceCloner{}
		existingHook := NewContainerStartupHook("existing", func(ctx context.Context, b *Box) error {
			return nil
		})
		box := &Box{
			ID:             "test-box",
			SandboxWorkDir: "/tmp/sandbox",
			ContainerHooks: []ContainerStartupHook{existingHook},
		}

		err := cloner.Hydrate(ctx, box)
		if err != nil {
			t.Fatalf("Hydrate() error = %v", err)
		}

		if len(box.ContainerHooks) != 2 {
			t.Errorf("Expected 2 hooks, got %d", len(box.ContainerHooks))
		}
	})
}

func TestDefaultWorkspaceCloner_CloneWorkDir(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		var mkdirCalls []string
		var copyCalls []struct{ src, dst string }
		var addRemoteCalls []struct{ dir, name, url string }
		var fetchCalls []struct{ dir, remote string }

		mockFile := &mockFileOps{
			mkdirAllFunc: func(path string, perm os.FileMode) error {
				mkdirCalls = append(mkdirCalls, path)
				return nil
			},
			copyFunc: func(ctx context.Context, src, dst string) error {
				copyCalls = append(copyCalls, struct{ src, dst string }{src, dst})
				return nil
			},
		}

		mockGit := &mockGitOps{
			addRemoteFunc: func(ctx context.Context, dir, name, url string) error {
				addRemoteCalls = append(addRemoteCalls, struct{ dir, name, url string }{dir, name, url})
				return nil
			},
			fetchFunc: func(ctx context.Context, dir, remote string) error {
				fetchCalls = append(fetchCalls, struct{ dir, remote string }{dir, remote})
				return nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			appRoot:   "/app",
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			gitOps:    mockGit,
			fileOps:   mockFile,
		}

		err := cloner.cloneWorkDir(ctx, "test-id", "/host/workdir")
		if err != nil {
			t.Fatalf("cloneWorkDir() error = %v", err)
		}

		if len(mkdirCalls) != 1 || mkdirCalls[0] != "/app/clones/test-id" {
			t.Errorf("Expected MkdirAll('/app/clones/test-id'), got: %v", mkdirCalls)
		}

		if len(copyCalls) != 1 {
			t.Fatalf("Expected 1 copy call, got %d", len(copyCalls))
		}
		if copyCalls[0].src != "/host/workdir" || copyCalls[0].dst != "/app/clones/test-id/app" {
			t.Errorf("Copy args: src=%s, dst=%s", copyCalls[0].src, copyCalls[0].dst)
		}

		if len(addRemoteCalls) != 2 {
			t.Fatalf("Expected 2 AddRemote calls, got %d", len(addRemoteCalls))
		}

		if addRemoteCalls[0].dir != "/app/clones/test-id/app" ||
			addRemoteCalls[0].name != "origin-host-workdir" ||
			addRemoteCalls[0].url != "/host/workdir" {
			t.Errorf("First AddRemote: %+v", addRemoteCalls[0])
		}

		if addRemoteCalls[1].dir != "/host/workdir" ||
			addRemoteCalls[1].name != "sand/test-id" ||
			addRemoteCalls[1].url != "/app/clones/test-id/app" {
			t.Errorf("Second AddRemote: %+v", addRemoteCalls[1])
		}

		if len(fetchCalls) != 2 {
			t.Fatalf("Expected 2 Fetch calls, got %d", len(fetchCalls))
		}
	})

	t.Run("mkdir error", func(t *testing.T) {
		expectedErr := errors.New("mkdir failed")
		mockFile := &mockFileOps{
			mkdirAllFunc: func(path string, perm os.FileMode) error {
				return expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			gitOps:    &mockGitOps{},
			fileOps:   mockFile,
		}

		err := cloner.cloneWorkDir(ctx, "test-id", "/host/workdir")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})

	t.Run("copy error", func(t *testing.T) {
		expectedErr := errors.New("copy failed")
		mockFile := &mockFileOps{
			copyFunc: func(ctx context.Context, src, dst string) error {
				return expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			gitOps:    &mockGitOps{},
			fileOps:   mockFile,
		}

		err := cloner.cloneWorkDir(ctx, "test-id", "/host/workdir")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})

	t.Run("git add remote error", func(t *testing.T) {
		expectedErr := errors.New("git add remote failed")
		mockGit := &mockGitOps{
			addRemoteFunc: func(ctx context.Context, dir, name, url string) error {
				return expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			gitOps:    mockGit,
			fileOps:   &mockFileOps{},
		}

		err := cloner.cloneWorkDir(ctx, "test-id", "/host/workdir")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})

	t.Run("git fetch error", func(t *testing.T) {
		expectedErr := errors.New("git fetch failed")
		mockGit := &mockGitOps{
			fetchFunc: func(ctx context.Context, dir, remote string) error {
				return expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			gitOps:    mockGit,
			fileOps:   &mockFileOps{},
		}

		err := cloner.cloneWorkDir(ctx, "test-id", "/host/workdir")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})
}

func TestDefaultWorkspaceCloner_CloneHostKeyPair(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		var mkdirCalls []string
		var copyCalls []struct{ src, dst string }

		mockFile := &mockFileOps{
			mkdirAllFunc: func(path string, perm os.FileMode) error {
				mkdirCalls = append(mkdirCalls, path)
				return nil
			},
			copyFunc: func(ctx context.Context, src, dst string) error {
				copyCalls = append(copyCalls, struct{ src, dst string }{src, dst})
				return nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			appRoot:   "/app",
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneHostKeyPair(ctx, "test-id")
		if err != nil {
			t.Fatalf("cloneHostKeyPair() error = %v", err)
		}

		if len(mkdirCalls) != 1 || mkdirCalls[0] != "/app/clones/test-id/hostkeys" {
			t.Errorf("Expected MkdirAll('/app/clones/test-id/hostkeys'), got: %v", mkdirCalls)
		}

		if len(copyCalls) != 2 {
			t.Fatalf("Expected 2 copy calls, got %d", len(copyCalls))
		}

		if !strings.HasSuffix(copyCalls[0].src, ".config/sand/container_server_identity") ||
			copyCalls[0].dst != "/app/clones/test-id/hostkeys/ssh_host_ed25519_key" {
			t.Errorf("First copy: src=%s, dst=%s", copyCalls[0].src, copyCalls[0].dst)
		}

		if !strings.HasSuffix(copyCalls[1].src, ".config/sand/container_server_identity.pub") ||
			copyCalls[1].dst != "/app/clones/test-id/hostkeys/ssh_host_ed25519_key.pub" {
			t.Errorf("Second copy: src=%s, dst=%s", copyCalls[1].src, copyCalls[1].dst)
		}
	})

	t.Run("mkdir error", func(t *testing.T) {
		expectedErr := errors.New("mkdir failed")
		mockFile := &mockFileOps{
			mkdirAllFunc: func(path string, perm os.FileMode) error {
				return expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			appRoot:   "/app",
			cloneRoot: "/app/clones",
			fileOps:   mockFile,
		}

		err := cloner.cloneHostKeyPair(ctx, "test-id")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})

	t.Run("copy private key error", func(t *testing.T) {
		expectedErr := errors.New("copy failed")
		callCount := 0
		mockFile := &mockFileOps{
			copyFunc: func(ctx context.Context, src, dst string) error {
				callCount++
				if callCount == 1 {
					return expectedErr
				}
				return nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			appRoot:   "/app",
			cloneRoot: "/app/clones",
			fileOps:   mockFile,
		}

		err := cloner.cloneHostKeyPair(ctx, "test-id")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})

	t.Run("copy public key error", func(t *testing.T) {
		expectedErr := errors.New("copy pub failed")
		callCount := 0
		mockFile := &mockFileOps{
			copyFunc: func(ctx context.Context, src, dst string) error {
				callCount++
				if callCount == 2 {
					return expectedErr
				}
				return nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			appRoot:   "/app",
			cloneRoot: "/app/clones",
			fileOps:   mockFile,
		}

		err := cloner.cloneHostKeyPair(ctx, "test-id")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})
}

func TestDefaultWorkspaceCloner_CloneDotfiles(t *testing.T) {
	ctx := context.Background()

	t.Run("regular file", func(t *testing.T) {
		var copyCalls []struct{ src, dst string }

		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				return mockFileInfo{name: ".gitconfig", mode: 0o644}, nil
			},
			copyFunc: func(ctx context.Context, src, dst string) error {
				copyCalls = append(copyCalls, struct{ src, dst string }{src, dst})
				return nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneDotfiles(ctx, "test-id")
		if err != nil {
			t.Fatalf("cloneDotfiles() error = %v", err)
		}

		if len(copyCalls) != 5 {
			t.Errorf("Expected 5 copy calls (one per dotfile), got %d", len(copyCalls))
		}
	})

	t.Run("missing file creates empty", func(t *testing.T) {
		var createCalls []string

		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			createFunc: func(path string) (*os.File, error) {
				createCalls = append(createCalls, path)
				return nil, nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneDotfiles(ctx, "test-id")
		if err != nil {
			t.Fatalf("cloneDotfiles() error = %v", err)
		}

		if len(createCalls) != 5 {
			t.Errorf("Expected 5 create calls, got %d", len(createCalls))
		}
	})

	t.Run("symlink to absolute path", func(t *testing.T) {
		var copyCalls []struct{ src, dst string }

		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				if strings.Contains(path, ".zshrc") {
					return mockFileInfo{name: ".zshrc", mode: os.ModeSymlink}, nil
				}
				return mockFileInfo{name: "target", mode: 0o644}, nil
			},
			readlinkFunc: func(path string) (string, error) {
				return "/absolute/target/.zshrc", nil
			},
			copyFunc: func(ctx context.Context, src, dst string) error {
				copyCalls = append(copyCalls, struct{ src, dst string }{src, dst})
				return nil
			},
			createFunc: func(path string) (*os.File, error) {
				return nil, nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneDotfiles(ctx, "test-id")
		if err != nil {
			t.Fatalf("cloneDotfiles() error = %v", err)
		}

		found := false
		for _, call := range copyCalls {
			if call.src == "/absolute/target/.zshrc" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected copy call with resolved absolute symlink path")
		}
	})

	t.Run("symlink to relative path", func(t *testing.T) {
		var copyCalls []struct{ src, dst string }
		homeDir := os.Getenv("HOME")

		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				if strings.Contains(path, ".zshrc") {
					return mockFileInfo{name: ".zshrc", mode: os.ModeSymlink}, nil
				}
				return mockFileInfo{name: "target", mode: 0o644}, nil
			},
			readlinkFunc: func(path string) (string, error) {
				return "relative/target/.zshrc", nil
			},
			copyFunc: func(ctx context.Context, src, dst string) error {
				copyCalls = append(copyCalls, struct{ src, dst string }{src, dst})
				return nil
			},
			createFunc: func(path string) (*os.File, error) {
				return nil, nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneDotfiles(ctx, "test-id")
		if err != nil {
			t.Fatalf("cloneDotfiles() error = %v", err)
		}

		expectedPath := filepath.Join(homeDir, "relative/target/.zshrc")
		found := false
		for _, call := range copyCalls {
			if call.src == expectedPath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected copy call with resolved relative symlink path: %s", expectedPath)
		}
	})

	t.Run("broken symlink creates empty", func(t *testing.T) {
		var createCalls []string

		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				if strings.Contains(path, ".zshrc") && !strings.Contains(path, "target") {
					return mockFileInfo{name: ".zshrc", mode: os.ModeSymlink}, nil
				}
				return nil, os.ErrNotExist
			},
			readlinkFunc: func(path string) (string, error) {
				return "/nonexistent/target", nil
			},
			createFunc: func(path string) (*os.File, error) {
				createCalls = append(createCalls, path)
				return nil, nil
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneDotfiles(ctx, "test-id")
		if err != nil {
			t.Fatalf("cloneDotfiles() error = %v", err)
		}

		if len(createCalls) == 0 {
			t.Error("Expected at least one create call for broken symlink")
		}
	})

	t.Run("create error", func(t *testing.T) {
		expectedErr := errors.New("create failed")
		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			createFunc: func(path string) (*os.File, error) {
				return nil, expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneDotfiles(ctx, "test-id")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})

	t.Run("mkdir error", func(t *testing.T) {
		expectedErr := errors.New("mkdir failed")
		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				return mockFileInfo{name: "file", mode: 0o644}, nil
			},
			mkdirAllFunc: func(path string, perm os.FileMode) error {
				return expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneDotfiles(ctx, "test-id")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})

	t.Run("copy error", func(t *testing.T) {
		expectedErr := errors.New("copy failed")
		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				return mockFileInfo{name: "file", mode: 0o644}, nil
			},
			copyFunc: func(ctx context.Context, src, dst string) error {
				return expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		err := cloner.cloneDotfiles(ctx, "test-id")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})
}

func TestDefaultWorkspaceCloner_DefaultContainerHook(t *testing.T) {
	ctx := context.Background()

	t.Run("hook returns correct name", func(t *testing.T) {
		cloner := &DefaultWorkspaceCloner{}
		hook := cloner.defaultContainerHook()

		if hook.Name() != "default container bootstrap" {
			t.Errorf("Expected name 'default container bootstrap', got: %s", hook.Name())
		}
	})

	t.Run("hook executes commands successfully", func(t *testing.T) {
		cloner := &DefaultWorkspaceCloner{}
		hook := cloner.defaultContainerHook()

		var execCalls []struct {
			cmd  string
			args []string
		}
		mockOps := &mockContainerOps{
			execFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
				execCalls = append(execCalls, struct {
					cmd  string
					args []string
				}{cmd, args})
				return "success", nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		err := hook.OnStart(ctx, box)
		if err != nil {
			t.Fatalf("Hook execution error = %v", err)
		}

		if len(execCalls) != 4 {
			t.Fatalf("Expected 4 exec calls, got %d", len(execCalls))
		}

		if execCalls[0].cmd != "cp" {
			t.Errorf("First command should be 'cp', got: %s", execCalls[0].cmd)
		}
		if execCalls[3].cmd != "/usr/sbin/sshd" {
			t.Errorf("Last command should be '/usr/sbin/sshd', got: %s", execCalls[3].cmd)
		}
	})

	t.Run("hook aggregates multiple errors", func(t *testing.T) {
		cloner := &DefaultWorkspaceCloner{}
		hook := cloner.defaultContainerHook()

		callCount := 0
		mockOps := &mockContainerOps{
			execFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
				callCount++
				if callCount == 1 || callCount == 3 {
					return "", errors.New("exec failed")
				}
				return "success", nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		err := hook.OnStart(ctx, box)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "copy dotfiles") {
			t.Error("Error should contain 'copy dotfiles'")
		}
		if !strings.Contains(errStr, "copy host keys") {
			t.Error("Error should contain 'copy host keys'")
		}
	})
}

func TestDefaultWorkspaceCloner_Prepare(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mockFile := &mockFileOps{
			lstatFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			createFunc: func(path string) (*os.File, error) {
				return nil, nil
			},
		}

		mockGit := &mockGitOps{}

		cloner := &DefaultWorkspaceCloner{
			appRoot:   "/app",
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			gitOps:    mockGit,
			fileOps:   mockFile,
		}

		req := CloneRequest{
			ID:          "test-id",
			HostWorkDir: "/host/work",
			EnvFile:     "/host/.env",
		}

		result, err := cloner.Prepare(ctx, req)
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}

		if result.SandboxWorkDir != "/app/clones/test-id" {
			t.Errorf("Expected SandboxWorkDir '/app/clones/test-id', got: %s", result.SandboxWorkDir)
		}

		if len(result.Mounts) != 3 {
			t.Errorf("Expected 3 mounts, got %d", len(result.Mounts))
		}

		if len(result.ContainerHooks) != 2 {
			t.Errorf("Expected 2 hooks, got %d", len(result.ContainerHooks))
		}
	})

	t.Run("create directory error", func(t *testing.T) {
		expectedErr := errors.New("mkdir failed")
		mockFile := &mockFileOps{
			mkdirAllFunc: func(path string, perm os.FileMode) error {
				return expectedErr
			},
		}

		cloner := &DefaultWorkspaceCloner{
			appRoot:   "/app",
			cloneRoot: "/app/clones",
			messenger: NewNullMessenger(),
			fileOps:   mockFile,
		}

		req := CloneRequest{ID: "test-id", HostWorkDir: "/host/work"}

		_, err := cloner.Prepare(ctx, req)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-id") {
			t.Errorf("Error should contain sandbox ID, got: %v", err)
		}
	})
}
