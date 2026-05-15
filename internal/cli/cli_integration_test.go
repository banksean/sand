package cli_test

import (
	"context"
	"slices"
	"sync"
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

func TestLsCmd_RequestsStatsForRunningSandboxes(t *testing.T) {
	var statsCalls []string
	var statsCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			StatsFunc: func(_ context.Context, containerID ...string) ([]types.ContainerStats, error) {
				statsCallsMu.Lock()
				defer statsCallsMu.Unlock()
				statsCalls = append(statsCalls, containerID...)
				return []types.ContainerStats{
					{ID: "ctr-alpha", CPUUsageUsec: 1000},
					{ID: "ctr-beta", CPUUsageUsec: 2000},
				}, nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("alpha"))
		s.SaveSandbox(ctx, testBox("beta"))
	})
	if err := (&cli.LsCmd{Long: true}).Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	slices.Sort(statsCalls)
	if !slices.Equal(statsCalls, []string{"ctr-alpha", "ctr-beta"}) {
		t.Fatalf("Stats called with %v, want ctr-alpha and ctr-beta", statsCalls)
	}
}

func TestLsCmd_DefaultDoesNotRequestStats(t *testing.T) {
	var statsCalls int
	var statsCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			StatsFunc: func(_ context.Context, containerID ...string) ([]types.ContainerStats, error) {
				statsCallsMu.Lock()
				defer statsCallsMu.Unlock()
				statsCalls++
				return nil, nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("alpha"))
	})
	if err := (&cli.LsCmd{}).Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	statsCallsMu.Lock()
	defer statsCallsMu.Unlock()
	if statsCalls != 0 {
		t.Fatalf("Stats called %d times, want 0", statsCalls)
	}
}

// --- RmCmd ---

func TestRmCmd_Single(t *testing.T) {
	cctx := newCLIContext(t, daemontest.Deps{}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("target"))
		s.SaveSandbox(ctx, testBox("keep"))
	})

	cmd := &cli.RmCmd{
		MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxNames: []string{"target"}},
		Force:                 true,
	}
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

func TestRmCmd_Multiple(t *testing.T) {
	cctx := newCLIContext(t, daemontest.Deps{}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("target"))
		s.SaveSandbox(ctx, testBox("other"))
		s.SaveSandbox(ctx, testBox("keep"))
	})

	cmd := &cli.RmCmd{
		MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxNames: []string{"target", "other"}},
		Force:                 true,
	}
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

	cmd := &cli.RmCmd{
		MultiSandboxNameFlags: cli.MultiSandboxNameFlags{All: true},
		Force:                 true,
	}
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
	cmd := &cli.RmCmd{
		MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxNames: []string{"ghost"}},
		Force:                 true,
	}
	if err := cmd.Run(cctx); err == nil {
		t.Fatal("expected error removing nonexistent sandbox, got nil")
	}
}

// --- StopCmd ---

func TestStopCmd_Single(t *testing.T) {
	var stopCalls []string
	var stopCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			StopFunc: func(_ context.Context, _ *options.StopContainer, containerID string) (string, error) {
				stopCallsMu.Lock()
				defer stopCallsMu.Unlock()
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("mysandbox"))
	})

	cmd := &cli.StopCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxNames: []string{"mysandbox"}}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(stopCalls) != 1 || stopCalls[0] != "ctr-mysandbox" {
		t.Errorf("expected Stop('ctr-mysandbox'), got: %v", stopCalls)
	}
}

func TestStopCmd_Multiple(t *testing.T) {
	var stopCalls []string
	var stopCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			StopFunc: func(_ context.Context, _ *options.StopContainer, containerID string) (string, error) {
				stopCallsMu.Lock()
				defer stopCallsMu.Unlock()
				stopCalls = append(stopCalls, containerID)
				return "stopped", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("first"))
		s.SaveSandbox(ctx, testBox("second"))
		s.SaveSandbox(ctx, testBox("keep-running"))
	})

	cmd := &cli.StopCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxNames: []string{"first", "second"}}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	slices.Sort(stopCalls)
	if !slices.Equal(stopCalls, []string{"ctr-first", "ctr-second"}) {
		t.Errorf("expected Stop calls for first and second, got: %v", stopCalls)
	}
}

func TestStopCmd_All(t *testing.T) {
	var stopCalls []string
	var stopCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			StopFunc: func(_ context.Context, _ *options.StopContainer, containerID string) (string, error) {
				stopCallsMu.Lock()
				defer stopCallsMu.Unlock()
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
	var startCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			// Default Inspect returns Status="running"
			InspectFunc: func(_ context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "running"}}, nil
			},
			StartFunc: func(_ context.Context, _ *options.StartContainer, containerID string) (string, error) {
				startCallsMu.Lock()
				defer startCallsMu.Unlock()
				startCalls = append(startCalls, containerID)
				return "started", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("running-box"))
	})

	cmd := &cli.StartCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxNames: []string{"running-box"}}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(startCalls) != 0 {
		t.Errorf("expected Start not to be called for already-running sandbox, got calls: %v", startCalls)
	}
}

func TestStartCmd_StartsStopped(t *testing.T) {
	var startCalls []string
	var startCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(_ context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "stopped"}}, nil
			},
			StartFunc: func(_ context.Context, _ *options.StartContainer, containerID string) (string, error) {
				startCallsMu.Lock()
				defer startCallsMu.Unlock()
				startCalls = append(startCalls, containerID)
				return "started", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("stopped-box"))
	})

	cmd := &cli.StartCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxNames: []string{"stopped-box"}}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(startCalls) != 1 {
		t.Errorf("expected 1 Start call, got %d: %v", len(startCalls), startCalls)
	}
}

func TestStartCmd_Multiple(t *testing.T) {
	var startCalls []string
	var startCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(_ context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "stopped"}}, nil
			},
			StartFunc: func(_ context.Context, _ *options.StartContainer, containerID string) (string, error) {
				startCallsMu.Lock()
				defer startCallsMu.Unlock()
				startCalls = append(startCalls, containerID)
				return "started", nil
			},
		},
	}, func(ctx context.Context, s daemontest.SandboxStore) {
		s.SaveSandbox(ctx, testBox("first"))
		s.SaveSandbox(ctx, testBox("second"))
		s.SaveSandbox(ctx, testBox("already-running"))
	})

	cmd := &cli.StartCmd{MultiSandboxNameFlags: cli.MultiSandboxNameFlags{SandboxNames: []string{"first", "second"}}}
	if err := cmd.Run(cctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	slices.Sort(startCalls)
	if !slices.Equal(startCalls, []string{"ctr-first", "ctr-second"}) {
		t.Errorf("expected Start calls for first and second, got: %v", startCalls)
	}
}

func TestStartCmd_All(t *testing.T) {
	var startCalls []string
	var startCallsMu sync.Mutex
	cctx := newCLIContext(t, daemontest.Deps{
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(_ context.Context, containerID string) ([]types.Container, error) {
				return []types.Container{{Status: "stopped"}}, nil
			},
			StartFunc: func(_ context.Context, _ *options.StartContainer, containerID string) (string, error) {
				startCallsMu.Lock()
				defer startCallsMu.Unlock()
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
