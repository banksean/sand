package boxer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimepaths"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/sshimmer"
)

type mockImageOps struct {
	listFunc    func(ctx context.Context) ([]types.ImageEntry, error)
	pullFunc    func(ctx context.Context, image string, w io.Writer) (func() error, error)
	inspectFunc func(ctx context.Context, name string) ([]*types.ImageManifest, error)
}

// Inspect implements [hostops.ImageOps].
func (m *mockImageOps) Inspect(ctx context.Context, name string) ([]*types.ImageManifest, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, name)
	}
	return nil, nil
}

func (m *mockImageOps) List(ctx context.Context) ([]types.ImageEntry, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []types.ImageEntry{}, nil
}

func (m *mockImageOps) Pull(ctx context.Context, image string, w io.Writer) (func() error, error) {
	if m.pullFunc != nil {
		return m.pullFunc(ctx, image, w)
	}
	return func() error { return nil }, nil
}

type mockSSHimmer struct {
	newKeysFunc func(ctx context.Context, domain, username string) (*sshimmer.Keys, error)
}

func (m *mockSSHimmer) NewKeys(ctx context.Context, domain, username string) (*sshimmer.Keys, error) {
	if m.newKeysFunc != nil {
		return m.newKeysFunc(ctx, domain, username)
	}
	return &sshimmer.Keys{
		HostKey:     []byte("fake-host-key"),
		HostKeyPub:  []byte("fake-host-key-pub"),
		HostKeyCert: []byte("fake-host-key-cert"),
		UserCAPub:   []byte("fake-user-ca-pub"),
	}, nil
}

type recordingHookStreamer struct {
	cmd    string
	args   []string
	output string
	err    error
}

func (r *recordingHookStreamer) Exec(ctx context.Context, shellCmd string, args ...string) (string, error) {
	r.cmd = shellCmd
	r.args = append([]string(nil), args...)
	return r.output, r.err
}

func (r *recordingHookStreamer) ExecStream(ctx context.Context, stdout, stderr io.Writer, shellCmd string, args ...string) error {
	return nil
}

func newTestBoxer(t *testing.T, containerOps hostops.ContainerOps, imageOps hostops.ImageOps) *Boxer {
	t.Helper()
	tmpDir := path.Join(t.TempDir(), "Application Support", "Sand")
	boxer, err := NewBoxerWithDeps(tmpDir, BoxerDeps{
		ContainerService: containerOps,
		ImageService:     imageOps,
		GitOps:           &hostops.MockGitOps{},
		SSHim:            &mockSSHimmer{},
		FileOps: &hostops.MockFileOps{
			LstatFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			CreateFunc: func(path string) (*os.File, error) {
				return nil, nil
			},
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create test Boxer: %v", err)
	}
	t.Cleanup(func() { boxer.Close() })
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
		boxer.FileOps = &hostops.MockFileOps{
			MkdirAllFunc: os.MkdirAll,
			CreateFunc:   os.Create,
		}

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
		boxer.AgentRegistry.Register(&cloning.AgentConfig{
			Name:          "test-agent",
			Preparation:   testPrep,
			Configuration: testConfig,
		})

		hostWorkDir := t.TempDir()
		result, err := boxer.NewSandbox(ctx, NewSandboxOpts{AgentType: "test-agent", ID: "test-sandbox", HostWorkDir: hostWorkDir, ImageName: "test-image:latest", CPUs: 2, Memory: 1024})
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
		boxer.AgentRegistry.Register(&cloning.AgentConfig{
			Name:          "test-error-agent",
			Preparation:   testPrep,
			Configuration: testConfig,
		})

		_, err := boxer.NewSandbox(ctx, NewSandboxOpts{AgentType: "test-error-agent", ID: "test-sandbox", HostWorkDir: "/host/work", ImageName: "test-image", CPUs: 2, Memory: 1024})
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

	t.Run("snapshot ref error does not fail sandbox creation", func(t *testing.T) {
		mockContainer := &hostops.MockContainerOps{}
		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.FileOps = &hostops.MockFileOps{
			MkdirAllFunc: os.MkdirAll,
			CreateFunc:   os.Create,
		}
		hostWorkDir := t.TempDir()
		boxer.GitOps = &hostops.MockGitOps{
			TopLevelFunc: func(ctx context.Context, dir string) string {
				return hostWorkDir
			},
			CommitFunc: func(ctx context.Context, dir string) string {
				return "abc123"
			},
			UpdateRefFunc: func(ctx context.Context, dir, ref, value string) error {
				return errors.New("snapshot failed")
			},
		}

		testPrep := &mockWorkspacePreparation{
			prepareFunc: func(ctx context.Context, req cloning.CloneRequest) (*cloning.CloneArtifacts, error) {
				sandboxRoot := filepath.Join(boxer.appRoot, "clones", req.ID)
				return &cloning.CloneArtifacts{
					HostGitMirrorDir: "/mirror/repo.git",
					SandboxWorkDir:   sandboxRoot,
					PathRegistry:     cloning.NewStandardPathRegistry(sandboxRoot),
				}, nil
			},
		}
		boxer.AgentRegistry.Register(&cloning.AgentConfig{
			Name:          "test-snapshot-agent",
			Preparation:   testPrep,
			Configuration: &mockContainerConfiguration{},
		})

		if _, err := boxer.NewSandbox(ctx, NewSandboxOpts{AgentType: "test-snapshot-agent", ID: "test-sandbox", HostWorkDir: hostWorkDir, ImageName: "test-image:latest", CPUs: 2, Memory: 1024}); err != nil {
			t.Fatalf("NewSandbox() error = %v", err)
		}
	})
}

func TestBoxer_CreateContainerSSHAgentOptIn(t *testing.T) {
	ctx := context.Background()

	var createCalls []*options.CreateContainer
	mockContainer := &hostops.MockContainerOps{
		CreateFunc: func(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
			createCalls = append(createCalls, opts)
			return "test-container-123", nil
		},
	}
	mockImage := &mockImageOps{
		inspectFunc: func(ctx context.Context, name string) ([]*types.ImageManifest, error) {
			return []*types.ImageManifest{{
				Variants: []types.ImageVariant{{
					Config: types.ImageVariantConfig{
						Config: types.ImageVariantContainerConfig{
							Cmd: []string{"/bin/sh"},
						},
					},
				}},
			}}, nil
		},
	}

	boxer := newTestBoxer(t, mockContainer, mockImage)
	sbox := &sandtypes.Box{
		ID:        "test-sandbox",
		Name:      "friendly-name",
		ImageName: "test-image:latest",
		EnvFile:   "/tmp/test.env",
		Volumes:   []string{"/host:/container"},
		CPUs:      2,
		MemoryMB:  1024,
	}

	if err := boxer.CreateContainer(ctx, sbox, false); err != nil {
		t.Fatalf("CreateContainer(false) error = %v", err)
	}
	if err := boxer.CreateContainer(ctx, sbox, true); err != nil {
		t.Fatalf("CreateContainer(true) error = %v", err)
	}

	if len(createCalls) != 2 {
		t.Fatalf("expected 2 create calls, got %d", len(createCalls))
	}
	if createCalls[0].ManagementOptions.SSH {
		t.Fatal("first create call unexpectedly enabled ssh-agent forwarding")
	}
	if !createCalls[1].ManagementOptions.SSH {
		t.Fatal("second create call did not enable ssh-agent forwarding")
	}
	if got := len(sbox.Volumes); got != 1 {
		t.Fatalf("CreateContainer mutated sandbox volumes, len = %d, want 1", got)
	}
	for i, call := range createCalls {
		if got := call.ManagementOptions.Name; got != "friendly-name" {
			t.Fatalf("create call %d container name = %q, want friendly-name", i, got)
		}
		if got := len(call.ManagementOptions.Volume); got != 3 {
			t.Fatalf("create call %d volume count = %d, want 3", i, got)
		}
		if got, want := call.ManagementOptions.Volume[1], runtimepaths.ContainerHTTPSocketPath("test-sandbox")+":/run/host-services/sandd.sock"; got != want {
			t.Fatalf("create call %d HTTP socket volume = %q", i, got)
		}
		if got, want := call.ManagementOptions.Volume[2], runtimepaths.ContainerGRPCSocketPath("test-sandbox")+":/run/host-services/sandd.grpc.sock"; got != want {
			t.Fatalf("create call %d gRPC socket volume = %q", i, got)
		}
		if got := call.ProcessOptions.EnvFile; got != "" {
			t.Fatalf("create call %d unexpectedly passed env file %q", i, got)
		}
	}
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
	getStartupHooksFunc func(artifacts cloning.CloneArtifacts) []sandtypes.ContainerHook
	getStartHooksFunc   func(artifacts cloning.CloneArtifacts) []sandtypes.ContainerHook
}

// GetStartHooks implements [cloning.ContainerConfiguration].
func (m *mockContainerConfiguration) GetStartHooks(artifacts cloning.CloneArtifacts) []sandtypes.ContainerHook {
	if m.getStartHooksFunc != nil {
		return m.getStartHooksFunc(artifacts)
	}
	return []sandtypes.ContainerHook{}
}

var _ cloning.ContainerConfiguration = &mockContainerConfiguration{}

func (m *mockContainerConfiguration) GetMounts(artifacts cloning.CloneArtifacts) []sandtypes.MountSpec {
	if m.getMountsFunc != nil {
		return m.getMountsFunc(artifacts)
	}
	return []sandtypes.MountSpec{}
}

func (m *mockContainerConfiguration) GetFirstStartHooks(artifacts cloning.CloneArtifacts) []sandtypes.ContainerHook {
	if m.getStartupHooksFunc != nil {
		return m.getStartupHooksFunc(artifacts)
	}
	return []sandtypes.ContainerHook{}
}

func TestInnieSocketPermissionHookRunsRepairScript(t *testing.T) {
	streamer := &recordingHookStreamer{}

	if err := innieSocketPermissionHook().Run(context.Background(), nil, streamer); err != nil {
		t.Fatalf("innieSocketPermissionHook() error = %v", err)
	}

	if streamer.cmd != "sh" {
		t.Fatalf("innieSocketPermissionHook() command = %q, want sh", streamer.cmd)
	}
	wantArgs := []string{"-c", innieSocketPermissionScript}
	if len(streamer.args) != len(wantArgs) {
		t.Fatalf("innieSocketPermissionHook() args = %q, want %q", streamer.args, wantArgs)
	}
	for i := range wantArgs {
		if streamer.args[i] != wantArgs[i] {
			t.Fatalf("innieSocketPermissionHook() args = %q, want %q", streamer.args, wantArgs)
		}
	}
}

func TestInnieSocketPermissionHookPropagatesExecError(t *testing.T) {
	expectedErr := errors.New("chmod failed")
	streamer := &recordingHookStreamer{
		output: "permission denied\n",
		err:    expectedErr,
	}

	err := innieSocketPermissionHook().Run(context.Background(), nil, streamer)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("innieSocketPermissionHook() error = %v, want %v", err, expectedErr)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("innieSocketPermissionHook() error missing command output: %v", err)
	}
}

func TestBoxer_StartContainersPrependInnieSocketPermissionHook(t *testing.T) {
	for _, tc := range []struct {
		name      string
		startFunc func(*Boxer, *sandtypes.Box) error
		config    *mockContainerConfiguration
	}{
		{
			name: "first start",
			startFunc: func(boxer *Boxer, sbox *sandtypes.Box) error {
				return boxer.StartNewContainer(context.Background(), sbox, nil)
			},
			config: &mockContainerConfiguration{
				getStartupHooksFunc: func(artifacts cloning.CloneArtifacts) []sandtypes.ContainerHook {
					return []sandtypes.ContainerHook{agentHookForTest("agent first-start hook")}
				},
			},
		},
		{
			name: "existing start",
			startFunc: func(boxer *Boxer, sbox *sandtypes.Box) error {
				return boxer.StartExistingContainer(context.Background(), sbox)
			},
			config: &mockContainerConfiguration{
				getStartHooksFunc: func(artifacts cloning.CloneArtifacts) []sandtypes.ContainerHook {
					return []sandtypes.ContainerHook{agentHookForTest("agent restart hook")}
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var execCalls []string
			mockContainer := &hostops.MockContainerOps{
				ExecFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
					execCalls = append(execCalls, strings.Join(append([]string{cmd}, args...), " "))
					return "", nil
				},
			}
			boxer := newTestBoxer(t, mockContainer, &mockImageOps{})
			boxer.AgentRegistry.Register(&cloning.AgentConfig{
				Name:          "default",
				Configuration: tc.config,
			})

			sbox := &sandtypes.Box{
				ID:             "test-sandbox",
				AgentType:      "default",
				ContainerID:    "test-container",
				SandboxWorkDir: t.TempDir(),
				Username:       "user",
				Uid:            "1000",
			}
			if err := tc.startFunc(boxer, sbox); err != nil {
				t.Fatalf("%s error = %v", tc.name, err)
			}

			if len(execCalls) != 2 {
				t.Fatalf("%s exec calls = %v, want 2 calls", tc.name, execCalls)
			}
			if got, want := execCalls[0], "sh -c "+innieSocketPermissionScript; got != want {
				t.Fatalf("%s first exec call = %q, want %q", tc.name, got, want)
			}
			if !strings.HasPrefix(execCalls[1], "agent-hook ") {
				t.Fatalf("%s second exec call = %q, want agent hook", tc.name, execCalls[1])
			}
		})
	}
}

func agentHookForTest(name string) sandtypes.ContainerHook {
	return sandtypes.NewContainerHook(name, func(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer) error {
		_, err := exec.Exec(ctx, "agent-hook", name)
		return err
	})
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
		var renameCalls []struct{ oldpath, newpath string }

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

		mockGit := &hostops.MockGitOps{
			RemoveRemoteFunc: func(ctx context.Context, dir, name string) error {
				removeRemoteCalls = append(removeRemoteCalls, struct{ dir, name string }{dir, name})
				return nil
			},
		}

		mockFile := &hostops.MockFileOps{
			RenameFunc: func(oldpath, newpath string) error {
				renameCalls = append(renameCalls, struct{ oldpath, newpath string }{oldpath: oldpath, newpath: newpath})
				return nil
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.GitOps = mockGit
		boxer.FileOps = mockFile

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

		wantTrashDir := filepath.Join(boxer.appRoot, "trash", "sandboxes", "test-sandbox")
		if len(renameCalls) != 1 || renameCalls[0].oldpath != sandboxDir || renameCalls[0].newpath != wantTrashDir {
			t.Errorf("Expected Rename(%q, %q), got: %v", sandboxDir, wantTrashDir, renameCalls)
		}

		loadedBox, err := boxer.Get(ctx, "test-sandbox")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if loadedBox != nil {
			t.Error("Expected sandbox to be hidden from active lookup")
		}
		deletedBox, err := boxer.GetByID(ctx, "test-sandbox")
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if deletedBox == nil || deletedBox.State != "deleted" || deletedBox.TrashWorkDir != wantTrashDir {
			t.Fatalf("Expected deleted sandbox with trash dir %q, got %#v", wantTrashDir, deletedBox)
		}
	})

	t.Run("cleanup logs container errors but continues", func(t *testing.T) {
		var renameCalled bool
		mockContainer := &hostops.MockContainerOps{
			StopFunc: func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
				return "", errors.New("stop failed")
			},
			DeleteFunc: func(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
				return "", errors.New("delete failed")
			},
		}

		mockFile := &hostops.MockFileOps{
			RenameFunc: func(oldpath, newpath string) error {
				renameCalled = true
				return nil
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.FileOps = mockFile

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

		if !renameCalled {
			t.Error("Expected cleanup to continue to trash move despite container errors")
		}
	})

	t.Run("cleanup returns error on git failure", func(t *testing.T) {
		expectedErr := errors.New("git remove remote failed")
		mockContainer := &hostops.MockContainerOps{}
		mockGit := &hostops.MockGitOps{
			RemoveRemoteFunc: func(ctx context.Context, dir, name string) error {
				return expectedErr
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.GitOps = mockGit

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

	t.Run("cleanup returns error on trash move failure", func(t *testing.T) {
		expectedErr := errors.New("rename failed")
		mockContainer := &hostops.MockContainerOps{}
		mockFile := &hostops.MockFileOps{
			RenameFunc: func(oldpath, newpath string) error {
				return expectedErr
			},
			CopyFunc: func(ctx context.Context, src, dst string) error {
				return expectedErr
			},
		}

		mockImage := &mockImageOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)
		boxer.FileOps = mockFile

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
		if err == nil {
			t.Fatal("Expected trash move failure to be returned")
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

		err := boxer.EnsureImage(ctx, "test-image:latest", io.Discard)
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
			pullFunc: func(ctx context.Context, image string, w io.Writer) (func() error, error) {
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

		err := boxer.EnsureImage(ctx, "new-image:latest", io.Discard)
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

		err := boxer.EnsureImage(ctx, "test-image:latest", io.Discard)
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
			pullFunc: func(ctx context.Context, image string, w io.Writer) (func() error, error) {
				return nil, expectedErr
			},
		}

		mockContainer := &hostops.MockContainerOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		err := boxer.EnsureImage(ctx, "test-image:latest", io.Discard)
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
			pullFunc: func(ctx context.Context, image string, w io.Writer) (func() error, error) {
				return func() error {
					return expectedErr
				}, nil
			},
		}

		mockContainer := &hostops.MockContainerOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		err := boxer.EnsureImage(ctx, "test-image:latest", io.Discard)
		if err == nil {
			t.Fatal("Expected error from wait, got nil")
		}
	})
}

func TestBoxer_ExecuteHooks_StreamsProgress(t *testing.T) {
	ctx := context.Background()

	var execStreamCalls []string
	var execStreamEnvFiles []string
	mockContainer := &hostops.MockContainerOps{
		InspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
			return []types.Container{{Status: "running"}}, nil
		},
		ExecStreamFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer, cmdArgs ...string) (func() error, error) {
			execStreamCalls = append(execStreamCalls, cmd)
			execStreamEnvFiles = append(execStreamEnvFiles, opts.ProcessOptions.EnvFile)
			if _, err := io.WriteString(stdout, "warming cache\n"); err != nil {
				return nil, err
			}
			return func() error { return nil }, nil
		},
	}
	mockImage := &mockImageOps{}
	boxer := newTestBoxer(t, mockContainer, mockImage)

	hooks := []sandtypes.ContainerHook{
		sandtypes.NewContainerHook("streamed hook", func(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer) error {
			return exec.ExecStream(ctx, io.Discard, io.Discard, "mise.sh")
		}),
	}

	var progress bytes.Buffer
	err := boxer.executeHooks(ctx, &sandtypes.Box{
		ID:          "test-sandbox",
		ContainerID: "test-container",
		EnvFile:     "/tmp/test.env",
	}, hooks, &progress)
	if err != nil {
		t.Fatalf("executeHooks() error = %v", err)
	}

	if len(execStreamCalls) != 1 || execStreamCalls[0] != "mise.sh" {
		t.Fatalf("executeHooks() ExecStream calls = %v, want [mise.sh]", execStreamCalls)
	}
	if len(execStreamEnvFiles) != 1 || execStreamEnvFiles[0] != "" {
		t.Fatalf("executeHooks() ExecStream env files = %v, want [\"\"]", execStreamEnvFiles)
	}

	got := progress.String()
	if !strings.Contains(got, "[sand] streamed hook\n") {
		t.Fatalf("executeHooks() progress missing hook banner: %q", got)
	}
	if !strings.Contains(got, "warming cache\n") {
		t.Fatalf("executeHooks() progress missing streamed output: %q", got)
	}
}

func TestBoxer_ExecuteHooks_DoesNotPassEnvFileToExec(t *testing.T) {
	ctx := context.Background()

	var execEnvFiles []string
	mockContainer := &hostops.MockContainerOps{
		InspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
			return []types.Container{{Status: "running"}}, nil
		},
		ExecFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
			execEnvFiles = append(execEnvFiles, opts.ProcessOptions.EnvFile)
			return "", nil
		},
	}
	boxer := newTestBoxer(t, mockContainer, &mockImageOps{})

	hooks := []sandtypes.ContainerHook{
		sandtypes.NewContainerHook("plain hook", func(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer) error {
			_, err := exec.Exec(ctx, "setup.sh")
			return err
		}),
	}

	err := boxer.executeHooks(ctx, &sandtypes.Box{
		ID:          "test-sandbox",
		ContainerID: "test-container",
		EnvFile:     "/tmp/test.env",
	}, hooks, nil)
	if err != nil {
		t.Fatalf("executeHooks() error = %v", err)
	}

	if len(execEnvFiles) != 1 || execEnvFiles[0] != "" {
		t.Fatalf("executeHooks() Exec env files = %v, want [\"\"]", execEnvFiles)
	}
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
