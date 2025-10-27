package sand

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
)

type mockContainerOps struct {
	createFunc     func(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error)
	startFunc      func(ctx context.Context, opts *options.StartContainer, containerID string) (string, error)
	stopFunc       func(ctx context.Context, opts *options.StopContainer, containerID string) (string, error)
	deleteFunc     func(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error)
	execFunc       func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error)
	execStreamFunc func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer) (func() error, error)
	inspectFunc    func(ctx context.Context, containerID string) ([]types.Container, error)
}

func (m *mockContainerOps) Create(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, opts, image, args)
	}
	return "mock-container-id", nil
}

func (m *mockContainerOps) Start(ctx context.Context, opts *options.StartContainer, containerID string) (string, error) {
	if m.startFunc != nil {
		return m.startFunc(ctx, opts, containerID)
	}
	return "started", nil
}

func (m *mockContainerOps) Stop(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
	if m.stopFunc != nil {
		return m.stopFunc(ctx, opts, containerID)
	}
	return "stopped", nil
}

func (m *mockContainerOps) Delete(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, opts, containerID)
	}
	return "deleted", nil
}

func (m *mockContainerOps) Exec(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, opts, containerID, cmd, env, args...)
	}
	return "exec output", nil
}

func (m *mockContainerOps) ExecStream(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer) (func() error, error) {
	if m.execStreamFunc != nil {
		return m.execStreamFunc(ctx, opts, containerID, cmd, env, stdin, stdout, stderr)
	}
	return func() error { return nil }, nil
}

func (m *mockContainerOps) Inspect(ctx context.Context, containerID string) ([]types.Container, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, containerID)
	}
	return []types.Container{{Status: "running"}}, nil
}

func TestBox_EffectiveMounts(t *testing.T) {
	tests := []struct {
		name     string
		box      *Box
		wantLen  int
		validate func(t *testing.T, mounts []MountSpec)
	}{
		{
			name: "custom mounts",
			box: &Box{
				SandboxWorkDir: "/tmp/sandbox",
				Mounts: []MountSpec{
					{Source: "/custom", Target: "/target", ReadOnly: false},
				},
			},
			wantLen: 1,
			validate: func(t *testing.T, mounts []MountSpec) {
				if mounts[0].Source != "/custom" {
					t.Errorf("Expected source /custom, got %s", mounts[0].Source)
				}
				if mounts[0].Target != "/target" {
					t.Errorf("Expected target /target, got %s", mounts[0].Target)
				}
			},
		},
		{
			name: "default mounts",
			box: &Box{
				SandboxWorkDir: "/tmp/sandbox",
				Mounts:         nil,
			},
			wantLen: 3,
			validate: func(t *testing.T, mounts []MountSpec) {
				if mounts[0].Source != "/tmp/sandbox/hostkeys" {
					t.Errorf("Expected hostkeys mount, got %s", mounts[0].Source)
				}
				if mounts[0].Target != "/hostkeys" {
					t.Errorf("Expected target /hostkeys, got %s", mounts[0].Target)
				}
				if !mounts[0].ReadOnly {
					t.Error("Expected hostkeys mount to be readonly")
				}

				if mounts[1].Source != "/tmp/sandbox/dotfiles" {
					t.Errorf("Expected dotfiles mount, got %s", mounts[1].Source)
				}
				if mounts[1].Target != "/dotfiles" {
					t.Errorf("Expected target /dotfiles, got %s", mounts[1].Target)
				}
				if !mounts[1].ReadOnly {
					t.Error("Expected dotfiles mount to be readonly")
				}

				if mounts[2].Source != "/tmp/sandbox/app" {
					t.Errorf("Expected app mount, got %s", mounts[2].Source)
				}
				if mounts[2].Target != "/app" {
					t.Errorf("Expected target /app, got %s", mounts[2].Target)
				}
				if mounts[2].ReadOnly {
					t.Error("Expected app mount to be readwrite")
				}
			},
		},
		{
			name: "no workdir",
			box: &Box{
				SandboxWorkDir: "",
				Mounts:         nil,
			},
			wantLen: 0,
		},
		{
			name: "custom mounts override defaults",
			box: &Box{
				SandboxWorkDir: "/tmp/sandbox",
				Mounts: []MountSpec{
					{Source: "/override1", Target: "/target1"},
					{Source: "/override2", Target: "/target2"},
				},
			},
			wantLen: 2,
			validate: func(t *testing.T, mounts []MountSpec) {
				if mounts[0].Source != "/override1" {
					t.Errorf("Expected source /override1, got %s", mounts[0].Source)
				}
				if mounts[1].Source != "/override2" {
					t.Errorf("Expected source /override2, got %s", mounts[1].Source)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.box.effectiveMounts()
			if len(got) != tt.wantLen {
				t.Errorf("effectiveMounts() returned %d mounts, want %d", len(got), tt.wantLen)
			}
			if tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

func TestBox_GetContainer(t *testing.T) {
	ctx := context.Background()

	t.Run("container found", func(t *testing.T) {
		mockOps := &mockContainerOps{
			inspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				if containerID != "test-container-123" {
					t.Errorf("Expected containerID test-container-123, got %s", containerID)
				}
				return []types.Container{{Status: "running"}}, nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container-123",
			containerService: mockOps,
		}

		container, err := box.GetContainer(ctx)
		if err != nil {
			t.Fatalf("GetContainer() error = %v", err)
		}
		if container == nil {
			t.Fatal("Expected container, got nil")
		}
		if container.Status != "running" {
			t.Errorf("Expected status running, got %s", container.Status)
		}
	})

	t.Run("container not found", func(t *testing.T) {
		mockOps := &mockContainerOps{
			inspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{}, nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "nonexistent",
			containerService: mockOps,
		}

		container, err := box.GetContainer(ctx)
		if err != nil {
			t.Fatalf("GetContainer() error = %v", err)
		}
		if container != nil {
			t.Error("Expected nil container for not found")
		}
	})

	t.Run("inspect error", func(t *testing.T) {
		expectedErr := errors.New("inspect failed")
		mockOps := &mockContainerOps{
			inspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				return nil, expectedErr
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "error-container",
			containerService: mockOps,
		}

		_, err := box.GetContainer(ctx)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-box") {
			t.Errorf("Error should contain sandbox ID 'test-box', got: %v", err)
		}
	})
}

func TestBox_Sync(t *testing.T) {
	ctx := context.Background()

	t.Run("work dir missing", func(t *testing.T) {
		mockOps := &mockContainerOps{
			inspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "running"}}, nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			SandboxWorkDir:   "/nonexistent/path/that/does/not/exist",
			containerService: mockOps,
		}

		err := box.Sync(ctx)
		if err != nil {
			t.Fatalf("Sync() should not return error, got: %v", err)
		}
		if box.SandboxWorkDirError != "NO CLONE DIR" {
			t.Errorf("Expected SandboxWorkDirError to be 'NO CLONE DIR', got: %s", box.SandboxWorkDirError)
		}
	})

	t.Run("container missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		mockOps := &mockContainerOps{
			inspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				return nil, errors.New("container not found")
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "missing-container",
			SandboxWorkDir:   tmpDir,
			containerService: mockOps,
		}

		err := box.Sync(ctx)
		if err != nil {
			t.Fatalf("Sync() should not return error, got: %v", err)
		}
		if !strings.Contains(box.SandboxContainerError, "NO CONTAINER") {
			t.Errorf("Expected SandboxContainerError to contain 'NO CONTAINER', got: %s", box.SandboxContainerError)
		}
	})

	t.Run("both present", func(t *testing.T) {
		tmpDir := t.TempDir()

		mockOps := &mockContainerOps{
			inspectFunc: func(ctx context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "running"}}, nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "good-container",
			SandboxWorkDir:   tmpDir,
			containerService: mockOps,
		}

		err := box.Sync(ctx)
		if err != nil {
			t.Fatalf("Sync() error = %v", err)
		}
		if box.SandboxWorkDirError != "" {
			t.Errorf("Expected no SandboxWorkDirError, got: %s", box.SandboxWorkDirError)
		}
		if box.SandboxContainerError != "" {
			t.Errorf("Expected no SandboxContainerError, got: %s", box.SandboxContainerError)
		}
	})
}

func TestBox_CreateContainer(t *testing.T) {
	ctx := context.Background()

	t.Run("success with default mounts", func(t *testing.T) {
		tmpDir := t.TempDir()
		var capturedOpts *options.CreateContainer
		var capturedImage string

		mockOps := &mockContainerOps{
			createFunc: func(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
				capturedOpts = opts
				capturedImage = image
				return "new-container-id", nil
			},
		}

		box := &Box{
			ID:               "test-box",
			SandboxWorkDir:   tmpDir,
			ImageName:        "test-image:latest",
			DNSDomain:        "test.local",
			EnvFile:          "/tmp/.env",
			containerService: mockOps,
		}

		err := box.CreateContainer(ctx)
		if err != nil {
			t.Fatalf("CreateContainer() error = %v", err)
		}

		if box.ContainerID != "new-container-id" {
			t.Errorf("Expected ContainerID to be 'new-container-id', got: %s", box.ContainerID)
		}

		if capturedImage != "test-image:latest" {
			t.Errorf("Expected image 'test-image:latest', got: %s", capturedImage)
		}

		if capturedOpts == nil {
			t.Fatal("Expected CreateContainer options to be captured")
		}

		if capturedOpts.Name != "test-box" {
			t.Errorf("Expected container name 'test-box', got: %s", capturedOpts.Name)
		}

		if capturedOpts.DNSDomain != "test.local" {
			t.Errorf("Expected DNS domain 'test.local', got: %s", capturedOpts.DNSDomain)
		}

		if !capturedOpts.SSH {
			t.Error("Expected SSH to be enabled")
		}

		if len(capturedOpts.Mount) != 3 {
			t.Errorf("Expected 3 mounts, got %d", len(capturedOpts.Mount))
		}
	})

	t.Run("success with custom mounts", func(t *testing.T) {
		var capturedOpts *options.CreateContainer

		mockOps := &mockContainerOps{
			createFunc: func(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
				capturedOpts = opts
				return "custom-container-id", nil
			},
		}

		box := &Box{
			ID:        "test-box",
			ImageName: "test-image",
			Mounts: []MountSpec{
				{Source: "/host/path", Target: "/container/path", ReadOnly: true},
			},
			containerService: mockOps,
		}

		err := box.CreateContainer(ctx)
		if err != nil {
			t.Fatalf("CreateContainer() error = %v", err)
		}

		if len(capturedOpts.Mount) != 1 {
			t.Errorf("Expected 1 mount, got %d", len(capturedOpts.Mount))
		}

		expectedMount := "type=bind,source=/host/path,target=/container/path,readonly"
		if capturedOpts.Mount[0] != expectedMount {
			t.Errorf("Expected mount %s, got %s", expectedMount, capturedOpts.Mount[0])
		}
	})

	t.Run("create error", func(t *testing.T) {
		expectedErr := errors.New("create failed")
		mockOps := &mockContainerOps{
			createFunc: func(ctx context.Context, opts *options.CreateContainer, image string, args []string) (string, error) {
				return "", expectedErr
			},
		}

		box := &Box{
			ID:               "test-box",
			ImageName:        "test-image",
			containerService: mockOps,
		}

		err := box.CreateContainer(ctx)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-box") {
			t.Errorf("Error should contain sandbox ID 'test-box', got: %v", err)
		}
	})
}

func TestBox_StartContainer(t *testing.T) {
	ctx := context.Background()

	t.Run("success without hooks", func(t *testing.T) {
		mockOps := &mockContainerOps{
			startFunc: func(ctx context.Context, opts *options.StartContainer, containerID string) (string, error) {
				if containerID != "test-container" {
					t.Errorf("Expected containerID 'test-container', got %s", containerID)
				}
				return "started", nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		err := box.StartContainer(ctx)
		if err != nil {
			t.Fatalf("StartContainer() error = %v", err)
		}
	})

	t.Run("success with hooks", func(t *testing.T) {
		mockOps := &mockContainerOps{}

		hookCalled := 0
		hook := NewContainerStartupHook("test-hook", func(ctx context.Context, b *Box) error {
			hookCalled++
			if b.ID != "test-box" {
				t.Errorf("Hook received wrong box ID: %s", b.ID)
			}
			return nil
		})

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			ContainerHooks:   []ContainerStartupHook{hook},
			containerService: mockOps,
		}

		err := box.StartContainer(ctx)
		if err != nil {
			t.Fatalf("StartContainer() error = %v", err)
		}

		if hookCalled != 1 {
			t.Errorf("Expected hook to be called once, was called %d times", hookCalled)
		}
	})

	t.Run("multiple hooks success", func(t *testing.T) {
		mockOps := &mockContainerOps{}

		callOrder := []string{}
		hook1 := NewContainerStartupHook("hook1", func(ctx context.Context, b *Box) error {
			callOrder = append(callOrder, "hook1")
			return nil
		})
		hook2 := NewContainerStartupHook("hook2", func(ctx context.Context, b *Box) error {
			callOrder = append(callOrder, "hook2")
			return nil
		})

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			ContainerHooks:   []ContainerStartupHook{hook1, hook2},
			containerService: mockOps,
		}

		err := box.StartContainer(ctx)
		if err != nil {
			t.Fatalf("StartContainer() error = %v", err)
		}

		if len(callOrder) != 2 {
			t.Errorf("Expected 2 hooks to be called, got %d", len(callOrder))
		}
		if callOrder[0] != "hook1" || callOrder[1] != "hook2" {
			t.Errorf("Expected hooks called in order [hook1, hook2], got %v", callOrder)
		}
	})

	t.Run("start error", func(t *testing.T) {
		expectedErr := errors.New("start failed")
		mockOps := &mockContainerOps{
			startFunc: func(ctx context.Context, opts *options.StartContainer, containerID string) (string, error) {
				return "", expectedErr
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		err := box.StartContainer(ctx)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-box") {
			t.Errorf("Error should contain sandbox ID 'test-box', got: %v", err)
		}
	})

	t.Run("hook error", func(t *testing.T) {
		mockOps := &mockContainerOps{}

		expectedErr := errors.New("hook failed")
		hook := NewContainerStartupHook("failing-hook", func(ctx context.Context, b *Box) error {
			return expectedErr
		})

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			ContainerHooks:   []ContainerStartupHook{hook},
			containerService: mockOps,
		}

		err := box.StartContainer(ctx)
		if err == nil {
			t.Fatal("Expected error from hook, got nil")
		}
		if !strings.Contains(err.Error(), "failing-hook") {
			t.Errorf("Error should contain hook name 'failing-hook', got: %v", err)
		}
	})

	t.Run("multiple hook failures", func(t *testing.T) {
		mockOps := &mockContainerOps{}

		hook1 := NewContainerStartupHook("hook1", func(ctx context.Context, b *Box) error {
			return errors.New("error1")
		})
		hook2 := NewContainerStartupHook("hook2", func(ctx context.Context, b *Box) error {
			return errors.New("error2")
		})

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			ContainerHooks:   []ContainerStartupHook{hook1, hook2},
			containerService: mockOps,
		}

		err := box.StartContainer(ctx)
		if err == nil {
			t.Fatal("Expected error from hooks, got nil")
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "hook1") || !strings.Contains(errStr, "hook2") {
			t.Errorf("Error should contain both hook names, got: %v", err)
		}
		if !strings.Contains(errStr, "error1") || !strings.Contains(errStr, "error2") {
			t.Errorf("Error should contain both error messages, got: %v", err)
		}
	})

	t.Run("partial hook failure", func(t *testing.T) {
		mockOps := &mockContainerOps{}

		successCount := 0
		hook1 := NewContainerStartupHook("hook1", func(ctx context.Context, b *Box) error {
			successCount++
			return nil
		})
		hook2 := NewContainerStartupHook("hook2", func(ctx context.Context, b *Box) error {
			return errors.New("hook2 failed")
		})
		hook3 := NewContainerStartupHook("hook3", func(ctx context.Context, b *Box) error {
			successCount++
			return nil
		})

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			ContainerHooks:   []ContainerStartupHook{hook1, hook2, hook3},
			containerService: mockOps,
		}

		err := box.StartContainer(ctx)
		if err == nil {
			t.Fatal("Expected error from hook2, got nil")
		}

		if successCount != 2 {
			t.Errorf("Expected 2 successful hooks, got %d", successCount)
		}

		if !strings.Contains(err.Error(), "hook2") {
			t.Errorf("Error should contain 'hook2', got: %v", err)
		}
	})
}

func TestBox_Shell(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		var capturedCmd string
		var capturedStdin io.Reader
		var capturedStdout, capturedStderr io.Writer

		mockOps := &mockContainerOps{
			execStreamFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer) (func() error, error) {
				capturedCmd = cmd
				capturedStdin = stdin
				capturedStdout = stdout
				capturedStderr = stderr

				if containerID != "test-container" {
					t.Errorf("Expected containerID 'test-container', got %s", containerID)
				}
				if opts.Interactive != true {
					t.Error("Expected Interactive to be true")
				}
				if opts.TTY != true {
					t.Error("Expected TTY to be true")
				}
				if opts.WorkDir != "/app" {
					t.Errorf("Expected WorkDir '/app', got %s", opts.WorkDir)
				}

				return func() error {
					fmt.Fprint(stdout, "command output")
					return nil
				}, nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		stdin := strings.NewReader("input")
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		env := map[string]string{"KEY": "value"}

		err := box.Shell(ctx, env, "/bin/bash", stdin, stdout, stderr)
		if err != nil {
			t.Fatalf("Shell() error = %v", err)
		}

		if capturedCmd != "/bin/bash" {
			t.Errorf("Expected cmd '/bin/bash', got %s", capturedCmd)
		}
		if capturedStdin != stdin {
			t.Error("stdin not passed correctly")
		}
		if capturedStdout != stdout {
			t.Error("stdout not passed correctly")
		}
		if capturedStderr != stderr {
			t.Error("stderr not passed correctly")
		}

		if stdout.String() != "command output" {
			t.Errorf("Expected stdout 'command output', got %s", stdout.String())
		}
	})

	t.Run("execstream error", func(t *testing.T) {
		expectedErr := errors.New("exec stream failed")
		mockOps := &mockContainerOps{
			execStreamFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer) (func() error, error) {
				return nil, expectedErr
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		err := box.Shell(ctx, nil, "/bin/sh", nil, io.Discard, io.Discard)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-box") {
			t.Errorf("Error should contain sandbox ID 'test-box', got: %v", err)
		}
	})

	t.Run("wait error", func(t *testing.T) {
		expectedErr := errors.New("wait failed")
		mockOps := &mockContainerOps{
			execStreamFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer) (func() error, error) {
				return func() error {
					return expectedErr
				}, nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		err := box.Shell(ctx, nil, "/bin/sh", nil, io.Discard, io.Discard)
		if err == nil {
			t.Fatal("Expected error from wait, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Errorf("Expected error to wrap expectedErr, got: %v", err)
		}
	})
}

func TestBox_Exec(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		var capturedCmd string
		var capturedArgs []string

		mockOps := &mockContainerOps{
			execFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
				capturedCmd = cmd
				capturedArgs = args

				if containerID != "test-container" {
					t.Errorf("Expected containerID 'test-container', got %s", containerID)
				}
				if opts.Interactive != false {
					t.Error("Expected Interactive to be false")
				}
				if opts.TTY != true {
					t.Error("Expected TTY to be true")
				}
				if opts.WorkDir != "/app" {
					t.Errorf("Expected WorkDir '/app', got %s", opts.WorkDir)
				}

				return "exec result", nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		output, err := box.Exec(ctx, "ls", "-la", "/tmp")
		if err != nil {
			t.Fatalf("Exec() error = %v", err)
		}

		if output != "exec result" {
			t.Errorf("Expected output 'exec result', got %s", output)
		}

		if capturedCmd != "ls" {
			t.Errorf("Expected cmd 'ls', got %s", capturedCmd)
		}

		if len(capturedArgs) != 2 {
			t.Errorf("Expected 2 args, got %d", len(capturedArgs))
		}
		if capturedArgs[0] != "-la" || capturedArgs[1] != "/tmp" {
			t.Errorf("Expected args ['-la', '/tmp'], got %v", capturedArgs)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		expectedErr := errors.New("exec failed")
		mockOps := &mockContainerOps{
			execFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
				return "", expectedErr
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			containerService: mockOps,
		}

		_, err := box.Exec(ctx, "failing-command")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "test-box") {
			t.Errorf("Error should contain sandbox ID 'test-box', got: %v", err)
		}
	})

	t.Run("with envfile", func(t *testing.T) {
		var capturedOpts *options.ExecContainer

		mockOps := &mockContainerOps{
			execFunc: func(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
				capturedOpts = opts
				return "output", nil
			},
		}

		box := &Box{
			ID:               "test-box",
			ContainerID:      "test-container",
			EnvFile:          "/path/to/.env",
			containerService: mockOps,
		}

		_, err := box.Exec(ctx, "echo", "test")
		if err != nil {
			t.Fatalf("Exec() error = %v", err)
		}

		if capturedOpts.EnvFile != "/path/to/.env" {
			t.Errorf("Expected EnvFile '/path/to/.env', got %s", capturedOpts.EnvFile)
		}
	})
}
