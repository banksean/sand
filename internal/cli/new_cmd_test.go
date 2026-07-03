package cli

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestCheckoutSandboxBranch(t *testing.T) {
	sbox := &sandtypes.Box{
		ID:       "sb-123",
		Name:     "sb-123",
		EnvFile:  "/tmp/test.env",
		Username: "alice",
		Uid:      "1001",
		Container: &types.Container{
			Configuration: types.ContainerConfig{ID: "sb-123.local"},
		},
	}
	var calls [][]string
	restore := stubSSH(t, &calls, []string{"", ""}, []int{0, 0})
	defer restore()

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
			calls = nil

			if err := checkoutSandboxBranch(context.Background(), sbox, tt.projectEnv); err != nil {
				t.Fatalf("checkoutSandboxBranch() error = %v", err)
			}

			if len(calls) != 2 {
				t.Fatalf("expected 2 ssh calls, got %d", len(calls))
			}
			wantFirst := []string{
				"sb-123.local",
				"cd '/app' && env 'HOSTNAME=sb-123.local' 'git' 'config' '--global' '--add' 'safe.directory' '/app'",
			}
			if tt.projectEnv.Env["PROJECT_NAME"] != "" {
				wantFirst[1] = "cd '/app' && env 'HOSTNAME=sb-123.local' 'PROJECT_NAME=sand' 'git' 'config' '--global' '--add' 'safe.directory' '/app'"
			}
			if !reflect.DeepEqual(calls[0], wantFirst) {
				t.Fatalf("config ssh call = %#v, want %#v", calls[0], wantFirst)
			}
			wantSecond := []string{
				"sb-123.local",
				"cd '/app' && env 'HOSTNAME=sb-123.local' 'git' 'checkout' '-b' 'sb-123'",
			}
			if tt.projectEnv.Env["PROJECT_NAME"] != "" {
				wantSecond[1] = "cd '/app' && env 'HOSTNAME=sb-123.local' 'PROJECT_NAME=sand' 'git' 'checkout' '-b' 'sb-123'"
			}
			if !reflect.DeepEqual(calls[1], wantSecond) {
				t.Fatalf("checkout ssh call = %#v, want %#v", calls[1], wantSecond)
			}
		})
	}
}

func TestCheckoutSandboxBranch_ReturnsCheckoutError(t *testing.T) {
	var calls [][]string
	restore := stubSSH(t, &calls, []string{"", "fatal: a branch named 'sb-123' already exists"}, []int{0, 128})
	defer restore()
	sbox := &sandtypes.Box{
		ID:   "sb-123",
		Name: "sb-123",
		Container: &types.Container{
			Configuration: types.ContainerConfig{ID: "sb-123.local"},
		},
	}

	err := checkoutSandboxBranch(context.Background(), sbox, plainCommandEnv{})
	if err == nil {
		t.Fatal("checkoutSandboxBranch() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `creating branch "sb-123"`) {
		t.Fatalf("checkoutSandboxBranch() error = %v, want branch context", err)
	}
}

func stubSSH(t *testing.T, calls *[][]string, outputs []string, exitCodes []int) func() {
	t.Helper()
	oldSSHCommand := sshCommand
	oldCheck := checkSSHReachability
	call := 0
	sshCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		*calls = append(*calls, append([]string(nil), args...))
		output := ""
		if call < len(outputs) {
			output = outputs[call]
		}
		exitCode := 0
		if call < len(exitCodes) {
			exitCode = exitCodes[call]
		}
		call++
		return exec.CommandContext(ctx, "sh", "-c", "printf %s "+shellQuote(output)+"; exit "+shellQuote(fmt.Sprint(exitCode)))
	}
	checkSSHReachability = func(context.Context, string) (func() error, error) {
		return nil, nil
	}
	return func() {
		sshCommand = oldSSHCommand
		checkSSHReachability = oldCheck
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
