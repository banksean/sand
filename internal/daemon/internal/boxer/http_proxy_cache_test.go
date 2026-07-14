package boxer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestHTTPProxyCacheEnsureCreatesAndStartsMissingContainer(t *testing.T) {
	var created, started, ready bool
	var mkdirPath string
	inspectCalls := 0

	b := &Boxer{
		appRoot: "/tmp/sand-app",
		FileOps: &hostops.MockFileOps{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				mkdirPath = path
				return nil
			},
		},
		ImageService: imageServiceWithHTTPProxyCacheImage(),
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
				inspectCalls++
				if containerID != HTTPProxyCacheContainerName {
					t.Fatalf("inspect container = %q", containerID)
				}
				if inspectCalls == 1 {
					return nil, errors.New("container not found")
				}
				return []sandtypes.Container{httpProxyCacheContainer("stopped")}, nil
			},
			CreateFunc: func(ctx context.Context, opts *hostops.CreateContainer, image string, args []string) (string, error) {
				created = true
				if image != HTTPProxyCacheImage {
					t.Fatalf("create image = %q", image)
				}
				if opts.Name != HTTPProxyCacheContainerName {
					t.Fatalf("create name = %q", opts.Name)
				}
				if opts.Publish != "127.0.0.1:3128:3128/tcp" {
					t.Fatalf("publish = %q", opts.Publish)
				}
				if opts.Label[httpProxyCacheServiceLabel] != httpProxyCacheServiceValue {
					t.Fatalf("service label = %v", opts.Label)
				}
				if len(opts.Volume) != 1 || !strings.Contains(opts.Volume[0], "target=/var/spool/squid") {
					t.Fatalf("volume = %#v", opts.Volume)
				}
				return HTTPProxyCacheContainerName, nil
			},
			StartFunc: func(ctx context.Context, opts *hostops.StartContainer, containerID string) (string, error) {
				started = true
				if containerID != HTTPProxyCacheContainerName {
					t.Fatalf("start container = %q", containerID)
				}
				return containerID, nil
			},
		},
	}
	service := b.HTTPProxyCacheService()
	service.readinessCheck = func(ctx context.Context) error {
		ready = true
		return nil
	}

	if err := service.Ensure(context.Background(), "test.local", nil); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !created || !started || !ready {
		t.Fatalf("created=%v started=%v ready=%v", created, started, ready)
	}
	if mkdirPath != "/tmp/sand-app/caches/http-proxy" {
		t.Fatalf("mkdir path = %q", mkdirPath)
	}
}

func TestHTTPProxyCacheEnsureRejectsNameCollision(t *testing.T) {
	b := &Boxer{
		appRoot:      "/tmp/sand-app",
		FileOps:      &hostops.MockFileOps{},
		ImageService: imageServiceWithHTTPProxyCacheImage(),
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
				return []sandtypes.Container{{Status: sandtypes.ContainerStatus{State: "running"}}}, nil
			},
		},
	}
	service := b.HTTPProxyCacheService()
	service.readinessCheck = func(ctx context.Context) error { return nil }

	err := service.Ensure(context.Background(), "test.local", nil)
	if err == nil || !strings.Contains(err.Error(), "not managed by sand") {
		t.Fatalf("Ensure error = %v, want name collision", err)
	}
}

func TestHTTPProxyCacheEnsureAdoptsExpectedImageWithoutLabels(t *testing.T) {
	var started bool
	b := &Boxer{
		appRoot:      "/tmp/sand-app",
		FileOps:      &hostops.MockFileOps{},
		ImageService: imageServiceWithHTTPProxyCacheImage(),
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
				return []sandtypes.Container{{
					Status: sandtypes.ContainerStatus{State: "stopped"},
					Configuration: sandtypes.ContainerConfig{
						Image: sandtypes.Image{Reference: "docker.io/" + HTTPProxyCacheImage},
					},
				}}, nil
			},
			StartFunc: func(ctx context.Context, opts *hostops.StartContainer, containerID string) (string, error) {
				started = true
				return containerID, nil
			},
		},
	}
	service := b.HTTPProxyCacheService()
	service.readinessCheck = func(ctx context.Context) error { return nil }

	if err := service.Ensure(context.Background(), "test.local", nil); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !started {
		t.Fatal("expected unlabeled stopped squid container to be started")
	}
}

func TestHTTPProxyCacheClearDeletesContainerAndCacheDir(t *testing.T) {
	var deleted, removed bool
	b := &Boxer{
		appRoot: "/tmp/sand-app",
		FileOps: &hostops.MockFileOps{
			RemoveAllFunc: func(path string) error {
				removed = true
				if path != "/tmp/sand-app/caches/http-proxy" {
					t.Fatalf("remove path = %q", path)
				}
				return nil
			},
		},
		ContainerService: &hostops.MockContainerOps{
			InspectFunc: func(ctx context.Context, containerID string) ([]sandtypes.Container, error) {
				return []sandtypes.Container{httpProxyCacheContainer("running")}, nil
			},
			DeleteFunc: func(ctx context.Context, opts *hostops.DeleteContainer, containerID string) (string, error) {
				deleted = true
				if !opts.Force {
					t.Fatal("delete force = false")
				}
				if containerID != HTTPProxyCacheContainerName {
					t.Fatalf("delete container = %q", containerID)
				}
				return containerID, nil
			},
		},
	}

	if err := b.HTTPProxyCacheService().Clear(context.Background()); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if !deleted || !removed {
		t.Fatalf("deleted=%v removed=%v", deleted, removed)
	}
}

func imageServiceWithHTTPProxyCacheImage() *mockImageOps {
	return &mockImageOps{
		listFunc: func(ctx context.Context) ([]sandtypes.ImageEntry, error) {
			return []sandtypes.ImageEntry{{
				Configuration: sandtypes.ImageConfiguration{Name: HTTPProxyCacheImage},
			}}, nil
		},
	}
}

func httpProxyCacheContainer(state string) sandtypes.Container {
	return sandtypes.Container{
		Status: sandtypes.ContainerStatus{State: state},
		Configuration: sandtypes.ContainerConfig{
			Labels: map[string]any{
				httpProxyCacheServiceLabel: httpProxyCacheServiceValue,
				httpProxyCacheVersionLabel: httpProxyCacheVersion,
			},
			Image: sandtypes.Image{Reference: HTTPProxyCacheImage},
		},
	}
}
