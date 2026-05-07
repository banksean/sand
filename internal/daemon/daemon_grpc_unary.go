package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/sandboxlog"
)

func (s *daemonGRPCServer) Shutdown(ctx context.Context, _ *daemonpb.ShutdownRequest) (*daemonpb.StatusResponse, error) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.daemon.Shutdown(ctx)
	}()
	return okStatus(), nil
}

func (s *daemonGRPCServer) LogSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.LogSandboxResponse, error) {
	id := req.GetId()
	ctx = sandboxlog.WithSandboxID(ctx, id)
	var buf bytes.Buffer
	if err := s.daemon.LogSandbox(ctx, id, &buf); err != nil {
		return nil, err
	}
	return &daemonpb.LogSandboxResponse{Data: buf.Bytes()}, nil
}

func (s *daemonGRPCServer) ListSandboxes(ctx context.Context, _ *daemonpb.ListSandboxesRequest) (*daemonpb.ListSandboxesResponse, error) {
	boxes, err := s.daemon.ListSandboxes(ctx)
	if err != nil {
		return nil, err
	}
	boxesJSON, err := json.Marshal(boxes)
	if err != nil {
		return nil, fmt.Errorf("marshal sandboxes: %w", err)
	}
	return &daemonpb.ListSandboxesResponse{BoxesJson: boxesJSON}, nil
}

func (s *daemonGRPCServer) GetSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.GetSandboxResponse, error) {
	id := req.GetId()
	ctx = sandboxlog.WithSandboxID(ctx, id)
	sbox, err := s.daemon.GetSandbox(ctx, id)
	if err != nil {
		return nil, err
	}
	if sbox == nil {
		return nil, fmt.Errorf("id not found: %q", id)
	}
	boxJSON, err := json.Marshal(sbox)
	if err != nil {
		return nil, fmt.Errorf("marshal sandbox: %w", err)
	}
	return &daemonpb.GetSandboxResponse{BoxJson: boxJSON}, nil
}

func (s *daemonGRPCServer) RemoveSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	id := req.GetId()
	ctx = sandboxlog.WithSandboxID(ctx, id)
	if err := s.daemon.RemoveSandbox(ctx, id); err != nil {
		return nil, err
	}
	return okStatus(), nil
}

func (s *daemonGRPCServer) StopSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	id := req.GetId()
	ctx = sandboxlog.WithSandboxID(ctx, id)
	if err := s.daemon.StopSandbox(ctx, id); err != nil {
		return nil, err
	}
	return okStatus(), nil
}

func (s *daemonGRPCServer) StartSandbox(ctx context.Context, req *daemonpb.StartSandboxRequest) (*daemonpb.StatusResponse, error) {
	if err := s.daemon.StartSandbox(ctx, StartSandboxOpts{
		Name:     req.GetId(),
		SSHAgent: req.GetSshAgent(),
	}); err != nil {
		return nil, err
	}
	return okStatus(), nil
}

func (s *daemonGRPCServer) SyncHostGitMirror(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.SyncHostGitMirrorResponse, error) {
	mirrorPath, err := s.daemon.SyncHostGitMirror(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &daemonpb.SyncHostGitMirrorResponse{MirrorPath: mirrorPath}, nil
}

func (s *daemonGRPCServer) ResolveAgentLaunchEnv(ctx context.Context, req *daemonpb.ResolveAgentLaunchEnvRequest) (*daemonpb.ResolveAgentLaunchEnvResponse, error) {
	resolved, err := s.daemon.resolveCreateSandboxCapabilities(CreateSandboxOpts{
		Agent:   req.GetAgent(),
		EnvFile: req.GetEnvFile(),
	})
	if err != nil {
		return nil, err
	}
	return &daemonpb.ResolveAgentLaunchEnvResponse{Env: resolved.AuthEnv}, nil
}

func (s *daemonGRPCServer) ExportImage(ctx context.Context, req *daemonpb.ExportImageRequest) (*daemonpb.StatusResponse, error) {
	sbox, err := s.daemon.GetSandbox(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if sbox == nil {
		return nil, fmt.Errorf("sandbox not found: %s", req.GetId())
	}
	if _, err := s.daemon.boxer.ContainerService.Export(ctx, &options.ExportContainer{Output: req.GetDestinationPath()}, sbox.ContainerID); err != nil {
		return nil, err
	}
	return okStatus(), nil
}

func (s *daemonGRPCServer) Stats(ctx context.Context, req *daemonpb.StatsRequest) (*daemonpb.StatsResponse, error) {
	ids := make([]string, 0, len(req.GetIds()))
	for _, name := range req.GetIds() {
		sbox, err := s.daemon.GetSandbox(ctx, name)
		if err != nil {
			return nil, err
		}
		if sbox == nil {
			return nil, fmt.Errorf("sandbox not found: %s", name)
		}
		ids = append(ids, sbox.ContainerID)
	}
	stats, err := s.daemon.boxer.GetContainerStats(ctx, ids...)
	if err != nil {
		return nil, err
	}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return nil, fmt.Errorf("marshal stats: %w", err)
	}
	return &daemonpb.StatsResponse{StatsJson: statsJSON}, nil
}

func (s *daemonGRPCServer) VSC(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	id := req.GetId()
	ctx = sandboxlog.WithSandboxID(ctx, id)
	sbox, err := s.daemon.GetSandbox(ctx, id)
	if err != nil {
		return nil, err
	}
	if sbox == nil || sbox.Container == nil || sbox.Container.Status != "running" {
		return nil, fmt.Errorf("cannot connect to sandbox %q becacuse it is not currently running", id)
	}

	hostname := types.GetContainerHostname(sbox.Container)
	vscCmd := exec.Command("code", "--remote", fmt.Sprintf("ssh-remote+%s", hostname), "/app", "-n")
	slog.InfoContext(ctx, "gRPC VSC: running code", "cmd", strings.Join(vscCmd.Args, " "))
	out, err := vscCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to start vsc for %q: %w: %s", id, err, strings.TrimSpace(string(out)))
	}
	return okStatus(), nil
}

func okStatus() *daemonpb.StatusResponse {
	return &daemonpb.StatusResponse{Status: "ok"}
}
