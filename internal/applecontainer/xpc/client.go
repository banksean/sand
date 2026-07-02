package xpc

import (
	"context"
	"fmt"
	"os"
	"time"
)

const (
	DefaultServiceIdentifier = "com.apple.container.apiserver"
	ImageServiceIdentifier   = "com.apple.container.core.container-core-images"
	DefaultRequestTimeout    = 60 * time.Second
)

type Sender interface {
	Send(ctx context.Context, message *Message) (*Message, error)
	Close() error
}

type Client struct {
	service string
	timeout time.Duration
	sender  Sender
}

type ClientOption func(*Client) error

func WithService(service string) ClientOption {
	return func(c *Client) error {
		if service == "" {
			return fmt.Errorf("service cannot be empty")
		}
		c.service = service
		return nil
	}
}

func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) error {
		c.timeout = timeout
		return nil
	}
}

func WithSender(sender Sender) ClientOption {
	return func(c *Client) error {
		if sender == nil {
			return fmt.Errorf("sender cannot be nil")
		}
		c.sender = sender
		return nil
	}
}

func NewClient(opts ...ClientOption) (*Client, error) {
	c := &Client{
		service: DefaultServiceIdentifier,
		timeout: DefaultRequestTimeout,
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	if c.sender == nil {
		sender, err := newDefaultSender(c.service)
		if err != nil {
			return nil, err
		}
		c.sender = sender
	}
	return c, nil
}

func (c *Client) Close() error {
	if c == nil || c.sender == nil {
		return nil
	}
	return c.sender.Close()
}

func (c *Client) Send(ctx context.Context, route XPCRoute, build func(*Message) error) (*Message, error) {
	if c == nil || c.sender == nil {
		return nil, fmt.Errorf("xpc client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok && c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	message := NewMessage(route)
	if build != nil {
		if err := build(message); err != nil {
			return nil, err
		}
	}
	reply, err := c.sender.Send(ctx, message)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("nil XPC reply for route %q", route)
	}
	if err := reply.checkProtocolError(); err != nil {
		return nil, err
	}
	return reply, nil
}

func (c *Client) ListContainers(ctx context.Context, filters ContainerListFilters) ([]ContainerSnapshot, error) {
	reply, err := c.Send(ctx, XPCRouteContainerList, func(message *Message) error {
		return message.SetJSON(XPCKeyListFilters, filters)
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	data, ok := reply.Data(XPCKeyContainers)
	if !ok {
		return []ContainerSnapshot{}, nil
	}
	var containers []ContainerSnapshot
	if err := decodeJSONData(data, &containers); err != nil {
		return nil, fmt.Errorf("decode containers: %w", err)
	}
	return containers, nil
}

func (c *Client) GetContainer(ctx context.Context, id string) (ContainerSnapshot, error) {
	containers, err := c.ListContainers(ctx, ContainerListFilters{IDs: []string{id}})
	if err != nil {
		return ContainerSnapshot{}, err
	}
	if len(containers) == 0 {
		return ContainerSnapshot{}, fmt.Errorf("container %q not found", id)
	}
	return containers[0], nil
}

func (c *Client) CreateContainer(ctx context.Context, cfg ContainerConfiguration, opts ContainerCreateOptions, kernel Kernel, initImage string, runtimeData []byte) error {
	_, err := c.Send(ctx, XPCRouteContainerCreate, func(message *Message) error {
		if err := message.SetJSON(XPCKeyContainerConfig, cfg); err != nil {
			return err
		}
		if err := message.SetJSON(XPCKeyKernel, kernel); err != nil {
			return err
		}
		if err := message.SetJSON(XPCKeyContainerOptions, opts); err != nil {
			return err
		}
		if initImage != "" {
			message.SetString(XPCKeyInitImage, initImage)
		}
		if runtimeData != nil {
			message.SetData(XPCKeyRuntimeData, runtimeData)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("create container %q: %w", cfg.ID, err)
	}
	return nil
}

func (c *Client) BootstrapContainer(ctx context.Context, id string, stdio [3]*os.File, dynamicEnv map[string]string) error {
	_, err := c.Send(ctx, XPCRouteContainerBootstrap, func(message *Message) error {
		message.SetString(XPCKeyID, id)
		setStdio(message, stdio)
		return message.SetJSON(XPCKeyDynamicEnv, stringMap(dynamicEnv))
	})
	if err != nil {
		return fmt.Errorf("bootstrap container %q: %w", id, err)
	}
	return nil
}

func (c *Client) StopContainer(ctx context.Context, id string, opts ContainerStopOptions) error {
	_, err := c.Send(ctx, XPCRouteContainerStop, func(message *Message) error {
		message.SetString(XPCKeyID, id)
		return message.SetJSON(XPCKeyStopOptions, opts.withDefaults())
	})
	if err != nil {
		return fmt.Errorf("stop container %q: %w", id, err)
	}
	return nil
}

func (c *Client) CreateProcess(ctx context.Context, containerID, processID string, cfg ProcessConfiguration, stdio [3]*os.File) error {
	_, err := c.Send(ctx, XPCRouteContainerCreateProcess, func(message *Message) error {
		message.SetString(XPCKeyID, containerID)
		message.SetString(XPCKeyProcessIdentifier, processID)
		if err := message.SetJSON(XPCKeyProcessConfig, cfg); err != nil {
			return err
		}
		setStdio(message, stdio)
		return nil
	})
	if err != nil {
		return fmt.Errorf("create process %q in container %q: %w", processID, containerID, err)
	}
	return nil
}

func (c *Client) StartProcess(ctx context.Context, containerID, processID string) error {
	_, err := c.Send(ctx, XPCRouteContainerStartProcess, func(message *Message) error {
		message.SetString(XPCKeyID, containerID)
		message.SetString(XPCKeyProcessIdentifier, processID)
		return nil
	})
	if err != nil {
		return fmt.Errorf("start process %q in container %q: %w", processID, containerID, err)
	}
	return nil
}

func (c *Client) WaitProcess(ctx context.Context, containerID, processID string) (int32, error) {
	reply, err := c.Send(ctx, XPCRouteContainerWait, func(message *Message) error {
		message.SetString(XPCKeyID, containerID)
		message.SetString(XPCKeyProcessIdentifier, processID)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("wait process %q in container %q: %w", processID, containerID, err)
	}
	return int32(reply.Int64(XPCKeyExitCode)), nil
}

func (c *Client) ResizeProcess(ctx context.Context, containerID, processID string, width, height uint64) error {
	_, err := c.Send(ctx, XPCRouteContainerResize, func(message *Message) error {
		message.SetString(XPCKeyID, containerID)
		message.SetString(XPCKeyProcessIdentifier, processID)
		message.SetUint64(XPCKeyWidth, width)
		message.SetUint64(XPCKeyHeight, height)
		return nil
	})
	if err != nil {
		return fmt.Errorf("resize process %q in container %q: %w", processID, containerID, err)
	}
	return nil
}

func (c *Client) KillProcess(ctx context.Context, containerID, processID string, signal int64) error {
	_, err := c.Send(ctx, XPCRouteContainerKill, func(message *Message) error {
		message.SetString(XPCKeyID, containerID)
		message.SetString(XPCKeyProcessIdentifier, processID)
		message.SetInt64(XPCKeySignal, signal)
		return nil
	})
	if err != nil {
		return fmt.Errorf("kill process %q in container %q: %w", processID, containerID, err)
	}
	return nil
}

func (c *Client) DeleteContainer(ctx context.Context, id string, force bool) error {
	_, err := c.Send(ctx, XPCRouteContainerDelete, func(message *Message) error {
		message.SetString(XPCKeyID, id)
		message.SetBool(XPCKeyForceDelete, force)
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete container %q: %w", id, err)
	}
	return nil
}

func (c *Client) ExportContainer(ctx context.Context, id, archivePath string) error {
	_, err := c.Send(ctx, XPCRouteContainerExport, func(message *Message) error {
		message.SetString(XPCKeyID, id)
		message.SetString(XPCKeyArchive, archivePath)
		return nil
	})
	if err != nil {
		return fmt.Errorf("export container %q: %w", id, err)
	}
	return nil
}

func (c *Client) ContainerStats(ctx context.Context, id string) (ContainerStats, error) {
	reply, err := c.Send(ctx, XPCRouteContainerStats, func(message *Message) error {
		message.SetString(XPCKeyID, id)
		return nil
	})
	if err != nil {
		return ContainerStats{}, fmt.Errorf("container stats %q: %w", id, err)
	}
	var stats ContainerStats
	if err := reply.DecodeJSON(XPCKeyStatistics, &stats); err != nil {
		return ContainerStats{}, err
	}
	return stats, nil
}

func (c *Client) ContainerDiskUsage(ctx context.Context, id string) (uint64, error) {
	reply, err := c.Send(ctx, XPCRouteContainerDiskUsage, func(message *Message) error {
		message.SetString(XPCKeyID, id)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("container disk usage %q: %w", id, err)
	}
	return reply.Uint64(XPCKeyContainerSize), nil
}

func (c *Client) ListNetworks(ctx context.Context) ([]NetworkResource, error) {
	reply, err := c.Send(ctx, XPCRouteNetworkList, nil)
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	data, ok := reply.Data(XPCKeyNetworkResources)
	if !ok {
		return []NetworkResource{}, nil
	}
	var networks []NetworkResource
	if err := decodeJSONData(data, &networks); err != nil {
		return nil, fmt.Errorf("decode networks: %w", err)
	}
	return networks, nil
}

func (c *Client) GetNetwork(ctx context.Context, id string) (NetworkResource, error) {
	networks, err := c.ListNetworks(ctx)
	if err != nil {
		return NetworkResource{}, err
	}
	for _, network := range networks {
		if network.ID() == id {
			return network, nil
		}
	}
	return NetworkResource{}, fmt.Errorf("network %q not found", id)
}

func (c *Client) CreateNetwork(ctx context.Context, cfg NetworkConfiguration) (NetworkResource, error) {
	reply, err := c.Send(ctx, XPCRouteNetworkCreate, func(message *Message) error {
		message.SetString(XPCKeyNetworkID, cfg.ID())
		return message.SetJSON(XPCKeyNetworkConfig, cfg)
	})
	if err != nil {
		return NetworkResource{}, fmt.Errorf("create network %q: %w", cfg.ID(), err)
	}
	var resource NetworkResource
	if err := reply.DecodeJSON(XPCKeyNetworkResource, &resource); err != nil {
		return NetworkResource{}, err
	}
	return resource, nil
}

func (c *Client) DeleteNetwork(ctx context.Context, id string) error {
	_, err := c.Send(ctx, XPCRouteNetworkDelete, func(message *Message) error {
		message.SetString(XPCKeyNetworkID, id)
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete network %q: %w", id, err)
	}
	return nil
}

func (c *Client) ListVolumes(ctx context.Context) ([]VolumeConfiguration, error) {
	reply, err := c.Send(ctx, XPCRouteVolumeList, nil)
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	data, ok := reply.Data(XPCKeyVolumes)
	if !ok {
		return []VolumeConfiguration{}, nil
	}
	var volumes []VolumeConfiguration
	if err := decodeJSONData(data, &volumes); err != nil {
		return nil, fmt.Errorf("decode volumes: %w", err)
	}
	return volumes, nil
}

func (c *Client) InspectVolume(ctx context.Context, name string) (VolumeConfiguration, error) {
	reply, err := c.Send(ctx, XPCRouteVolumeInspect, func(message *Message) error {
		message.SetString(XPCKeyVolumeName, name)
		return nil
	})
	if err != nil {
		return VolumeConfiguration{}, fmt.Errorf("inspect volume %q: %w", name, err)
	}
	var volume VolumeConfiguration
	if err := reply.DecodeJSON(XPCKeyVolume, &volume); err != nil {
		return VolumeConfiguration{}, err
	}
	return volume, nil
}

func (c *Client) CreateVolume(ctx context.Context, name, driver string, driverOpts, labels map[string]string) (VolumeConfiguration, error) {
	if driver == "" {
		driver = "local"
	}
	reply, err := c.Send(ctx, XPCRouteVolumeCreate, func(message *Message) error {
		message.SetString(XPCKeyVolumeName, name)
		message.SetString(XPCKeyVolumeDriver, driver)
		if err := message.SetJSON(XPCKeyVolumeDriverOpts, stringMap(driverOpts)); err != nil {
			return err
		}
		return message.SetJSON(XPCKeyVolumeLabels, stringMap(labels))
	})
	if err != nil {
		return VolumeConfiguration{}, fmt.Errorf("create volume %q: %w", name, err)
	}
	var volume VolumeConfiguration
	if err := reply.DecodeJSON(XPCKeyVolume, &volume); err != nil {
		return VolumeConfiguration{}, err
	}
	return volume, nil
}

func (c *Client) DeleteVolume(ctx context.Context, name string) error {
	_, err := c.Send(ctx, XPCRouteVolumeDelete, func(message *Message) error {
		message.SetString(XPCKeyVolumeName, name)
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete volume %q: %w", name, err)
	}
	return nil
}

func (c *Client) VolumeDiskUsage(ctx context.Context, name string) (uint64, error) {
	reply, err := c.Send(ctx, XPCRouteVolumeDiskUsage, func(message *Message) error {
		message.SetString(XPCKeyVolumeName, name)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("volume disk usage %q: %w", name, err)
	}
	return reply.Uint64(XPCKeyVolumeSize), nil
}

func (c *Client) SystemDiskUsage(ctx context.Context) (DiskUsageStats, error) {
	reply, err := c.Send(ctx, XPCRouteSystemDiskUsage, nil)
	if err != nil {
		return DiskUsageStats{}, fmt.Errorf("system disk usage: %w", err)
	}
	var stats DiskUsageStats
	if err := reply.DecodeJSON(XPCKeyDiskUsageStats, &stats); err != nil {
		return DiskUsageStats{}, err
	}
	return stats, nil
}

func (c *Client) Ping(ctx context.Context) (SystemHealth, error) {
	reply, err := c.Send(ctx, XPCRoutePing, nil)
	if err != nil {
		return SystemHealth{}, fmt.Errorf("ping: %w", err)
	}
	return decodeSystemHealth(reply)
}

func (c *Client) GetDefaultKernel(ctx context.Context, platform SystemPlatform) (Kernel, error) {
	reply, err := c.Send(ctx, XPCRouteGetDefaultKernel, func(message *Message) error {
		return message.SetJSON(XPCKeySystemPlatform, platform)
	})
	if err != nil {
		return Kernel{}, fmt.Errorf("get default kernel: %w", err)
	}
	var kernel Kernel
	if err := reply.DecodeJSON(XPCKeyKernel, &kernel); err != nil {
		return Kernel{}, err
	}
	return kernel, nil
}

func (c *Client) ListImages(ctx context.Context) ([]ImageDescription, error) {
	reply, err := c.Send(ctx, XPCRouteImageList, nil)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	data, ok := reply.Data(XPCKeyImageDescriptions)
	if !ok {
		return []ImageDescription{}, nil
	}
	var images []ImageDescription
	if err := decodeJSONData(data, &images); err != nil {
		return nil, fmt.Errorf("decode images: %w", err)
	}
	return images, nil
}

func setStdio(message *Message, stdio [3]*os.File) {
	keys := [3]XPCKey{XPCKeyStdin, XPCKeyStdout, XPCKeyStderr}
	for i, file := range stdio {
		if file != nil {
			message.SetFile(keys[i], file)
		}
	}
}
