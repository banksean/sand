package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/banksean/sand/internal/agents"
	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/containerruntime"
	"github.com/banksean/sand/internal/hookscript"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/banksean/sand/internal/runtimepaths"
	"github.com/banksean/sand/internal/sandboxlog"
	"github.com/banksean/sand/internal/sandtypes"
)

const innieSocketPermissionScript = `[exists:/run/host-services] exec chmod 755 /run/host-services
[exists:/run/host-services/sandd.grpc.sock] exec chmod 666 /run/host-services/sandd.grpc.sock
[exists:/run/host-services/sandd.sock] exec chmod 666 /run/host-services/sandd.sock
`

type Store interface {
	GetContainer(ctx context.Context, containerID string) (*sandtypes.Container, error)
	UpdateContainerID(ctx context.Context, sbox *sandtypes.Box, containerID string) error
	UpdateContainerBootstrapped(ctx context.Context, sbox *sandtypes.Box, bootstrapped bool) error
}

type Service struct {
	AppRoot          string
	ContainerService hostops.ContainerOps
	ImageService     hostops.ImageOps
	AgentRegistry    *agents.AgentRegistry
	Store            Store
}

type Deps struct {
	AppRoot          string
	ContainerService hostops.ContainerOps
	ImageService     hostops.ImageOps
	AgentRegistry    *agents.AgentRegistry
	Store            Store
}

func NewService(deps Deps) *Service {
	return &Service{
		AppRoot:          deps.AppRoot,
		ContainerService: deps.ContainerService,
		ImageService:     deps.ImageService,
		AgentRegistry:    deps.AgentRegistry,
		Store:            deps.Store,
	}
}

type hookExecutor struct {
	ctx         context.Context
	sandboxID   string
	containerID string
	container   hostops.ContainerOps
	progress    io.Writer
	env         []string
}

func (h hookExecutor) Exec(ctx context.Context, shellCmd string, args ...string) (string, error) {
	output, err := h.container.Exec(ctx,
		&hostops.ExecContainer{
			ProcessOptions: hostops.ProcessOptions{
				Interactive: false,
				WorkDir:     "/app",
			},
		}, h.containerID, shellCmd, h.env, args...)
	if err != nil {
		slog.ErrorContext(h.ctx, "shell: containerService.Exec", "sandbox", h.sandboxID, "error", err, "output", output)
		return output, fmt.Errorf("failed to execute command for sandbox %s: %w", h.sandboxID, err)
	}
	return output, nil
}

func (h hookExecutor) ExecStream(ctx context.Context, stdout, stderr io.Writer, shellCmd string, args ...string) error {
	return h.ExecStreamInput(ctx, nil, stdout, stderr, shellCmd, args...)
}

func (h hookExecutor) ExecStreamInput(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, shellCmd string, args ...string) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	if h.progress != nil {
		stdout = io.MultiWriter(stdout, h.progress)
		stderr = io.MultiWriter(stderr, h.progress)
	}

	wait, err := h.container.ExecStream(ctx,
		&hostops.ExecContainer{
			ProcessOptions: hostops.ProcessOptions{
				Interactive: false,
				WorkDir:     "/app",
			},
		}, h.containerID, shellCmd, h.env,
		stdin, stdout, stderr, args...)
	if err != nil {
		slog.ErrorContext(h.ctx, "shell: containerService.ExecStream", "sandbox", h.sandboxID, "error", err, "command", shellCmd)
		return fmt.Errorf("failed to start command for sandbox %s: %w", h.sandboxID, err)
	}
	if err := wait(); err != nil {
		slog.ErrorContext(h.ctx, "shell: containerService.ExecStream wait", "sandbox", h.sandboxID, "error", err, "command", shellCmd)
		return fmt.Errorf("failed to execute command for sandbox %s: %w", h.sandboxID, err)
	}
	return nil
}

func startHooks(hooks []sandtypes.ContainerHook) []sandtypes.ContainerHook {
	systemHooks := []sandtypes.ContainerHook{
		InnieSocketPermissionHook(),
	}
	return append(systemHooks, hooks...)
}

func InnieSocketPermissionHook() sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("repair host service socket permissions", func(ctx context.Context, ctr *sandtypes.Container, exec sandtypes.HookStreamer) error {
		var out bytes.Buffer
		if err := hookscript.Execute(ctx, exec, "innie-socket-permissions.txt", innieSocketPermissionScript, &out); err != nil {
			if out.String() != "" {
				return fmt.Errorf("repair host service socket permissions: %w: %s", err, strings.TrimSpace(out.String()))
			}
			return fmt.Errorf("repair host service socket permissions: %w", err)
		}
		return nil
	})
}

func hookExecutionEnv(sharedCaches sandtypes.SharedCacheMounts) []string {
	env := []string{}
	for key, value := range sandtypes.SharedHTTPProxyEnv(sharedCaches.HTTPProxyURL) {
		env = setEnvValue(env, key, value)
	}
	return env
}

func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func runtimeArtifactsFromBox(sb *sandtypes.Box) containerruntime.Artifacts {
	pathRegistry := cloning.NewStandardPathRegistry(sb.SandboxWorkDir)
	return containerruntime.Artifacts{
		SandboxWorkDir:    sb.SandboxWorkDir,
		WorkDir:           pathRegistry.WorkDir(),
		DotfilesDir:       pathRegistry.DotfilesDir(),
		SSHKeysDir:        pathRegistry.SSHKeysDir(),
		Username:          sb.Username,
		Uid:               sb.Uid,
		SharedCacheMounts: sb.SharedCacheMounts,
	}
}

func (s *Service) EffectiveMounts(sb *sandtypes.Box) []sandtypes.MountSpec {
	if len(sb.Mounts) > 0 {
		return sb.Mounts
	}
	if sb.SandboxWorkDir == "" {
		return nil
	}

	pathRegistry := cloning.NewStandardPathRegistry(sb.SandboxWorkDir)
	baseConfig := containerruntime.NewBaseContainerConfiguration()
	return baseConfig.GetMounts(containerruntime.Artifacts{
		SandboxWorkDir:    sb.SandboxWorkDir,
		WorkDir:           pathRegistry.WorkDir(),
		DotfilesDir:       pathRegistry.DotfilesDir(),
		SSHKeysDir:        pathRegistry.SSHKeysDir(),
		SharedCacheMounts: sb.SharedCacheMounts,
	})
}

func (s *Service) CreateContainer(ctx context.Context, sb *sandtypes.Box, enableSSHAgent bool) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	mounts := s.EffectiveMounts(sb)
	mountOpts := make([]string, 0, len(mounts))
	for _, m := range mounts {
		mountOpts = append(mountOpts, m.String())
	}

	mountOpts = append(effectiveRuntimeMounts(sb), mountOpts...)

	volumeOpts := []string{}
	volumeOpts = append(volumeOpts, runtimepaths.ContainerHTTPSocketPath(sb.ID)+":/run/host-services/sandd.sock")
	volumeOpts = append(volumeOpts, runtimepaths.ContainerGRPCSocketPath(sb.ID)+":/run/host-services/sandd.grpc.sock")
	if sb.SharedCacheMounts.HTTPProxyCAHostPath != "" {
		volumeOpts = append(volumeOpts, sb.SharedCacheMounts.HTTPProxyCAHostPath+":"+sandtypes.HTTPProxyCACertContainerPath+":ro")
	}

	mgmtOpts := hostops.ManagementOptions{
		Name:      sandboxContainerName(sb),
		SSH:       enableSSHAgent,
		DNSDomain: sb.DNSDomain,
		Remove:    false,
		Mount:     mountOpts,
		Volume:    volumeOpts,
	}
	resOpts := hostops.ResourceOptions{
		CPUs:   sb.CPUs,
		Memory: fmt.Sprintf("%dM", sb.MemoryMB),
	}
	if len(sb.AllowedDomains) > 0 {
		mgmtOpts.InitImage = runtimedeps.CustomInitImage
		mgmtOpts.DNS = "127.0.0.1"
		mgmtOpts.Kernel = filepath.Join(s.AppRoot, "kernel", runtimedeps.CustomKernelReleaseVersion, "vmlinux")
	}
	if err := s.checkImageHasEntrypoint(ctx, sb.ImageName); err != nil {
		mgmtOpts.Entrypoint = "/bin/sh"
	}

	if platform, err := s.selectImagePlatform(ctx, sb.ImageName); err != nil {
		slog.WarnContext(ctx, "selectImagePlatform", "image", sb.ImageName, "error", err)
	} else if platform != "" {
		mgmtOpts.Platform = platform
	}

	containerID, err := s.ContainerService.Create(ctx,
		&hostops.CreateContainer{
			ProcessOptions: hostops.ProcessOptions{
				Interactive: true,
				TTY:         true,
			},
			ManagementOptions: mgmtOpts,
			ResourceOptions:   resOpts,
		},
		sb.ImageName, nil)
	if err != nil {
		slog.ErrorContext(ctx, "createContainer", "imageName", sb.ImageName, "error", err, "output", containerID)
		return fmt.Errorf("failed to create container for sandbox %s: %w", sb.ID, err)
	}

	sb.ContainerID = containerID
	return nil
}

func effectiveRuntimeMounts(sb *sandtypes.Box) []string {
	return sandtypes.RuntimeMountRequests(sb.MountRequests)
}

func (s *Service) RecreateContainer(ctx context.Context, sb *sandtypes.Box, enableSSHAgent bool) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	if sb.ContainerID != "" {
		out, err := s.ContainerService.Stop(ctx, nil, sb.ContainerID)
		if err != nil {
			slog.WarnContext(ctx, "lifecycle.RecreateContainer stop old container", "containerID", sb.ContainerID, "error", err, "output", out)
		}

		out, err = s.ContainerService.Delete(ctx, nil, sb.ContainerID)
		if err != nil {
			return fmt.Errorf("delete old container for sandbox %s: %w", sb.ID, err)
		}
	}

	if err := s.CreateContainer(ctx, sb, enableSSHAgent); err != nil {
		return err
	}
	if err := s.Store.UpdateContainerID(ctx, sb, sb.ContainerID); err != nil {
		return err
	}
	return nil
}

func (s *Service) selectImagePlatform(ctx context.Context, imageName string) (string, error) {
	if imageName == "" {
		return "", nil
	}
	imgs, err := s.ImageService.Inspect(ctx, imageName)
	if err != nil {
		return "", err
	}
	if len(imgs) == 0 || len(imgs[0].Variants) == 0 {
		return "", nil
	}
	hostArch := runtime.GOARCH
	for _, v := range imgs[0].Variants {
		if v.Platform.OS == "linux" && v.Platform.Architecture == hostArch {
			return "", nil
		}
	}
	v := imgs[0].Variants[0]
	if v.Platform.OS == "" || v.Platform.Architecture == "" {
		return "", nil
	}
	return v.Platform.OS + "/" + v.Platform.Architecture, nil
}

func (s *Service) checkImageHasEntrypoint(ctx context.Context, imageName string) error {
	if imageName != "" {
		img, err := s.ImageService.Inspect(ctx, imageName)
		if err != nil {
			return err
		}
		if len(img) == 0 {
			return fmt.Errorf("image not found: %s", imageName)
		}
		for _, v := range img[0].Variants {
			if len(v.Config.Config.Cmd) != 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("image %q has no command or entrypoint specified for container process", imageName)
}

func (s *Service) StartNewContainer(ctx context.Context, sb *sandtypes.Box, progress io.Writer) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	artifacts := runtimeArtifactsFromBox(sb)
	agentConfig := s.AgentRegistry.Get(sb.AgentType)
	hooks := startHooks(agentConfig.Configuration.GetFirstStartHooks(artifacts))

	slog.InfoContext(ctx, "lifecycle.StartNewContainer", "box", *sb, "ContainerHooks", len(hooks))
	if err := s.startContainerProcess(ctx, sb.ID, sb.ContainerID); err != nil {
		return err
	}

	if err := s.ExecuteHooks(ctx, sb, hooks, progress); err != nil {
		return err
	}
	return s.Store.UpdateContainerBootstrapped(ctx, sb, true)
}

func (s *Service) StartExistingContainer(ctx context.Context, sb *sandtypes.Box) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	artifacts := runtimeArtifactsFromBox(sb)
	agentConfig := s.AgentRegistry.Get(sb.AgentType)
	hooks := startHooks(agentConfig.Configuration.GetStartHooks(artifacts))

	slog.InfoContext(ctx, "lifecycle.StartExistingContainer", "box", *sb, "ContainerHooks", len(hooks))
	if err := s.startContainerProcess(ctx, sb.ID, sb.ContainerID); err != nil {
		return err
	}

	return s.ExecuteHooks(ctx, sb, hooks, nil)
}

func (s *Service) startContainerProcess(ctx context.Context, sandboxID, containerID string) error {
	ctx = sandboxlog.WithSandboxID(ctx, sandboxID)
	slog.InfoContext(ctx, "lifecycle.startContainerProcess", "containerID", containerID)
	output, err := s.ContainerService.Start(ctx, nil, containerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainerProcess", "containerID", containerID, "error", err, "output", output)
		return fmt.Errorf("failed to start container for sandbox %s: %w", sandboxID, err)
	}
	slog.InfoContext(ctx, "lifecycle.startContainerProcess succeeded", "output", output)
	return nil
}

func (s *Service) ExecuteHooks(ctx context.Context, sb *sandtypes.Box, hooks []sandtypes.ContainerHook, progress io.Writer) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	var hookErrs []error
	for _, hook := range hooks {
		slog.InfoContext(ctx, "lifecycle.ExecuteHooks running hook", "hook", hook.Name())
		if progress != nil {
			fmt.Fprintf(progress, "[sand] %s\n", hook.Name())
		}
		ctr, err := s.Store.GetContainer(ctx, sb.ContainerID)
		if err != nil {
			return err
		}
		exec := hookExecutor{
			ctx:         ctx,
			sandboxID:   sb.ID,
			containerID: sb.ContainerID,
			container:   s.ContainerService,
			progress:    progress,
			env:         hookExecutionEnv(sb.SharedCacheMounts),
		}
		if err := hook.Run(ctx, ctr, exec); err != nil {
			slog.ErrorContext(ctx, "lifecycle.ExecuteHooks hook error", "hook", hook.Name(), "error", err)
			hookErrs = append(hookErrs, fmt.Errorf("%s: %w", hook.Name(), err))
		}
	}
	if len(hookErrs) > 0 {
		return errors.Join(hookErrs...)
	}
	return nil
}

func sandboxContainerName(box *sandtypes.Box) string {
	if box.Name != "" {
		return box.Name
	}
	return box.ID
}
