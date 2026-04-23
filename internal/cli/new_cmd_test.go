package cli

import (
	"context"
	"reflect"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestCheckoutSandboxBranch(t *testing.T) {
	type execCall struct {
		containerID string
		cmd         string
		args        []string
		opts        *options.ExecContainer
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
	sbox := &sandtypes.Box{
		ID:          "sb-123",
		ContainerID: "ctr-123",
		EnvFile:     "/tmp/test.env",
		Username:    "alice",
		Uid:         "1001",
	}

	checkoutSandboxBranch(context.Background(), containerSvc, sbox)

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
	if diff := reflect.DeepEqual(calls[1].args, []string{"checkout", "-b", sbox.ID}); !diff {
		t.Fatalf("checkout call args = %v", calls[1].args)
	}

	wantOpts := &options.ExecContainer{
		ProcessOptions: options.ProcessOptions{
			WorkDir: "/app",
			EnvFile: sbox.EnvFile,
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
}
