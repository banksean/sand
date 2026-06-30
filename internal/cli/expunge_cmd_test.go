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
	cctx := newTestCLIContext(t, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, newDeletedTestBox("deleted"))
	})

	var stdout bytes.Buffer
	restoreExpungeCmdIO(t, bytes.NewBufferString("n\n"), &stdout)

	cmd := &ExpungeCmd{SandboxIDs: []string{"deleted"}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	deleted, err := cctx.Daemon.ListDeletedSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListDeletedSandboxes() error = %v", err)
	}
	if len(deleted) != 1 || deleted[0].ID != "deleted" {
		t.Fatalf("expected deleted sandbox to remain, got %v", testBoxIDs(deleted))
	}
	if got := stdout.String(); got != "expunge deleted [y/N]? " {
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
