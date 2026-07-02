package xpc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

type fakeSender struct {
	requests []*Message
	contexts []context.Context
	handler  func(*Message) (*Message, error)
}

func (s *fakeSender) Send(ctx context.Context, message *Message) (*Message, error) {
	s.requests = append(s.requests, message)
	s.contexts = append(s.contexts, ctx)
	if s.handler == nil {
		return newEmptyMessage(), nil
	}
	return s.handler(message)
}

func (s *fakeSender) Close() error { return nil }

func newFakeClient(t *testing.T, sender *fakeSender) *Client {
	t.Helper()
	client, err := NewClient(WithSender(sender))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestListContainersSendsFiltersAndDecodesResponse(t *testing.T) {
	sender := &fakeSender{handler: func(request *Message) (*Message, error) {
		if request.Route() != XPCRouteContainerList {
			t.Fatalf("route = %q", request.Route())
		}
		var filters ContainerListFilters
		if err := request.DecodeJSON(XPCKeyListFilters, &filters); err != nil {
			t.Fatal(err)
		}
		if len(filters.IDs) != 1 || filters.IDs[0] != "ctr" {
			t.Fatalf("filters = %#v", filters)
		}
		reply := newEmptyMessage()
		mustSetJSON(t, reply, XPCKeyContainers, []ContainerSnapshot{{
			Configuration: ContainerConfiguration{ID: "ctr"},
			Status:        RuntimeStatusRunning,
		}})
		return reply, nil
	}}
	client := newFakeClient(t, sender)

	containers, err := client.ListContainers(context.Background(), ContainerListFilters{IDs: []string{"ctr"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 || containers[0].ID() != "ctr" {
		t.Fatalf("containers = %#v", containers)
	}
}

func TestListMethodsReturnEmptySlicesWhenResponseDataMissing(t *testing.T) {
	client := newFakeClient(t, &fakeSender{})

	containers, err := client.ListContainers(context.Background(), ContainerListFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if containers == nil || len(containers) != 0 {
		t.Fatalf("containers = %#v", containers)
	}
	networks, err := client.ListNetworks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if networks == nil || len(networks) != 0 {
		t.Fatalf("networks = %#v", networks)
	}
	volumes, err := client.ListVolumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if volumes == nil || len(volumes) != 0 {
		t.Fatalf("volumes = %#v", volumes)
	}
}

func TestSendAppliesDefaultTimeout(t *testing.T) {
	sender := &fakeSender{}
	client := newFakeClient(t, sender)

	if _, err := client.Send(context.Background(), XPCRoutePing, nil); err != nil {
		t.Fatal(err)
	}
	if len(sender.contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(sender.contexts))
	}
	if _, ok := sender.contexts[0].Deadline(); !ok {
		t.Fatal("Send did not apply default timeout")
	}
}

func TestWaitProcessDoesNotApplyDefaultTimeout(t *testing.T) {
	sender := &fakeSender{handler: func(_ *Message) (*Message, error) {
		reply := newEmptyMessage()
		reply.SetInt64(XPCKeyExitCode, 0)
		return reply, nil
	}}
	client := newFakeClient(t, sender)

	if _, err := client.WaitProcess(context.Background(), "ctr", "proc"); err != nil {
		t.Fatal(err)
	}
	if len(sender.contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(sender.contexts))
	}
	if deadline, ok := sender.contexts[0].Deadline(); ok {
		t.Fatalf("WaitProcess applied default timeout deadline %s", deadline)
	}
}

func TestWaitProcessPreservesCallerDeadline(t *testing.T) {
	sender := &fakeSender{handler: func(_ *Message) (*Message, error) {
		reply := newEmptyMessage()
		reply.SetInt64(XPCKeyExitCode, 0)
		return reply, nil
	}}
	client := newFakeClient(t, sender)
	wantDeadline := time.Now().Add(time.Hour).Round(0)
	ctx, cancel := context.WithDeadline(context.Background(), wantDeadline)
	defer cancel()

	if _, err := client.WaitProcess(ctx, "ctr", "proc"); err != nil {
		t.Fatal(err)
	}
	gotDeadline, ok := sender.contexts[0].Deadline()
	if !ok {
		t.Fatal("WaitProcess dropped caller deadline")
	}
	if !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("deadline = %s, want %s", gotDeadline, wantDeadline)
	}
}

func TestContainerScalarRoutes(t *testing.T) {
	tests := []struct {
		name  string
		call  func(*Client) error
		route XPCRoute
		check func(*testing.T, *Message)
		reply func(*Message)
	}{
		{
			name: "stop",
			call: func(c *Client) error {
				return c.StopContainer(context.Background(), "ctr", ContainerStopOptions{})
			},
			route: XPCRouteContainerStop,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
				var opts ContainerStopOptions
				if err := request.DecodeJSON(XPCKeyStopOptions, &opts); err != nil {
					t.Fatal(err)
				}
				if opts.TimeoutInSeconds != 5 {
					t.Fatalf("stop options = %#v", opts)
				}
			},
		},
		{
			name: "delete",
			call: func(c *Client) error {
				return c.DeleteContainer(context.Background(), "ctr", true)
			},
			route: XPCRouteContainerDelete,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
				if !request.Bool(XPCKeyForceDelete) {
					t.Fatal("forceDelete not set")
				}
			},
		},
		{
			name: "stats",
			call: func(c *Client) error {
				stats, err := c.ContainerStats(context.Background(), "ctr")
				if err != nil {
					return err
				}
				if stats.ID != "ctr" {
					return errors.New("wrong stats id")
				}
				return nil
			},
			route: XPCRouteContainerStats,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
			},
			reply: func(reply *Message) {
				mustSetJSON(t, reply, XPCKeyStatistics, ContainerStats{ID: "ctr"})
			},
		},
		{
			name: "disk usage",
			call: func(c *Client) error {
				size, err := c.ContainerDiskUsage(context.Background(), "ctr")
				if err != nil {
					return err
				}
				if size != 42 {
					return errors.New("wrong size")
				}
				return nil
			},
			route: XPCRouteContainerDiskUsage,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
			},
			reply: func(reply *Message) {
				reply.SetUint64(XPCKeyContainerSize, 42)
			},
		},
		{
			name: "start process",
			call: func(c *Client) error {
				return c.StartProcess(context.Background(), "ctr", "proc")
			},
			route: XPCRouteContainerStartProcess,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
				assertStringKey(t, request, XPCKeyProcessIdentifier, "proc")
			},
		},
		{
			name: "resize process",
			call: func(c *Client) error {
				return c.ResizeProcess(context.Background(), "ctr", "proc", 120, 40)
			},
			route: XPCRouteContainerResize,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
				assertStringKey(t, request, XPCKeyProcessIdentifier, "proc")
				if request.Uint64(XPCKeyWidth) != 120 || request.Uint64(XPCKeyHeight) != 40 {
					t.Fatalf("size = %dx%d", request.Uint64(XPCKeyWidth), request.Uint64(XPCKeyHeight))
				}
			},
		},
		{
			name: "kill process",
			call: func(c *Client) error {
				return c.KillProcess(context.Background(), "ctr", "proc", 2)
			},
			route: XPCRouteContainerKill,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
				assertStringKey(t, request, XPCKeyProcessIdentifier, "proc")
				if request.Int64(XPCKeySignal) != 2 {
					t.Fatalf("signal = %d", request.Int64(XPCKeySignal))
				}
			},
		},
		{
			name: "wait process",
			call: func(c *Client) error {
				code, err := c.WaitProcess(context.Background(), "ctr", "proc")
				if err != nil {
					return err
				}
				if code != 7 {
					return errors.New("wrong exit code")
				}
				return nil
			},
			route: XPCRouteContainerWait,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
				assertStringKey(t, request, XPCKeyProcessIdentifier, "proc")
			},
			reply: func(reply *Message) {
				reply.SetInt64(XPCKeyExitCode, 7)
			},
		},
		{
			name: "export",
			call: func(c *Client) error {
				return c.ExportContainer(context.Background(), "ctr", "/tmp/ctr.tar")
			},
			route: XPCRouteContainerExport,
			check: func(t *testing.T, request *Message) {
				assertStringKey(t, request, XPCKeyID, "ctr")
				assertStringKey(t, request, XPCKeyArchive, "/tmp/ctr.tar")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{handler: func(request *Message) (*Message, error) {
				if request.Route() != tt.route {
					t.Fatalf("route = %q, want %q", request.Route(), tt.route)
				}
				tt.check(t, request)
				reply := newEmptyMessage()
				if tt.reply != nil {
					tt.reply(reply)
				}
				return reply, nil
			}}
			client := newFakeClient(t, sender)
			if err := tt.call(client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestContainerCreateBootstrapAndProcessCreateRoutes(t *testing.T) {
	sender := &fakeSender{handler: func(request *Message) (*Message, error) {
		switch request.Route() {
		case XPCRouteContainerCreate:
			assertProcessArraysAreEmptyArrays(t, request, XPCKeyContainerConfig)
			var cfg ContainerConfiguration
			if err := request.DecodeJSON(XPCKeyContainerConfig, &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.ID != "ctr" {
				t.Fatalf("create cfg = %#v", cfg)
			}
			var kernel Kernel
			if err := request.DecodeJSON(XPCKeyKernel, &kernel); err != nil {
				t.Fatal(err)
			}
			if kernel.Platform.Architecture != "arm64" {
				t.Fatalf("kernel = %#v", kernel)
			}
			var opts ContainerCreateOptions
			if err := request.DecodeJSON(XPCKeyContainerOptions, &opts); err != nil {
				t.Fatal(err)
			}
			if opts.AutoRemove {
				t.Fatal("autoRemove unexpectedly set")
			}
			assertStringKey(t, request, XPCKeyInitImage, "init")
		case XPCRouteContainerBootstrap:
			assertStringKey(t, request, XPCKeyID, "ctr")
			var env map[string]string
			if err := request.DecodeJSON(XPCKeyDynamicEnv, &env); err != nil {
				t.Fatal(err)
			}
			if env["A"] != "B" {
				t.Fatalf("dynamic env = %#v", env)
			}
		case XPCRouteContainerCreateProcess:
			assertStringKey(t, request, XPCKeyID, "ctr")
			assertStringKey(t, request, XPCKeyProcessIdentifier, "proc")
			assertProcessArraysAreEmptyArrays(t, request, XPCKeyProcessConfig)
			var cfg ProcessConfiguration
			if err := request.DecodeJSON(XPCKeyProcessConfig, &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.Executable != "sh" {
				t.Fatalf("process cfg = %#v", cfg)
			}
		default:
			t.Fatalf("unexpected route %q", request.Route())
		}
		return newEmptyMessage(), nil
	}}
	client := newFakeClient(t, sender)
	cfg := ContainerConfiguration{
		ID: "ctr",
		Image: ImageDescription{
			Reference:  "image",
			Descriptor: Descriptor{Digest: "sha256:abc", MediaType: "application/vnd.oci.image.index.v1+json"},
		},
		InitProcess: ProcessConfiguration{Executable: "sh"},
	}
	kernel := NewKernel("/kernel", SystemPlatform{OS: "linux", Architecture: "arm64"})
	if err := client.CreateContainer(context.Background(), cfg, ContainerCreateOptions{}, kernel, "init", nil); err != nil {
		t.Fatal(err)
	}
	if err := client.BootstrapContainer(context.Background(), "ctr", [3]*os.File{}, map[string]string{"A": "B"}); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateProcess(context.Background(), "ctr", "proc", ProcessConfiguration{Executable: "sh"}, [3]*os.File{}); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkVolumeAndSystemRoutes(t *testing.T) {
	sender := &fakeSender{handler: func(request *Message) (*Message, error) {
		reply := newEmptyMessage()
		switch request.Route() {
		case XPCRouteNetworkCreate:
			assertStringKey(t, request, XPCKeyNetworkID, "net")
			var cfg NetworkConfiguration
			if err := request.DecodeJSON(XPCKeyNetworkConfig, &cfg); err != nil {
				t.Fatal(err)
			}
			mustSetJSON(t, reply, XPCKeyNetworkResource, NetworkResource{Configuration: cfg})
		case XPCRouteVolumeCreate:
			assertStringKey(t, request, XPCKeyVolumeName, "vol")
			assertStringKey(t, request, XPCKeyVolumeDriver, "local")
			mustSetJSON(t, reply, XPCKeyVolume, VolumeConfiguration{Name: "vol"})
		case XPCRouteSystemDiskUsage:
			mustSetJSON(t, reply, XPCKeyDiskUsageStats, DiskUsageStats{Containers: ResourceUsage{Total: 1}})
		case XPCRoutePing:
			reply.SetString(XPCKeyAppRoot, "file:///app")
			reply.SetString(XPCKeyInstallRoot, "file:///install")
			reply.SetString(XPCKeyAPIServerVersion, "v")
			reply.SetString(XPCKeyAPIServerCommit, "c")
			reply.SetString(XPCKeyAPIServerBuild, "debug")
			reply.SetString(XPCKeyAPIServerAppName, "container-apiserver")
		default:
			t.Fatalf("unexpected route %q", request.Route())
		}
		return reply, nil
	}}
	client := newFakeClient(t, sender)

	if _, err := client.CreateNetwork(context.Background(), NetworkConfiguration{Name: "net"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateVolume(context.Background(), "vol", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	stats, err := client.SystemDiskUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Containers.Total != 1 {
		t.Fatalf("disk usage = %#v", stats)
	}
	health, err := client.Ping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.APIServerAppName != "container-apiserver" {
		t.Fatalf("health = %#v", health)
	}
}

func TestProtocolErrorIsReturned(t *testing.T) {
	sender := &fakeSender{handler: func(_ *Message) (*Message, error) {
		reply := newEmptyMessage()
		data, err := json.Marshal(XPCError{Code: "invalid_argument", Message: "bad request"})
		if err != nil {
			t.Fatal(err)
		}
		reply.SetDataRaw(xpcErrorKey, data)
		return reply, nil
	}}
	client := newFakeClient(t, sender)

	_, err := client.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var xpcErr XPCError
	if !errors.As(err, &xpcErr) {
		t.Fatalf("error = %T %[1]v", err)
	}
	if xpcErr.Code != "invalid_argument" {
		t.Fatalf("xpc error = %#v", xpcErr)
	}
}

func mustSetJSON(t *testing.T, message *Message, key XPCKey, value any) {
	t.Helper()
	if err := message.SetJSON(key, value); err != nil {
		t.Fatal(err)
	}
}

func assertStringKey(t *testing.T, message *Message, key XPCKey, want string) {
	t.Helper()
	got, ok := message.String(key)
	if !ok {
		t.Fatalf("missing string key %s", key)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func assertProcessArraysAreEmptyArrays(t *testing.T, message *Message, key XPCKey) {
	t.Helper()
	data, ok := message.Data(key)
	if !ok {
		t.Fatalf("missing data key %s", key)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	process := root
	if nested, ok := root["initProcess"].(map[string]any); ok {
		process = nested
	}
	for _, name := range []string{"arguments", "environment", "supplementalGroups", "rlimits"} {
		if _, ok := process[name].([]any); !ok {
			t.Fatalf("%s encoded as %T (%[2]v), want array in %s", name, process[name], string(key))
		}
	}
}
