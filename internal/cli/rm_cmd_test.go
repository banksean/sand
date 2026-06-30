package cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/banksean/sand/internal/daemon/daemontest"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestRmCmd_DeclinedConfirmationSkipsRemoval(t *testing.T) {
	cctx := newTestCLIContext(t, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, newTestBox("target"))
	})

	var stdout bytes.Buffer
	restoreRmCmdIO(t, bytes.NewBufferString("n\n"), &stdout)

	cmd := &RmCmd{MultiSandboxNameFlags: MultiSandboxNameFlags{SandboxNames: []string{"target"}}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	boxes, err := cctx.Daemon.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes() error = %v", err)
	}
	if len(boxes) != 1 || boxes[0].ID != "target" {
		t.Fatalf("expected target to remain, got %v", testBoxIDs(boxes))
	}
	if got := stdout.String(); got != "remove target [y/N]? " {
		t.Fatalf("unexpected prompt output: %q", got)
	}
}

func TestRmCmd_ConfirmedAllOnlyRemovesApprovedSandboxes(t *testing.T) {
	cctx := newTestCLIContext(t, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, newTestBox("one"))
		s.SaveSandbox(ctx, newTestBox("two"))
	})

	restoreRmCmdIO(t, bytes.NewBufferString("y\nn\n"), &bytes.Buffer{})

	cmd := &RmCmd{MultiSandboxNameFlags: MultiSandboxNameFlags{All: true}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	boxes, err := cctx.Daemon.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes() error = %v", err)
	}
	if len(boxes) != 1 {
		t.Fatalf("expected one sandbox to remain, got %v", testBoxIDs(boxes))
	}
	if boxes[0].ID != "one" && boxes[0].ID != "two" {
		t.Fatalf("expected one of the original sandboxes to remain, got %v", testBoxIDs(boxes))
	}
}

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

func newTestCLIContext(t *testing.T, configure func(context.Context, daemontest.SandboxStore)) *CLIContext {
	t.Helper()
	client := daemontest.StartDaemon(t, daemontest.Deps{}, configure)
	return &CLIContext{
		Context: context.Background(),
		Daemon:  client,
	}
}

func newTestBox(id string) *sandtypes.Box {
	return &sandtypes.Box{
		ID:             id,
		ContainerID:    "ctr-" + id,
		HostOriginDir:  "/home/user/project",
		SandboxWorkDir: "/tmp/" + id,
		ImageName:      "test-image:latest",
		AgentType:      "default",
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

func restoreRmCmdIO(t *testing.T, stdin *bytes.Buffer, stdout *bytes.Buffer) {
	t.Helper()

	prevIn := rmCmdStdin
	prevOut := rmCmdStdout
	rmCmdStdin = stdin
	rmCmdStdout = stdout

	t.Cleanup(func() {
		rmCmdStdin = prevIn
		rmCmdStdout = prevOut
	})
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

func testBoxIDs(boxes []sandtypes.Box) []string {
	ids := make([]string, len(boxes))
	for i, b := range boxes {
		ids[i] = b.ID
	}
	return ids
}
