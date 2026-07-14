package daemon

import (
	"log/slog"
	"sync"

	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/imageprogress"
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
	err    error
}

func (w *grpcEnsureImageProgressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if err := w.stream.Send(&daemonpb.EnsureImageResponse{
		Event: &daemonpb.EnsureImageResponse_Progress{Progress: append([]byte(nil), p...)},
	}); err != nil {
		w.err = err
		return 0, err
	}
	return len(p), nil
}

func (w *grpcEnsureImageProgressWriter) Update(update imageprogress.Update) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return
	}
	if err := w.stream.Send(&daemonpb.EnsureImageResponse{
		Event: &daemonpb.EnsureImageResponse_PullProgress{
			PullProgress: imageProgressUpdateToProto(update),
		},
	}); err != nil {
		w.err = err
	}
}

func (w *grpcEnsureImageProgressWriter) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func imageProgressUpdateToProto(update imageprogress.Update) *daemonpb.ImagePullProgressUpdate {
	return &daemonpb.ImagePullProgressUpdate{
		Description:    update.Description,
		SubDescription: update.SubDescription,
		ItemsName:      update.ItemsName,
		AddTasks:       update.AddTasks,
		SetTasks:       update.SetTasks,
		AddTotalTasks:  update.AddTotalTasks,
		SetTotalTasks:  update.SetTotalTasks,
		AddItems:       update.AddItems,
		SetItems:       update.SetItems,
		AddTotalItems:  update.AddTotalItems,
		SetTotalItems:  update.SetTotalItems,
		AddSize:        update.AddSize,
		SetSize:        update.SetSize,
		AddTotalSize:   update.AddTotalSize,
		SetTotalSize:   update.SetTotalSize,
	}
}

func (s *daemonGRPCServer) CreateSandbox(req *daemonpb.CreateSandboxRequest, stream daemonpb.DaemonService_CreateSandboxServer) error {
	opts := createSandboxOptsFromProto(req)
	ctx := stream.Context()
	writer := &grpcCreateSandboxProgressWriter{stream: stream}

	sbox, err := s.daemon.createSandbox(ctx, opts, writer)
	if err != nil {
		slog.ErrorContext(ctx, "Daemon.gRPC CreateSandbox createSandbox", "error", err)
		return stream.Send(&daemonpb.CreateSandboxResponse{
			Event: &daemonpb.CreateSandboxResponse_Error{Error: err.Error()},
		})
	}

	return stream.Send(&daemonpb.CreateSandboxResponse{
		Event: &daemonpb.CreateSandboxResponse_Box{Box: sandboxToProto(sbox)},
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
	if err := writer.Err(); err != nil {
		return err
	}
	return stream.Send(&daemonpb.EnsureImageResponse{
		Event: &daemonpb.EnsureImageResponse_Ok{Ok: true},
	})
}

func createSandboxOptsToProto(opts CreateSandboxOpts) *daemonpb.CreateSandboxRequest {
	name := opts.Name
	if name == "" {
		name = opts.ID
	}
	return &daemonpb.CreateSandboxRequest{
		Id:             name,
		CloneFromDir:   opts.CloneFromDir,
		ProfileName:    opts.ProfileName,
		ImageName:      opts.ImageName,
		EnvFile:        opts.EnvFile,
		Agent:          opts.Agent,
		SshAgent:       opts.SSHAgent,
		Username:       opts.Username,
		Uid:            opts.Uid,
		AllowedDomains: append([]string(nil), opts.AllowedDomains...),
		Mounts:         append([]string(nil), opts.Mounts...),
		CloneMounts:    append([]string(nil), opts.CloneMounts...),
		SharedCaches: &daemonpb.SharedCacheConfig{
			Mise:      opts.SharedCaches.Mise,
			Apk:       opts.SharedCaches.APK,
			Agents:    opts.SharedCaches.Agents,
			Bazel:     opts.SharedCaches.Bazel,
			HttpProxy: opts.SharedCaches.HTTPProxy,
		},
		Cpus:   int32(opts.CPUs),
		Memory: int32(opts.Memory),
	}
}

func createSandboxOptsFromProto(req *daemonpb.CreateSandboxRequest) CreateSandboxOpts {
	opts := CreateSandboxOpts{
		Name:           req.GetId(),
		CloneFromDir:   req.GetCloneFromDir(),
		ProfileName:    req.GetProfileName(),
		ImageName:      req.GetImageName(),
		EnvFile:        req.GetEnvFile(),
		Agent:          req.GetAgent(),
		SSHAgent:       req.GetSshAgent(),
		Username:       req.GetUsername(),
		Uid:            req.GetUid(),
		AllowedDomains: append([]string(nil), req.GetAllowedDomains()...),
		Mounts:         append([]string(nil), req.GetMounts()...),
		CloneMounts:    append([]string(nil), req.GetCloneMounts()...),
		CPUs:           int(req.GetCpus()),
		Memory:         int(req.GetMemory()),
	}
	if sharedCaches := req.GetSharedCaches(); sharedCaches != nil {
		opts.SharedCaches = sandtypes.SharedCacheConfig{
			Mise:      sharedCaches.GetMise(),
			APK:       sharedCaches.GetApk(),
			Agents:    sharedCaches.GetAgents(),
			Bazel:     sharedCaches.GetBazel(),
			HTTPProxy: sharedCaches.GetHttpProxy(),
		}
	}
	return opts
}
