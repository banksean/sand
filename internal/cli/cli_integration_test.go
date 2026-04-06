package cli_test

import (
	"context"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/cli"
	"github.com/banksean/sand/internal/daemon/daemontest"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
)

// testBox returns a minimal sandbox suitable for seeding the database.
func testBox(id string) *sandtypes.Box {
	return &sandtypes.Box{
		ID:             id,
		ContainerID:    "ctr-" + id,
		HostOriginDir:  "/home/user/project",
		SandboxWorkDir: "/tmp/" + id,
		ImageName:      "test-image:latest",
		AgentType:      "default",
	}
}

// newCLIContext starts a test daemon and returns a CLIContext wired to it.
func newCLIContext(t *testing.T, deps daemontest.Deps, configure func(context.Context, daemontest.SandboxStore)) *cli.CLIContext {
	t.Helper()
	client := daemontest.StartDaemon(t, deps, configure)
	return &cli.CLIContext{
		Context: context.Background(),
		Daemon:  client,
	}
}

// --- LsCmd ---

func TestLsCmd_Empty(t *testing.T) {
	cctx := newCLIContext(t, daemontest.Deps{}, nil)
	if err := (&cli.LsCmd{}).Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestLsCmd_WithSandboxes(t *testing.T) {
	cctx := newCLIContext(t, daemontest.Deps{}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("alpha"))
		s.SaveSandbox(ctx, testBox("beta"))
	})
	if err := (&cli.LsCmd{}).Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// --- RmCmd ---

func TestRmCmd_Single(t *testing.T) {
	cctx := newCLIContext(t, daemontest.Deps{}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("target"))
		s.SaveSandbox(ctx, testBox("keep"))
	})

	cmd := &cli.RmCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxName: "target"}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	boxes, err := cctx.Daemon.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes() error = %v", err)
	}
	if len(boxes) != 1 || boxes[0].ID != "keep" {
		t.Errorf("expected only 'keep' to remain, got %v", boxIDs(boxes))
	}
}

func TestRmCmd_All(t *testing.T) {
	cctx := newCLIContext(t, daemontest.Deps{}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("one"))
		s.SaveSandbox(ctx, testBox("two"))
		s.SaveSandbox(ctx, testBox("three"))
	})

	cmd := &cli.RmCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{All: true}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	boxes, err := cctx.Daemon.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes() error = %v", err)
	}
	if len(boxes) != 0 {
		t.Errorf("expected no sandboxes after rm -a, got %v", boxIDs(boxes))
	}
}

func TestRmCmd_NotFound(t *testing.T) {
	cctx := newCLIContext(t, daemontest.Deps{}, nil)
	cmd := &cli.RmCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxName: "ghost"}}
	if err := cmd.Run(cctx); err == nil {
		t.Fatal("expected error removing nonexistent sandbox, got nil")
	}
}

// --- StopCmd ---

func TestStopCmd_Single(t *testing.T) {
	var stopCalls []string
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			StopFunc: func(_ context.Context, _ *options.StopContainer, containerID string) (string, error) {
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("mysandbox"))
	})

	cmd := &cli.StopCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxName: "mysandbox"}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(stopCalls) != 1 || stopCalls[0] != "ctr-mysandbox" {
		t.Errorf("expected Stop('ctr-mysandbox'), got: %v", stopCalls)
	}
}

func TestStopCmd_All(t *testing.T) {
	var stopCalls []string
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			StopFunc: func(_ context.Context, _ *options.StopContainer, containerID string) (string, error) {
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("sb1"))
		s.SaveSandbox(ctx, testBox("sb2"))
	})

	cmd := &cli.StopCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{All: true}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(stopCalls) != 2 {
		t.Errorf("expected 2 Stop calls, got %d: %v", len(stopCalls), stopCalls)
	}
}

// --- StartCmd ---

func TestStartCmd_AlreadyRunning(t *testing.T) {
	var startCalls []string
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			// Default Inspect returns Status="running"
			InspectFunc: func(_ context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "running"}}, nil
			},
			StartFunc: func(_ context.Context, _ *options.StartContainer, containerID string) (string, error) {
				startCalls = append(startCalls, containerID)
				return "started", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("running-box"))
	})

	cmd := &cli.StartCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxName: "running-box"}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(startCalls) != 0 {
		t.Errorf("expected Start not to be called for already-running sandbox, got calls: %v", startCalls)
	}
}

func TestStartCmd_StartsStopped(t *testing.T) {
	var startCalls []string
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(_ context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "stopped"}}, nil
			},
			StartFunc: func(_ context.Context, _ *options.StartContainer, containerID string) (string, error) {
				startCalls = append(startCalls, containerID)
				return "started", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("stopped-box"))
	})

	cmd := &cli.StartCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxName: "stopped-box"}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(startCalls) != 1 {
		t.Errorf("expected 1 Start call, got %d: %v", len(startCalls), startCalls)
	}
}

func TestStartCmd_All(t *testing.T) {
	var startCalls []string
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(_ context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "stopped"}}, nil
			},
			StartFunc: func(_ context.Context, _ *options.StartContainer, containerID string) (string, error) {
				startCalls = append(startCalls, containerID)
				return "started", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("node1"))
		s.SaveSandbox(ctx, testBox("node2"))
	})

	cmd := &cli.StartCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{All: true}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(startCalls) != 2 {
		t.Errorf("expected 2 Start calls, got %d: %v", len(startCalls), startCalls)
	}
}

func boxIDs(boxes []sandtypes.Box) []string {
	ids := make([]string, len(boxes))
	for i, b := range boxes {
		ids[i] = b.ID
	}
	return ids
}
