package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/sandboxlog"
	"github.com/banksean/sand/internal/sandtypes"
)

type grpcCreateSandboxProgressWriter struct {
	stream daemonpb.DaemonService_CreateSandboxServer
	mu     sync.Mutex
}

func (w *grpcCreateSandboxProgressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.stream.Send(&daemonpb.CreateSandboxResponse{
		Event: &daemonpb.CreateSandboxResponse_Progress{Progress: string(p)},
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

type grpcEnsureImageProgressWriter struct {
	stream daemonpb.DaemonService_EnsureImageServer
	mu     sync.Mutex
}

func (w *grpcEnsureImageProgressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.stream.Send(&daemonpb.EnsureImageResponse{
		Event: &daemonpb.EnsureImageResponse_Progress{Progress: append([]byte(nil), p...)},
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *daemonGRPCServer) CreateSandbox(req *daemonpb.CreateSandboxRequest, stream daemonpb.DaemonService_CreateSandboxServer) error {
	opts := createSandboxOptsFromProto(req)
	ctx := sandboxlog.WithSandboxID(stream.Context(), opts.ID)
	writer := &grpcCreateSandboxProgressWriter{stream: stream}

	sbox, err := s.daemon.createSandbox(ctx, opts, writer)
	if err != nil {
		slog.ErrorContext(ctx, "Daemon.gRPC CreateSandbox createSandbox", "error", err)
		return stream.Send(&daemonpb.CreateSandboxResponse{
			Event: &daemonpb.CreateSandboxResponse_Error{Error: err.Error()},
		})
	}

	boxJSON, err := json.Marshal(sbox)
	if err != nil {
		err = fmt.Errorf("marshal created sandbox: %w", err)
		return stream.Send(&daemonpb.CreateSandboxResponse{
			Event: &daemonpb.CreateSandboxResponse_Error{Error: err.Error()},
		})
	}
	return stream.Send(&daemonpb.CreateSandboxResponse{
		Event: &daemonpb.CreateSandboxResponse_BoxJson{BoxJson: boxJSON},
	})
}

func (s *daemonGRPCServer) EnsureImage(req *daemonpb.EnsureImageRequest, stream daemonpb.DaemonService_EnsureImageServer) error {
	ctx := stream.Context()
	writer := &grpcEnsureImageProgressWriter{stream: stream}

	if err := s.daemon.boxer.EnsureImage(ctx, req.GetImageName(), writer); err != nil {
		return stream.Send(&daemonpb.EnsureImageResponse{
			Event: &daemonpb.EnsureImageResponse_Error{Error: err.Error()},
		})
	}
	return stream.Send(&daemonpb.EnsureImageResponse{
		Event: &daemonpb.EnsureImageResponse_Ok{Ok: true},
	})
}

func createSandboxOptsToProto(opts CreateSandboxOpts) *daemonpb.CreateSandboxRequest {
	return &daemonpb.CreateSandboxRequest{
		Id:             opts.ID,
		CloneFromDir:   opts.CloneFromDir,
		ImageName:      opts.ImageName,
		EnvFile:        opts.EnvFile,
		Agent:          opts.Agent,
		SshAgent:       opts.SSHAgent,
		Username:       opts.Username,
		Uid:            opts.Uid,
		AllowedDomains: append([]string(nil), opts.AllowedDomains...),
		Volumes:        append([]string(nil), opts.Volumes...),
		SharedCaches: &daemonpb.SharedCacheConfig{
			Mise: opts.SharedCaches.Mise,
			Apk:  opts.SharedCaches.APK,
		},
		Cpus:   int32(opts.CPUs),
		Memory: int32(opts.Memory),
	}
}

func createSandboxOptsFromProto(req *daemonpb.CreateSandboxRequest) CreateSandboxOpts {
	opts := CreateSandboxOpts{
		ID:             req.GetId(),
		CloneFromDir:   req.GetCloneFromDir(),
		ImageName:      req.GetImageName(),
		EnvFile:        req.GetEnvFile(),
		Agent:          req.GetAgent(),
		SSHAgent:       req.GetSshAgent(),
		Username:       req.GetUsername(),
		Uid:            req.GetUid(),
		AllowedDomains: append([]string(nil), req.GetAllowedDomains()...),
		Volumes:        append([]string(nil), req.GetVolumes()...),
		CPUs:           int(req.GetCpus()),
		Memory:         int(req.GetMemory()),
	}
	if sharedCaches := req.GetSharedCaches(); sharedCaches != nil {
		opts.SharedCaches = sandtypes.SharedCacheConfig{
			Mise: sharedCaches.GetMise(),
			APK:  sharedCaches.GetApk(),
		}
	}
	return opts
}
