package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandboxlog"
	"github.com/banksean/sand/internal/sandtypes"
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
	return &daemonpb.ListSandboxesResponse{Boxes: sandboxesToProto(boxes)}, nil
}

func (s *daemonGRPCServer) ListDeletedSandboxes(ctx context.Context, _ *daemonpb.ListSandboxesRequest) (*daemonpb.ListSandboxesResponse, error) {
	boxes, err := s.daemon.ListDeletedSandboxes(ctx)
	if err != nil {
		return nil, err
	}
	return &daemonpb.ListSandboxesResponse{Boxes: sandboxesToProto(boxes)}, nil
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
	return &daemonpb.GetSandboxResponse{Box: sandboxToProto(sbox)}, nil
}

func (s *daemonGRPCServer) RemoveSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	id := req.GetId()
	ctx = sandboxlog.WithSandboxID(ctx, id)
	if err := s.daemon.RemoveSandbox(ctx, id); err != nil {
		return nil, err
	}
	return okStatus(), nil
}

func (s *daemonGRPCServer) ExpungeSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	id := req.GetId()
	ctx = sandboxlog.WithSandboxID(ctx, id)
	if err := s.daemon.ExpungeSandbox(ctx, id); err != nil {
		return nil, err
	}
	return okStatus(), nil
}

func (s *daemonGRPCServer) RecoverSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.RecoverSandboxResponse, error) {
	sbox, err := s.daemon.RecoverSandbox(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &daemonpb.RecoverSandboxResponse{Box: sandboxToProto(sbox)}, nil
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

func (s *daemonGRPCServer) RenameSandbox(ctx context.Context, req *daemonpb.RenameSandboxRequest) (*daemonpb.RenameSandboxResponse, error) {
	sbox, err := s.daemon.RenameSandbox(ctx, req.GetOldName(), req.GetNewName())
	if err != nil {
		return nil, err
	}
	return &daemonpb.RenameSandboxResponse{Box: sandboxToProto(sbox)}, nil
}

func (s *daemonGRPCServer) ResolveAgentLaunchEnv(ctx context.Context, req *daemonpb.ResolveAgentLaunchEnvRequest) (*daemonpb.ResolveAgentLaunchEnvResponse, error) {
	resolved, err := s.daemon.resolveCreateSandboxRequirements(CreateSandboxOpts{
		Agent:                req.GetAgent(),
		EnvFile:              req.GetEnvFile(),
		ProfileName:          req.GetProfileName(),
		ProfileEnv:           envPolicyFromProto(req.GetProfileEnv()),
		ProfileEnvConfigured: req.GetProfileEnvConfigured(),
	})
	if err != nil {
		return nil, err
	}
	return &daemonpb.ResolveAgentLaunchEnvResponse{Env: resolved.AuthEnv}, nil
}

func envPolicyFromProto(policy *daemonpb.EnvPolicy) sandtypes.EnvPolicy {
	if policy == nil {
		return sandtypes.EnvPolicy{}
	}
	out := sandtypes.EnvPolicy{
		Files: make([]sandtypes.EnvFileRef, 0, len(policy.GetFiles())),
		Vars:  make([]sandtypes.EnvVarRule, 0, len(policy.GetVars())),
	}
	for _, file := range policy.GetFiles() {
		out.Files = append(out.Files, sandtypes.EnvFileRef{
			Path:  file.GetPath(),
			Scope: sandtypes.EnvScope(file.GetScope()),
		})
	}
	for _, variable := range policy.GetVars() {
		out.Vars = append(out.Vars, sandtypes.EnvVarRule{
			Name:  variable.GetName(),
			Scope: sandtypes.EnvScope(variable.GetScope()),
		})
	}
	return out
}

func (s *daemonGRPCServer) ExportImage(ctx context.Context, req *daemonpb.ExportImageRequest) (*daemonpb.StatusResponse, error) {
	sbox, err := s.daemon.GetSandbox(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if sbox == nil {
		return nil, fmt.Errorf("sandbox not found: %s", req.GetId())
	}
	if _, err := s.daemon.boxer.ContainerService.Export(ctx, &hostops.ExportContainer{Output: req.GetDestinationPath()}, sbox.ContainerID); err != nil {
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
	return &daemonpb.StatsResponse{Stats: containerStatsToProto(stats)}, nil
}

func (s *daemonGRPCServer) VSC(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	id := req.GetId()
	ctx = sandboxlog.WithSandboxID(ctx, id)
	sbox, err := s.daemon.GetSandbox(ctx, id)
	if err != nil {
		return nil, err
	}
	if sbox == nil || sbox.Container == nil || sbox.Container.Status.State != "running" {
		return nil, fmt.Errorf("cannot connect to sandbox %q becacuse it is not currently running", id)
	}

	hostname := sandtypes.GetContainerHostname(sbox.Container)
	vscCmd := exec.Command("code", "--remote", fmt.Sprintf("ssh-remote+%s", hostname), "/app", "-n")
	slog.InfoContext(ctx, "gRPC VSC: running code", "cmd", strings.Join(vscCmd.Args, " "))
	out, err := vscCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to start vsc for %q: %w: %s", id, err, strings.TrimSpace(string(out)))
	}
	return okStatus(), nil
}

func (s *daemonGRPCServer) HTTPProxyCache(ctx context.Context, req *daemonpb.HTTPProxyCacheRequest) (*daemonpb.StatusResponse, error) {
	if err := s.daemon.HTTPProxyCache(ctx, req.GetAction(), nil); err != nil {
		return nil, err
	}
	return okStatus(), nil
}

func (s *daemonGRPCServer) HTTPProxyCacheStatus(ctx context.Context, _ *daemonpb.HTTPProxyCacheStatusRequest) (*daemonpb.HTTPProxyCacheStatusResponse, error) {
	status, err := s.daemon.HTTPProxyCacheStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &daemonpb.HTTPProxyCacheStatusResponse{
		Name:     status.Name,
		Image:    status.Image,
		State:    status.State,
		Url:      status.URL,
		CacheDir: status.CacheDir,
		Running:  status.Running,
	}, nil
}

func okStatus() *daemonpb.StatusResponse {
	return &daemonpb.StatusResponse{Status: "ok"}
}
