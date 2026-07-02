//go:build darwin && cgo

package hostops

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	ac "github.com/banksean/sand/internal/applecontainer"
	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/applecontainer/xpc"
	"github.com/google/uuid"
	"golang.org/x/term"
)

type xpcContainerOps struct {
	client      *xpc.Client
	imageClient *xpc.Client
}

func NewXPCContainerOps() (ContainerOps, error) {
	client, err := xpc.NewClient()
	if err != nil {
		return nil, err
	}
	imageClient, err := xpc.NewClient(xpc.WithService(xpc.ImageServiceIdentifier))
	if err != nil {
		client.Close()
		return nil, err
	}
	return &xpcContainerOps{client: client, imageClient: imageClient}, nil
}

func (o *xpcContainerOps) Create(ctx context.Context, opts *options.CreateContainer, imageName string, initArgs []string) (string, error) {
	if opts == nil {
		opts = &options.CreateContainer{}
	}
	id := opts.Name
	if id == "" {
		id = uuid.NewString()
	}
	platform := xpc.Platform{OS: "linux", Architecture: runtime.GOARCH}
	if platform.Architecture == "arm64" {
		platform.Variant = "v8"
	}
	if opts.Platform != "" {
		parsed, err := parsePlatform(opts.Platform)
		if err != nil {
			return "", err
		}
		platform = parsed
	}

	image, imageConfig, err := o.imageDescription(ctx, imageName, platform)
	if err != nil {
		return "", err
	}
	process, err := createProcessConfig(opts.ProcessOptions, opts.ManagementOptions, initArgs, imageConfig)
	if err != nil {
		return "", err
	}
	cfg := xpc.ContainerConfiguration{
		ID:          id,
		Image:       image,
		InitProcess: process,
		Platform:    platform,
		Resources: xpc.Resources{
			CPUs:          defaultInt(opts.CPUs, 4),
			MemoryInBytes: memoryBytes(opts.Memory, 1024*1024*1024),
			CPUOverhead:   1,
		},
		RuntimeHandler: "container-runtime-linux",
		SSH:            opts.SSH,
		Virtualization: opts.Virtualization,
		Labels:         opts.Label,
	}
	cfg.Mounts, err = parseFilesystems(append(append([]string{}, opts.Mount...), opts.Volume...))
	if err != nil {
		return "", err
	}
	cfg.Networks = defaultNetworkAttachments(id, opts.DNSDomain, opts.Netowrk)
	if !opts.NoDNS {
		cfg.DNS = &xpc.DNSConfiguration{
			Nameservers:   stringList(opts.DNS),
			Domain:        stringPtrOrNil(opts.DNSDomain),
			SearchDomains: stringList(opts.DNSSearch),
			Options:       stringList(opts.DNSOption),
		}
	}

	systemPlatform := xpc.CurrentSystemPlatform(runtime.GOARCH)
	kernel, err := o.kernel(ctx, opts.Kernel, systemPlatform)
	if err != nil {
		return "", err
	}
	if err := o.client.CreateContainer(ctx, cfg, xpc.ContainerCreateOptions{AutoRemove: opts.Remove}, kernel, opts.InitImage, nil); err != nil {
		return "", err
	}
	return id, nil
}

func (o *xpcContainerOps) Start(ctx context.Context, opts *options.StartContainer, containerID string) (string, error) {
	if err := o.client.BootstrapContainer(ctx, containerID, [3]*os.File{}, nil); err != nil {
		return "", err
	}
	if err := o.client.StartProcess(ctx, containerID, containerID); err != nil {
		return "", err
	}
	return containerID, nil
}

func (o *xpcContainerOps) Stop(ctx context.Context, opts *options.StopContainer, containerID string) (string, error) {
	stopOpts := xpc.ContainerStopOptions{}
	if opts != nil {
		stopOpts.TimeoutInSeconds = int32(opts.Time)
		if opts.Signal != "" {
			stopOpts.Signal = &opts.Signal
		}
	}
	if err := o.client.StopContainer(ctx, containerID, stopOpts); err != nil {
		return "", err
	}
	return containerID, nil
}

func (o *xpcContainerOps) Delete(ctx context.Context, opts *options.DeleteContainer, containerID string) (string, error) {
	force := opts != nil && opts.Force
	if err := o.client.DeleteContainer(ctx, containerID, force); err != nil {
		return "", err
	}
	return containerID, nil
}

func (o *xpcContainerOps) Exec(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	wait, err := o.ExecStream(ctx, opts, containerID, cmd, env, nil, &stdout, &stderr, args...)
	if err != nil {
		return "", err
	}
	err = wait()
	out := stdout.String() + stderr.String()
	if err != nil {
		return out, err
	}
	return out, nil
}

func (o *xpcContainerOps) ExecStream(ctx context.Context, opts *options.ExecContainer, containerID, cmd string, env []string, stdin io.Reader, stdout, stderr io.Writer, args ...string) (func() error, error) {
	if opts == nil {
		opts = &options.ExecContainer{}
	}
	snapshot, err := o.client.GetContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}
	cfg := snapshot.Configuration.InitProcess
	if err := applyExecOptions(&cfg, opts.ProcessOptions, cmd, args); err != nil {
		return nil, err
	}

	stdio, cleanup, err := processFiles(stdin, stdout, stderr, cfg.Terminal)
	if err != nil {
		return nil, err
	}
	processID := uuid.NewString()
	if err := o.client.CreateProcess(ctx, containerID, processID, cfg, stdio); err != nil {
		cleanup()
		return nil, err
	}

	var savedState *term.State
	if stdinFile, ok := stdin.(*os.File); ok && term.IsTerminal(int(stdinFile.Fd())) {
		savedState, err = term.MakeRaw(int(stdinFile.Fd()))
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("making terminal raw: %w", err)
		}
	}
	restore := func() {
		if savedState != nil {
			if stdinFile, ok := stdin.(*os.File); ok {
				_ = term.Restore(int(stdinFile.Fd()), savedState)
			}
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer signal.Stop(sigCh)
		for {
			select {
			case <-done:
				return
			case sig := <-sigCh:
				switch sig {
				case syscall.SIGWINCH:
					if w, h, ok := terminalSize(stdin, stdout); ok {
						_ = o.client.ResizeProcess(ctx, containerID, processID, uint64(w), uint64(h))
					}
				case syscall.SIGINT:
					_ = o.client.KillProcess(ctx, containerID, processID, int64(syscall.SIGINT))
				case syscall.SIGTERM:
					restore()
					_ = o.client.KillProcess(ctx, containerID, processID, int64(syscall.SIGTERM))
				}
			}
		}
	}()

	if err := o.client.StartProcess(ctx, containerID, processID); err != nil {
		close(done)
		restore()
		cleanup()
		return nil, err
	}

	return func() error {
		defer close(done)
		defer restore()
		defer cleanup()
		code, err := o.client.WaitProcess(ctx, containerID, processID)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("process exited with code %d", code)
		}
		return nil
	}, nil
}

func (o *xpcContainerOps) Inspect(ctx context.Context, containerID string) ([]types.Container, error) {
	ctr, err := o.client.GetContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}
	return []types.Container{xpcSnapshotToContainer(ctr)}, nil
}

func (o *xpcContainerOps) Stats(ctx context.Context, containerID ...string) ([]types.ContainerStats, error) {
	var ret []types.ContainerStats
	for _, id := range containerID {
		stats, err := o.client.ContainerStats(ctx, id)
		if err != nil {
			return nil, err
		}
		ret = append(ret, xpcStatsToTypes(stats))
	}
	return ret, nil
}

func (o *xpcContainerOps) Export(ctx context.Context, opts *options.ExportContainer, containerID string) (string, error) {
	if opts == nil || opts.Output == "" {
		return "", fmt.Errorf("export output path is required")
	}
	if err := o.client.ExportContainer(ctx, containerID, opts.Output); err != nil {
		return "", err
	}
	return "", nil
}

func (o *xpcContainerOps) imageDescription(ctx context.Context, imageName string, platform xpc.Platform) (xpc.ImageDescription, *types.ImageVariantContainerConfig, error) {
	images, err := o.imageClient.ListImages(ctx)
	if err != nil {
		return xpc.ImageDescription{}, nil, err
	}
	var desc *xpc.ImageDescription
	for i := range images {
		if images[i].Reference == imageName {
			desc = &images[i]
			break
		}
	}
	if desc == nil {
		return xpc.ImageDescription{}, nil, fmt.Errorf("image %q not found", imageName)
	}

	manifests, err := ac.Images.Inspect(ctx, imageName)
	if err != nil || len(manifests) == 0 {
		return *desc, nil, nil
	}
	for _, variant := range manifests[0].Variants {
		if variant.Platform.OS == platform.OS && variant.Platform.Architecture == platform.Architecture {
			return *desc, &variant.Config.Config, nil
		}
	}
	if len(manifests[0].Variants) > 0 {
		return *desc, &manifests[0].Variants[0].Config.Config, nil
	}
	return *desc, nil, nil
}

func (o *xpcContainerOps) kernel(ctx context.Context, customPath string, platform xpc.SystemPlatform) (xpc.Kernel, error) {
	if customPath != "" {
		return xpc.NewKernel(customPath, platform), nil
	}
	return o.client.GetDefaultKernel(ctx, platform)
}

func createProcessConfig(process options.ProcessOptions, management options.ManagementOptions, initArgs []string, imageConfig *types.ImageVariantContainerConfig) (xpc.ProcessConfiguration, error) {
	args := append([]string{}, initArgs...)
	if management.Entrypoint != "" {
		args = append([]string{management.Entrypoint}, args...)
	} else if len(args) == 0 && imageConfig != nil && len(imageConfig.Cmd) > 0 {
		args = append(args, imageConfig.Cmd...)
	}
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}
	workingDir := process.WorkDir
	if workingDir == "" && imageConfig != nil {
		workingDir = imageConfig.WorkingDir
	}
	if workingDir == "" {
		workingDir = "/"
	}
	env, err := processEnvironment(process, imageConfig)
	if err != nil {
		return xpc.ProcessConfiguration{}, err
	}
	user := xpc.IDProcessUser(0, 0)
	if process.User != "" {
		user = xpc.RawProcessUser(process.User)
	} else if process.UID != "" {
		user = xpc.RawProcessUser(process.UID)
	}
	return xpc.ProcessConfiguration{
		Executable:       args[0],
		Arguments:        args[1:],
		Environment:      env,
		WorkingDirectory: workingDir,
		Terminal:         process.TTY,
		User:             user,
	}, nil
}

func applyExecOptions(cfg *xpc.ProcessConfiguration, process options.ProcessOptions, cmd string, args []string) error {
	cfg.Executable = cmd
	cfg.Arguments = append([]string{}, args...)
	cfg.Terminal = process.TTY
	if process.WorkDir != "" {
		cfg.WorkingDirectory = process.WorkDir
	}
	env, err := processEnvironment(process, nil)
	if err != nil {
		return err
	}
	cfg.Environment = append(cfg.Environment, env...)
	if process.User != "" {
		cfg.User = xpc.RawProcessUser(process.User)
	} else if process.UID != "" {
		cfg.User = xpc.RawProcessUser(process.UID)
	}
	return nil
}

func processEnvironment(process options.ProcessOptions, imageConfig *types.ImageVariantContainerConfig) ([]string, error) {
	var env []string
	if imageConfig != nil {
		env = append(env, imageConfig.Env...)
	}
	if process.EnvFile != "" {
		fileEnv, err := parseEnvFile(process.EnvFile)
		if err != nil {
			return nil, err
		}
		env = append(env, fileEnv...)
	}
	keys := make([]string, 0, len(process.Env))
	for key := range process.Env {
		keys = append(keys, key)
	}
	for _, key := range keys {
		env = append(env, key+"="+process.Env[key])
	}
	return env, nil
}

func parseEnvFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var env []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		env = append(env, line)
	}
	return env, scanner.Err()
}

func parseFilesystems(specs []string) ([]xpc.Filesystem, error) {
	var filesystems []xpc.Filesystem
	for _, spec := range specs {
		if strings.Contains(spec, ":") && !strings.Contains(spec, "source=") {
			fs, err := parseVolumeFilesystem(spec)
			if err != nil {
				return nil, err
			}
			filesystems = append(filesystems, fs)
			continue
		}
		fs, err := parseMountFilesystem(spec)
		if err != nil {
			return nil, err
		}
		filesystems = append(filesystems, fs)
	}
	return filesystems, nil
}

func parseMountFilesystem(spec string) (xpc.Filesystem, error) {
	parts := strings.Split(spec, ",")
	values := map[string]string{"type": "virtiofs"}
	var readonly bool
	for _, part := range parts {
		if part == "readonly" || part == "ro" {
			readonly = true
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return xpc.Filesystem{}, fmt.Errorf("invalid mount directive %q", part)
		}
		switch key {
		case "type":
			if val == "bind" {
				val = "virtiofs"
			}
			values["type"] = val
		case "source", "src":
			values["source"] = val
		case "target", "destination", "dst":
			values["destination"] = val
		}
	}
	options := []string{}
	if readonly {
		options = append(options, "ro")
	}
	if values["type"] == "tmpfs" {
		return xpc.Filesystem{Type: xpc.FilesystemType{Kind: xpc.FilesystemTypeTmpfs}, Source: "tmpfs", Destination: values["destination"], Options: options}, nil
	}
	return xpc.Filesystem{Type: xpc.FilesystemType{Kind: xpc.FilesystemTypeVirtiofs}, Source: values["source"], Destination: values["destination"], Options: options}, nil
}

func parseVolumeFilesystem(spec string) (xpc.Filesystem, error) {
	parts := strings.Split(spec, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return xpc.Filesystem{}, fmt.Errorf("invalid volume spec %q", spec)
	}
	source, err := filepath.Abs(parts[0])
	if err != nil {
		return xpc.Filesystem{}, err
	}
	options := []string{}
	if len(parts) == 3 && parts[2] != "" {
		options = strings.Split(parts[2], ",")
	}
	return xpc.Filesystem{
		Type:        xpc.FilesystemType{Kind: xpc.FilesystemTypeVirtiofs},
		Source:      source,
		Destination: parts[1],
		Options:     options,
	}, nil
}

func processFiles(stdin io.Reader, stdout, stderr io.Writer, tty bool) ([3]*os.File, func(), error) {
	var files [3]*os.File
	var cleanups []func()
	addCleanup := func(fn func()) { cleanups = append(cleanups, fn) }
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	if f, ok := stdin.(*os.File); ok {
		files[0] = f
	} else if stdin != nil {
		r, w, err := os.Pipe()
		if err != nil {
			return files, cleanup, err
		}
		files[0] = r
		addCleanup(func() { r.Close(); w.Close() })
		go func() {
			io.Copy(w, stdin) //nolint:errcheck
			w.Close()
		}()
	}
	outFile, outCleanup, err := writerFile(stdout)
	if err != nil {
		cleanup()
		return files, cleanup, err
	}
	files[1] = outFile
	addCleanup(outCleanup)
	if !tty {
		errFile, errCleanup, err := writerFile(stderr)
		if err != nil {
			cleanup()
			return files, cleanup, err
		}
		files[2] = errFile
		addCleanup(errCleanup)
	}
	return files, cleanup, nil
}

func writerFile(w io.Writer) (*os.File, func(), error) {
	if w == nil {
		w = io.Discard
	}
	if file, ok := w.(*os.File); ok {
		return file, func() {}, nil
	}
	r, file, err := os.Pipe()
	if err != nil {
		return nil, func() {}, err
	}
	var once sync.Once
	done := make(chan struct{})
	go func() {
		io.Copy(w, r) //nolint:errcheck
		r.Close()
		close(done)
	}()
	cleanup := func() {
		once.Do(func() {
			file.Close()
			<-done
		})
	}
	return file, cleanup, nil
}

func terminalSize(stdin io.Reader, stdout io.Writer) (int, int, bool) {
	for _, v := range []any{stdout, stdin} {
		if f, ok := v.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			w, h, err := term.GetSize(int(f.Fd()))
			return w, h, err == nil
		}
	}
	return 0, 0, false
}

func parsePlatform(platform string) (xpc.Platform, error) {
	parts := strings.Split(platform, "/")
	if len(parts) < 2 {
		return xpc.Platform{}, fmt.Errorf("invalid platform %q", platform)
	}
	p := xpc.Platform{OS: parts[0], Architecture: parts[1]}
	if len(parts) > 2 {
		p.Variant = parts[2]
	} else if p.Architecture == "arm64" {
		p.Variant = "v8"
	}
	return p, nil
}

func defaultNetworkAttachments(id, domain, network string) []xpc.AttachmentConfiguration {
	if network == "" {
		network = "default"
	}
	hostname := id
	if domain != "" && !strings.Contains(id, ".") {
		hostname = id + "." + domain + "."
	} else if strings.Contains(id, ".") {
		hostname = id + "."
	}
	mtu := uint32(1280)
	return []xpc.AttachmentConfiguration{{Network: network, Options: xpc.AttachmentOptions{Hostname: hostname, MTU: &mtu}}}
}

func xpcSnapshotToContainer(snapshot xpc.ContainerSnapshot) types.Container {
	ctr := types.Container{
		Status: types.ContainerStatus{State: string(snapshot.Status)},
		Configuration: types.ContainerConfig{
			ID:  snapshot.Configuration.ID,
			SSH: snapshot.Configuration.SSH,
		},
	}
	for _, network := range snapshot.Networks {
		ctr.Networks = append(ctr.Networks, struct {
			Hostname    string `json:"hostname"`
			Network     string `json:"network"`
			IPv4Address string `json:"ipv4Address"`
			IPv4Gateway string `json:"ipv4Gateway"`
			IPv6Address string `json:"ipv6Address"`
			IPv6Gateway string `json:"ipv6Gateway"`
		}{
			Hostname:    network.Hostname,
			Network:     network.Network,
			IPv4Address: string(network.IPv4Address),
			IPv4Gateway: string(network.IPv4Gateway),
		})
	}
	for _, network := range snapshot.Configuration.Networks {
		ctr.Configuration.Networks = append(ctr.Configuration.Networks, types.ContainerNetwork{
			Network: network.Network,
			Options: types.NetowrkOptions{
				Hostname: network.Options.Hostname,
			},
		})
	}
	return ctr
}

func xpcStatsToTypes(stats xpc.ContainerStats) types.ContainerStats {
	return types.ContainerStats{
		ID:               stats.ID,
		MemoryUsageBytes: uint64PtrToInt(stats.MemoryUsageBytes),
		MemoryLimitBytes: uint64PtrToInt(stats.MemoryLimitBytes),
		CPUUsageUsec:     uint64PtrToInt(stats.CPUUsageUsec),
		NetworkRxBytes:   uint64PtrToInt(stats.NetworkRxBytes),
		NetworkTxBytes:   uint64PtrToInt(stats.NetworkTxBytes),
		BlockReadBytes:   uint64PtrToInt(stats.BlockReadBytes),
		BlockWriteBytes:  uint64PtrToInt(stats.BlockWriteBytes),
		NumProcesses:     uint64PtrToInt(stats.NumProcesses),
	}
}

func uint64PtrToInt(v *uint64) int {
	if v == nil {
		return 0
	}
	return int(*v)
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringList(value string) []string {
	if value == "" {
		return []string{}
	}
	return []string{value}
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func memoryBytes(value string, fallback uint64) uint64 {
	if value == "" {
		return fallback
	}
	raw := strings.TrimSpace(strings.ToUpper(value))
	multiplier := uint64(1)
	switch {
	case strings.HasSuffix(raw, "G"):
		multiplier = 1024 * 1024 * 1024
		raw = strings.TrimSuffix(raw, "G")
	case strings.HasSuffix(raw, "M"):
		multiplier = 1024 * 1024
		raw = strings.TrimSuffix(raw, "M")
	case strings.HasSuffix(raw, "K"):
		multiplier = 1024
		raw = strings.TrimSuffix(raw, "K")
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed * multiplier
}
