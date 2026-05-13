package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestCheckoutSandboxBranch(t *testing.T) {
	type execCall struct {
		containerID string
		cmd         string
		args        []string
		opts        *options.ExecContainer
	}

	sbox := &sandtypes.Box{
		ID:          "sb-123",
		Name:        "sb-123",
		ContainerID: "ctr-123",
		EnvFile:     "/tmp/test.env",
		Username:    "alice",
		Uid:         "1001",
	}
	var calls []execCall
	containerSvc := &hostops.MockContainerOps{
		ExecFunc: func(_ context.Context, opts *options.ExecContainer, containerID, cmd string, _ []string, args ...string) (string, error) {
			calls = append(calls, execCall{
				containerID: containerID,
				cmd:         cmd,
				args:        append([]string(nil), args...),
				opts:        opts,
			})
			return "", nil
		},
	}
	if err := checkoutSandboxBranch(context.Background(), containerSvc, sbox, plainCommandEnv{}); err != nil {
		t.Fatalf("checkoutSandboxBranch() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(calls))
	}
	if calls[0].containerID != sbox.ContainerID {
		t.Fatalf("config call container ID = %q, want %q", calls[0].containerID, sbox.ContainerID)
	}
	if calls[0].cmd != "git" {
		t.Fatalf("config call cmd = %q, want git", calls[0].cmd)
	}
	if diff := reflect.DeepEqual(calls[0].args, []string{"config", "--global", "--add", "safe.directory", "/app"}); !diff {
		t.Fatalf("config call args = %v", calls[0].args)
	}

	tests := []struct {
		name       string
		projectEnv plainCommandEnv
	}{
		{name: "without project env"},
		{name: "with project env", projectEnv: plainCommandEnv{
			EnvFile: sbox.EnvFile,
			Env:     map[string]string{"PROJECT_NAME": "sand"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []execCall
			containerSvc := &hostops.MockContainerOps{
				ExecFunc: func(_ context.Context, opts *options.ExecContainer, containerID, cmd string, _ []string, args ...string) (string, error) {
					calls = append(calls, execCall{
						containerID: containerID,
						cmd:         cmd,
						args:        append([]string(nil), args...),
						opts:        opts,
					})
					return "", nil
				},
			}

			checkoutSandboxBranch(context.Background(), containerSvc, sbox, tt.projectEnv)

			if len(calls) != 2 {
				t.Fatalf("expected 2 exec calls, got %d", len(calls))
			}

			if calls[0].containerID != sbox.ContainerID {
				t.Fatalf("config call container ID = %q, want %q", calls[0].containerID, sbox.ContainerID)
			}
			if calls[0].cmd != "git" {
				t.Fatalf("config call cmd = %q, want git", calls[0].cmd)
			}
			if diff := reflect.DeepEqual(calls[0].args, []string{"config", "--global", "--add", "safe.directory", "/app"}); !diff {
				t.Fatalf("config call args = %v", calls[0].args)
			}

			if calls[1].cmd != "git" {
				t.Fatalf("checkout call cmd = %q, want git", calls[1].cmd)
			}
			if diff := reflect.DeepEqual(calls[1].args, []string{"checkout", "-b", sbox.Name}); !diff {
				t.Fatalf("checkout call args = %v", calls[1].args)
			}

			wantOpts := &options.ExecContainer{
				ProcessOptions: options.ProcessOptions{
					WorkDir: "/app",
					Env:     tt.projectEnv.Env,
					EnvFile: tt.projectEnv.EnvFile,
					User:    sbox.Username,
					UID:     sbox.Uid,
				},
			}
			if !reflect.DeepEqual(calls[0].opts, wantOpts) {
				t.Fatalf("config call opts = %+v, want %+v", calls[0].opts, wantOpts)
			}
			if !reflect.DeepEqual(calls[1].opts, wantOpts) {
				t.Fatalf("checkout call opts = %+v, want %+v", calls[1].opts, wantOpts)
			}
		})
	}
}

func TestCheckoutSandboxBranch_ReturnsCheckoutError(t *testing.T) {
	containerSvc := &hostops.MockContainerOps{
		ExecFunc: func(_ context.Context, _ *options.ExecContainer, _ string, _ string, _ []string, args ...string) (string, error) {
			if reflect.DeepEqual(args, []string{"checkout", "-b", "sb-123"}) {
				return "fatal: a branch named 'sb-123' already exists", errors.New("exit status 128")
			}
			return "", nil
		},
	}
	sbox := &sandtypes.Box{
		ID:          "sb-123",
		Name:        "sb-123",
		ContainerID: "ctr-123",
	}

	err := checkoutSandboxBranch(context.Background(), containerSvc, sbox, plainCommandEnv{})
	if err == nil {
		t.Fatal("checkoutSandboxBranch() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `creating branch "sb-123"`) {
		t.Fatalf("checkoutSandboxBranch() error = %v, want branch context", err)
	}
}

func TestValidateNewSandboxBranch(t *testing.T) {
	t.Run("available branch name", func(t *testing.T) {
		var gotDir, gotBranch string
		gitOps := &hostops.MockGitOps{
			LocalBranchExistsFunc: func(_ context.Context, dir, branch string) bool {
				gotDir = dir
				gotBranch = branch
				return false
			},
		}

		err := validateNewSandboxBranch(context.Background(), gitOps, "/repo", "sb-123")
		if err != nil {
			t.Fatalf("validateNewSandboxBranch() error = %v", err)
		}
		if gotDir != "/repo" || gotBranch != "sb-123" {
			t.Fatalf("validateNewSandboxBranch() called with dir=%q branch=%q", gotDir, gotBranch)
		}
	})

	t.Run("existing branch name", func(t *testing.T) {
		gitOps := &hostops.MockGitOps{
			LocalBranchExistsFunc: func(_ context.Context, dir, branch string) bool {
				return dir == "/repo" && branch == "sb-123"
			},
		}

		err := validateNewSandboxBranch(context.Background(), gitOps, "/repo", "sb-123")
		if err == nil {
			t.Fatal("validateNewSandboxBranch() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), `branch name "sb-123" is already taken`) {
			t.Fatalf("validateNewSandboxBranch() error = %v", err)
		}
	})
}

func TestEnsureImageForNewCmdStartsContainerSystemAndRetries(t *testing.T) {
	var attempts int
	var startCalled bool
	var stdout bytes.Buffer

	err := ensureImageForNewCmd(
		context.Background(),
		"test-image:latest",
		&stdout,
		strings.NewReader("\n"),
		func(ctx context.Context, image string, w io.Writer) error {
			attempts++
			if attempts == 1 {
				return runtimedeps.ContainerSystemNotRunningError(errors.New("system service is not running"))
			}
			return nil
		},
		func(ctx context.Context, opts *options.SystemStart) (string, error) {
			startCalled = true
			return "started", nil
		},
	)
	if err != nil {
		t.Fatalf("ensureImageForNewCmd() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("ensure attempts = %d, want 2", attempts)
	}
	if !startCalled {
		t.Fatal("container system start was not called")
	}
	if got := stdout.String(); !strings.Contains(got, "Start container system now [Y/n]? ") || !strings.Contains(got, "started\n") {
		t.Fatalf("stdout = %q, want prompt and start output", got)
	}
}

func TestEnsureImageForNewCmdDeclineReturnsOriginalError(t *testing.T) {
	wantErr := runtimedeps.ContainerSystemNotRunningError(errors.New("system service is not running"))

	err := ensureImageForNewCmd(
		context.Background(),
		"test-image:latest",
		io.Discard,
		strings.NewReader("n\n"),
		func(ctx context.Context, image string, w io.Writer) error {
			return wantErr
		},
		func(ctx context.Context, opts *options.SystemStart) (string, error) {
			t.Fatal("container system start should not be called")
			return "", nil
		},
	)
	if err == nil {
		t.Fatal("ensureImageForNewCmd() error = nil, want error")
	}
	if err.Error() != wantErr.Error() {
		t.Fatalf("ensureImageForNewCmd() error = %q, want %q", err, wantErr)
	}
}

func TestEnsureImageForNewCmdReturnsStartError(t *testing.T) {
	err := ensureImageForNewCmd(
		context.Background(),
		"test-image:latest",
		io.Discard,
		strings.NewReader("y\n"),
		func(ctx context.Context, image string, w io.Writer) error {
			return runtimedeps.ContainerSystemNotRunningError(errors.New("system service is not running"))
		},
		func(ctx context.Context, opts *options.SystemStart) (string, error) {
			return "", fmt.Errorf("start failed")
		},
	)
	if err == nil {
		t.Fatal("ensureImageForNewCmd() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "starting container system") {
		t.Fatalf("ensureImageForNewCmd() error = %q, want start context", err)
	}
}
