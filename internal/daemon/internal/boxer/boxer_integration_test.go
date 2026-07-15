package boxer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/imageprogress"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/sshimmer"
)

type mockImageOps struct {
	listFunc    func(ctx context.Context) ([]sandtypes.ImageEntry, error)
	pullFunc    func(ctx context.Context, image string, progress imageprogress.Sink) (func() error, error)
	inspectFunc func(ctx context.Context, name string) ([]*sandtypes.ImageManifest, error)
}

// Inspect implements [hostops.ImageOps].
func (m *mockImageOps) Inspect(ctx context.Context, name string) ([]*sandtypes.ImageManifest, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, name)
	}
	return nil, nil
}

func (m *mockImageOps) List(ctx context.Context) ([]sandtypes.ImageEntry, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []sandtypes.ImageEntry{}, nil
}

func (m *mockImageOps) Pull(ctx context.Context, image string, progress imageprogress.Sink) (func() error, error) {
	if m.pullFunc != nil {
		return m.pullFunc(ctx, image, progress)
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
	cmd         string
	args        []string
	output      string
	err         error
	execResults map[string]struct {
		output string
		err    error
	}
	calls []string
}

func (r *recordingHookStreamer) Exec(ctx context.Context, shellCmd string, args ...string) (string, error) {
	r.cmd = shellCmd
	r.args = append([]string(nil), args...)
	call := strings.Join(append([]string{shellCmd}, args...), " ")
	r.calls = append(r.calls, call)
	if res, ok := r.execResults[call]; ok {
		return res.output, res.err
	}
	return r.output, r.err
}

func (r *recordingHookStreamer) ExecStream(ctx context.Context, stdout, stderr io.Writer, shellCmd string, args ...string) error {
	return nil
}

func (r *recordingHookStreamer) ExecStreamInput(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, shellCmd string, args ...string) error {
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
			CreateFunc: func(ctx context.Context, opts *hostops.CreateContainer, image string, args []string) (string, error) {
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
		result, err := boxer.NewSandbox(ctx, NewSandboxOpts{AgentType: "test-agent", ID: "test-sandbox", HostWorkDir: hostWorkDir, ImageName: "test-image:latest", CPUs: 2, Memory: 1024, LocalDomain: "test.local"})
		if err != nil {
			t.Fatalf("NewSandbox() error = %v", err)
		}

		if result.ID != "test-sandbox" {
			t.Errorf("Expected ID 'test-sandbox', got %s", result.ID)
		}

		if result.ImageName != "test-image:latest" {
			t.Errorf("Expected ImageName 'test-image:latest', got %s", result.ImageName)
		}

		if result.DNSDomain != "test.local" {
			t.Errorf("Expected DNSDomain 'test.local', got %s", result.DNSDomain)
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
		if loadedBox.DNSDomain != "test.local" {
			t.Errorf("Expected loaded DNSDomain 'test.local', got %s", loadedBox.DNSDomain)
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

	var createCalls []*hostops.CreateContainer
	mockContainer := &hostops.MockContainerOps{
		CreateFunc: func(ctx context.Context, opts *hostops.CreateContainer, image string, args []string) (string, error) {
			createCalls = append(createCalls, opts)
			return "test-container-123", nil
		},
	}
	mockImage := &mockImageOps{
		inspectFunc: func(ctx context.Context, name string) ([]*sandtypes.ImageManifest, error) {
			return []*sandtypes.ImageManifest{{
				Variants: []sandtypes.ImageVariant{{
					Config: sandtypes.ImageVariantConfig{
						Config: sandtypes.ImageVariantContainerConfig{
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
		MountRequests: []sandtypes.MountRequest{{
			Kind:    sandtypes.MountKindBind,
			Runtime: "type=bind,source=/host,target=/container",
		}},
		CPUs:     2,
		MemoryMB: 1024,
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
	if got := len(sbox.MountRequests); got != 1 {
		t.Fatalf("CreateContainer mutated sandbox mount requests, len = %d, want 1", got)
	}
	for i, call := range createCalls {
		if got := call.ManagementOptions.Name; got != "friendly-name" {
			t.Fatalf("create call %d container name = %q, want friendly-name", i, got)
		}
		if got := len(call.ManagementOptions.Volume); got != 2 {
			t.Fatalf("create call %d volume count = %d, want 2", i, got)
		}
		if got := len(call.ManagementOptions.Mount); got != 1 {
			t.Fatalf("create call %d mount count = %d, want 1", i, got)
		}
		if got, want := call.ManagementOptions.Mount[0], "type=bind,source=/host,target=/container"; got != want {
			t.Fatalf("create call %d user mount = %q, want %q", i, got, want)
		}
		if got := call.ProcessOptions.EnvFile; got != "" {
			t.Fatalf("create call %d unexpectedly passed env file %q", i, got)
		}
	}
}

func TestBoxer_RenameSandboxRegeneratesSSHKeysAndMarksReplacementUnbootstrapped(t *testing.T) {
	ctx := context.Background()
	oldContainerID := "ancient-smoke"
	newContainerID := "mysandbox"
	sandboxDir := t.TempDir()

	var newKeysCalls []struct {
		domain   string
		username string
	}
	var deleteCalls []string
	var createCalls []*hostops.CreateContainer
	mockContainer := &hostops.MockContainerOps{
		InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
			switch containerID {
			case oldContainerID:
				return []sandtypes.Container{{
					Status: sandtypes.ContainerStatus{State: "stopped"},
					Configuration: sandtypes.ContainerConfig{
						SSH: true,
					},
				}}, nil
			case newContainerID:
				return []sandtypes.Container{{
					Status: sandtypes.ContainerStatus{State: "stopped"},
					Configuration: sandtypes.ContainerConfig{
						SSH: true,
					},
				}}, nil
			default:
				return nil, nil
			}
		},
		DeleteFunc: func(ctx context.Context, opts *hostops.DeleteContainer, containerID string) (string, error) {
			deleteCalls = append(deleteCalls, containerID)
			return "deleted", nil
		},
		CreateFunc: func(ctx context.Context, opts *hostops.CreateContainer, image string, args []string) (string, error) {
			createCalls = append(createCalls, opts)
			return newContainerID, nil
		},
	}

	boxer := newTestBoxer(t, mockContainer, &mockImageOps{})
	boxer.FileOps = &hostops.MockFileOps{
		MkdirAllFunc: os.MkdirAll,
		CreateFunc:   os.Create,
	}
	boxer.SSHim = &mockSSHimmer{
		newKeysFunc: func(ctx context.Context, domain, username string) (*sshimmer.Keys, error) {
			newKeysCalls = append(newKeysCalls, struct {
				domain   string
				username string
			}{domain: domain, username: username})
			return &sshimmer.Keys{
				HostKey:     []byte("new-host-key"),
				HostKeyPub:  []byte("new-host-key-pub"),
				HostKeyCert: []byte("new-host-key-cert"),
				UserCAPub:   []byte("new-user-ca-pub"),
			}, nil
		},
	}

	if err := boxer.SaveSandbox(ctx, &sandtypes.Box{
		ID:                    "sandbox-id",
		Name:                  "ancient-smoke",
		ContainerID:           oldContainerID,
		ContainerBootstrapped: true,
		HostOriginDir:         t.TempDir(),
		SandboxWorkDir:        sandboxDir,
		ImageName:             "test-image:latest",
		DNSDomain:             "dev.local",
		Username:              "sean",
		Uid:                   "1000",
	}); err != nil {
		t.Fatalf("SaveSandbox() error = %v", err)
	}

	got, err := boxer.RenameSandbox(ctx, "ancient-smoke", "mysandbox", io.Discard)
	if err != nil {
		t.Fatalf("RenameSandbox() error = %v", err)
	}
	if got.Name != "mysandbox" {
		t.Fatalf("renamed sandbox name = %q, want mysandbox", got.Name)
	}

	if len(newKeysCalls) != 1 || newKeysCalls[0].domain != "mysandbox.dev.local" || newKeysCalls[0].username != "sean" {
		t.Fatalf("NewKeys calls = %#v, want mysandbox.dev.local/sean", newKeysCalls)
	}
	if len(deleteCalls) != 1 || deleteCalls[0] != oldContainerID {
		t.Fatalf("Delete calls = %v, want [%s]", deleteCalls, oldContainerID)
	}
	if len(createCalls) != 1 {
		t.Fatalf("Create calls = %d, want 1", len(createCalls))
	}
	if createCalls[0].ManagementOptions.Name != "mysandbox" {
		t.Fatalf("created container name = %q, want mysandbox", createCalls[0].ManagementOptions.Name)
	}
	if !createCalls[0].ManagementOptions.SSH {
		t.Fatal("renamed container did not preserve ssh-agent forwarding")
	}

	keyPath := filepath.Join(sandboxDir, "sshkeys", "ssh_host_key")
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", keyPath, err)
	}
	if string(keyBytes) != "new-host-key" {
		t.Fatalf("ssh host key = %q, want new-host-key", keyBytes)
	}

	loaded, err := boxer.Get(ctx, "mysandbox")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.ContainerID != newContainerID {
		t.Fatalf("ContainerID = %q, want %q", loaded.ContainerID, newContainerID)
	}
	if loaded.ContainerBootstrapped {
		t.Fatal("ContainerBootstrapped = true, want false after rename replacement")
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
	streamer := &recordingHookStreamer{
		execResults: map[string]struct {
			output string
			err    error
		}{
			"test -e /run/host-services":                 {},
			"test -e /run/host-services/sandd.grpc.sock": {},
			"test -e /run/host-services/sandd.sock":      {},
		},
	}

	if err := innieSocketPermissionHook().Run(context.Background(), nil, streamer); err != nil {
		t.Fatalf("innieSocketPermissionHook() error = %v", err)
	}

	wantCalls := []string{
		"test -e /run/host-services",
		"chmod 755 /run/host-services",
		"test -e /run/host-services/sandd.grpc.sock",
		"chmod 666 /run/host-services/sandd.grpc.sock",
		"test -e /run/host-services/sandd.sock",
		"chmod 666 /run/host-services/sandd.sock",
	}
	if !reflect.DeepEqual(streamer.calls, wantCalls) {
		t.Fatalf("innieSocketPermissionHook() calls = %q, want %q", streamer.calls, wantCalls)
	}
}

func TestInnieSocketPermissionHookPropagatesExecError(t *testing.T) {
	expectedErr := errors.New("chmod failed")
	streamer := &recordingHookStreamer{
		execResults: map[string]struct {
			output string
			err    error
		}{
			"test -e /run/host-services": {},
			"chmod 755 /run/host-services": {
				output: "permission denied\n",
				err:    expectedErr,
			},
		},
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
				ExecFunc: func(ctx context.Context, opts *hostops.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
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

			if len(execCalls) != 7 {
				t.Fatalf("%s exec calls = %v, want socket checks plus agent hook", tc.name, execCalls)
			}
			if got, want := execCalls[0], "test -e /run/host-services"; got != want {
				t.Fatalf("%s first exec call = %q, want %q", tc.name, got, want)
			}
			if !strings.HasPrefix(execCalls[len(execCalls)-1], "agent-hook ") {
				t.Fatalf("%s last exec call = %q, want agent hook", tc.name, execCalls[len(execCalls)-1])
			}
		})
	}
}

func agentHookForTest(name string) sandtypes.ContainerHook {
	return sandtypes.NewContainerHook(name, func(ctx context.Context, ctr *sandtypes.Container, exec sandtypes.HookStreamer) error {
		_, err := exec.Exec(ctx, "agent-hook", name)
		return err
	})
}

func TestBoxer_Sync(t *testing.T) {
	ctx := context.Background()

	t.Run("syncs all sandboxes", func(t *testing.T) {
		var inspectCalls []string

		mockContainer := &hostops.MockContainerOps{
			InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
				inspectCalls = append(inspectCalls, containerID)
				return []sandtypes.Container{{Status: sandtypes.ContainerStatus{State: "running"}}}, nil
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
			InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
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
			StopFunc: func(ctx context.Context, opts *hostops.StopContainer, containerID string) (string, error) {
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
			DeleteFunc: func(ctx context.Context, opts *hostops.DeleteContainer, containerID string) (string, error) {
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
			StopFunc: func(ctx context.Context, opts *hostops.StopContainer, containerID string) (string, error) {
				return "", errors.New("stop failed")
			},
			DeleteFunc: func(ctx context.Context, opts *hostops.DeleteContainer, containerID string) (string, error) {
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
			listFunc: func(ctx context.Context) ([]sandtypes.ImageEntry, error) {
				return []sandtypes.ImageEntry{
					{Configuration: sandtypes.ImageConfiguration{Name: "test-image:latest"}},
					{Configuration: sandtypes.ImageConfiguration{Name: "other-image:v1"}},
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
			listFunc: func(ctx context.Context) ([]sandtypes.ImageEntry, error) {
				return []sandtypes.ImageEntry{
					{Configuration: sandtypes.ImageConfiguration{Name: "other-image:v1"}},
				}, nil
			},
			pullFunc: func(ctx context.Context, image string, progress imageprogress.Sink) (func() error, error) {
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
			listFunc: func(ctx context.Context) ([]sandtypes.ImageEntry, error) {
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

	t.Run("container system not running list error", func(t *testing.T) {
		mockImage := &mockImageOps{
			listFunc: func(ctx context.Context) ([]sandtypes.ImageEntry, error) {
				return nil, errors.New("container system service is not running")
			},
		}

		mockContainer := &hostops.MockContainerOps{}
		boxer := newTestBoxer(t, mockContainer, mockImage)

		err := boxer.EnsureImage(ctx, "test-image:latest", io.Discard)
		if err == nil {
			t.Fatal("Expected error from list, got nil")
		}
		if got := err.Error(); !strings.Contains(got, "container system start") {
			t.Fatalf("EnsureImage() error = %q, want container system start remedy", got)
		}
		if got := err.Error(); strings.Contains(got, "failed to list images") {
			t.Fatalf("EnsureImage() error = %q, should not obscure system service remedy", got)
		}
	})

	t.Run("pull error", func(t *testing.T) {
		expectedErr := errors.New("pull failed")
		mockImage := &mockImageOps{
			listFunc: func(ctx context.Context) ([]sandtypes.ImageEntry, error) {
				return []sandtypes.ImageEntry{}, nil
			},
			pullFunc: func(ctx context.Context, image string, progress imageprogress.Sink) (func() error, error) {
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
			listFunc: func(ctx context.Context) ([]sandtypes.ImageEntry, error) {
				return []sandtypes.ImageEntry{}, nil
			},
			pullFunc: func(ctx context.Context, image string, progress imageprogress.Sink) (func() error, error) {
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
	var execStreamTTY []bool
	mockContainer := &hostops.MockContainerOps{
		InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
			return []sandtypes.Container{{Status: sandtypes.ContainerStatus{State: "running"}}}, nil
		},
		ExecStreamFunc: func(ctx context.Context, opts *hostops.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer, cmdArgs ...string) (func() error, error) {
			execStreamCalls = append(execStreamCalls, cmd)
			execStreamEnvFiles = append(execStreamEnvFiles, opts.ProcessOptions.EnvFile)
			execStreamTTY = append(execStreamTTY, opts.ProcessOptions.TTY)
			if _, err := io.WriteString(stdout, "warming cache\n"); err != nil {
				return nil, err
			}
			return func() error { return nil }, nil
		},
	}
	mockImage := &mockImageOps{}
	boxer := newTestBoxer(t, mockContainer, mockImage)

	hooks := []sandtypes.ContainerHook{
		sandtypes.NewContainerHook("streamed hook", func(ctx context.Context, ctr *sandtypes.Container, exec sandtypes.HookStreamer) error {
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
	if len(execStreamTTY) != 1 || execStreamTTY[0] {
		t.Fatalf("executeHooks() ExecStream TTY = %v, want [false]", execStreamTTY)
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
	var execTTY []bool
	mockContainer := &hostops.MockContainerOps{
		InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
			return []sandtypes.Container{{Status: sandtypes.ContainerStatus{State: "running"}}}, nil
		},
		ExecFunc: func(ctx context.Context, opts *hostops.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
			execEnvFiles = append(execEnvFiles, opts.ProcessOptions.EnvFile)
			execTTY = append(execTTY, opts.ProcessOptions.TTY)
			return "", nil
		},
	}
	boxer := newTestBoxer(t, mockContainer, &mockImageOps{})

	hooks := []sandtypes.ContainerHook{
		sandtypes.NewContainerHook("plain hook", func(ctx context.Context, ctr *sandtypes.Container, exec sandtypes.HookStreamer) error {
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
	if len(execTTY) != 1 || execTTY[0] {
		t.Fatalf("executeHooks() Exec TTY = %v, want [false]", execTTY)
	}
}

func TestBoxer_StopContainer(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		var stopCalls []string
		mockContainer := &hostops.MockContainerOps{
			StopFunc: func(ctx context.Context, opts *hostops.StopContainer, containerID string) (string, error) {
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
			StopFunc: func(ctx context.Context, opts *hostops.StopContainer, containerID string) (string, error) {
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
