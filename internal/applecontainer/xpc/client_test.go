package xpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeSender struct {
	requests []*Message
	handler  func(*Message) (*Message, error)
}

func (s *fakeSender) Send(_ context.Context, message *Message) (*Message, error) {
	s.requests = append(s.requests, message)
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
