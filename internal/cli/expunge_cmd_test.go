package cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/banksean/sand/internal/daemon/daemontest"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestExpungeCmd_DefaultsToAllDeletedSandboxes(t *testing.T) {
	cctx := newTestCLIContext(t, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, newDeletedTestBox("deleted-one"))
		s.SaveSandbox(ctx, newDeletedTestBox("deleted-two"))
		s.SaveSandbox(ctx, newTestBox("active"))
	})

	var stdout bytes.Buffer
	restoreExpungeCmdIO(t, bytes.NewBufferString("y\ny\n"), &stdout)

	cmd := &ExpungeCmd{}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	deleted, err := cctx.Daemon.ListDeletedSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListDeletedSandboxes() error = %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("expected no deleted sandboxes, got %v", testBoxIDs(deleted))
	}
	active, err := cctx.Daemon.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != "active" {
		t.Fatalf("expected active sandbox to remain, got %v", testBoxIDs(active))
	}
}

func TestExpungeCmd_DeclinedConfirmationSkipsExpunge(t *testing.T) {
	const sandboxID = "3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b"
	cctx := newTestCLIContext(t, func(ctx context.Context, s daemontest.SandboxStore) {
		box := newDeletedTestBox(sandboxID)
		box.Name = "deleted-name"
		box.ImageName = "ghcr.io/banksean/sand/base:latest"
		box.OriginalGitDetails = &sandtypes.GitDetails{
			Branch:  "main",
			Commit:  "abcdef1234567890",
			IsDirty: false,
		}
		s.SaveSandbox(ctx, box)
	})

	var stdout bytes.Buffer
	restoreExpungeCmdIO(t, bytes.NewBufferString("n\n"), &stdout)

	cmd := &ExpungeCmd{SandboxIDs: []string{sandboxID}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	deleted, err := cctx.Daemon.ListDeletedSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListDeletedSandboxes() error = %v", err)
	}
	if len(deleted) != 1 || deleted[0].ID != sandboxID {
		t.Fatalf("expected deleted sandbox to remain, got %v", testBoxIDs(deleted))
	}
	wantPrompt := "expunge deleted-name\t0d7e41f1df1b\t/home/user/project\t abcdef12 main\tbase:latest [y/N]? "
	if got := stdout.String(); got != wantPrompt {
		t.Fatalf("unexpected prompt output: %q", got)
	}
}

func TestExpungeCmd_ActiveSandboxIDErrors(t *testing.T) {
	cctx := newTestCLIContext(t, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, newTestBox("active"))
	})

	restoreExpungeCmdIO(t, bytes.NewBufferString(""), &bytes.Buffer{})

	cmd := &ExpungeCmd{SandboxIDs: []string{"active"}, Force: true}
	if err := cmd.Run(cctx); err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}

func newDeletedTestBox(id string) *sandtypes.Box {
	box := newTestBox(id)
	box.State = "deleted"
	box.ContainerID = ""
	box.DeletedAt = time.Now()
	box.TrashWorkDir = "/tmp/trash/" + id
	return box
}

func restoreExpungeCmdIO(t *testing.T, stdin *bytes.Buffer, stdout *bytes.Buffer) {
	t.Helper()

	prevIn := expungeCmdStdin
	prevOut := expungeCmdStdout
	expungeCmdStdin = stdin
	expungeCmdStdout = stdout

	t.Cleanup(func() {
		expungeCmdStdin = prevIn
		expungeCmdStdout = prevOut
	})
}
