package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/sandtypes"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func TestGRPCSB(t *testing.T) {
	t.Run("stream", func(t *testing.T) {
		appDir := t.TempDir()
		srv := startTestGRPCDaemon(t, appDir, &testGRPCDaemonService{
			CreateSandboxFunc: func(req *daemonpb.CreateSandboxRequest, stream daemonpb.DaemonService_CreateSandboxServer) error {
				if req.GetId() != "test-box" {
					t.Fatalf("CreateSandbox request ID = %q, want test-box", req.GetId())
				}
				if err := stream.Send(&daemonpb.CreateSandboxResponse{
					Event: &daemonpb.CreateSandboxResponse_Progress{Progress: "step 1\n"},
				}); err != nil {
					return err
				}
				if err := stream.Send(&daemonpb.CreateSandboxResponse{
					Event: &daemonpb.CreateSandboxResponse_Progress{Progress: "step 2\n"},
				}); err != nil {
					return err
				}
				boxJSON, err := json.Marshal(testSandboxBox)
				if err != nil {
					return err
				}
				return stream.Send(&daemonpb.CreateSandboxResponse{
					Event: &daemonpb.CreateSandboxResponse_BoxJson{BoxJson: boxJSON},
				})
			},
		})
		defer srv.Stop()

		client, err := NewUnixSocketGRPCClient(context.Background(), appDir)
		if err != nil {
			t.Fatalf("NewUnixSocketGRPCClient() error = %v", err)
		}
		defer client.Close()

		var progress bytes.Buffer
		box, err := client.CreateSandbox(context.Background(), CreateSandboxOpts{ID: "test-box"}, &progress)
		if err != nil {
			t.Fatalf("CreateSandbox() error = %v", err)
		}
		if box == nil {
			t.Fatal("CreateSandbox() returned nil box")
		}
		if box.ID != testSandboxBox.ID {
			t.Fatalf("CreateSandbox() box ID = %q, want %q", box.ID, testSandboxBox.ID)
		}
		if got := progress.String(); got != "step 1\nstep 2\n" {
			t.Fatalf("CreateSandbox() progress = %q, want %q", got, "step 1\nstep 2\n")
		}
	})

	t.Run("stream err", func(t *testing.T) {
		appDir := t.TempDir()
		srv := startTestGRPCDaemon(t, appDir, &testGRPCDaemonService{
			CreateSandboxFunc: func(req *daemonpb.CreateSandboxRequest, stream daemonpb.DaemonService_CreateSandboxServer) error {
				if err := stream.Send(&daemonpb.CreateSandboxResponse{
					Event: &daemonpb.CreateSandboxResponse_Progress{Progress: "starting\n"},
				}); err != nil {
					return err
				}
				return stream.Send(&daemonpb.CreateSandboxResponse{
					Event: &daemonpb.CreateSandboxResponse_Error{Error: "bootstrap failed"},
				})
			},
		})
		defer srv.Stop()

		client, err := NewUnixSocketGRPCClient(context.Background(), appDir)
		if err != nil {
			t.Fatalf("NewUnixSocketGRPCClient() error = %v", err)
		}
		defer client.Close()

		var progress bytes.Buffer
		box, err := client.CreateSandbox(context.Background(), CreateSandboxOpts{ID: "test-box"}, &progress)
		if err == nil {
			t.Fatal("CreateSandbox() error = nil, want error")
		}
		if box != nil {
			t.Fatalf("CreateSandbox() box = %#v, want nil", box)
		}
		if !strings.Contains(err.Error(), "bootstrap failed") {
			t.Fatalf("CreateSandbox() error = %q, want streamed error", err)
		}
		if got := progress.String(); got != "starting\n" {
			t.Fatalf("CreateSandbox() progress = %q, want %q", got, "starting\n")
		}
	})
}

func TestGRPCImage(t *testing.T) {
	t.Run("stream", func(t *testing.T) {
		appDir := t.TempDir()
		srv := startTestGRPCDaemon(t, appDir, &testGRPCDaemonService{
			EnsureImageFunc: func(req *daemonpb.EnsureImageRequest, stream daemonpb.DaemonService_EnsureImageServer) error {
				if req.GetImageName() != "test-image:latest" {
					t.Fatalf("EnsureImage request image = %q, want test-image:latest", req.GetImageName())
				}
				if err := stream.Send(&daemonpb.EnsureImageResponse{
					Event: &daemonpb.EnsureImageResponse_Progress{Progress: []byte("pulling\r")},
				}); err != nil {
					return err
				}
				if err := stream.Send(&daemonpb.EnsureImageResponse{
					Event: &daemonpb.EnsureImageResponse_Progress{Progress: []byte("done\n")},
				}); err != nil {
					return err
				}
				return stream.Send(&daemonpb.EnsureImageResponse{
					Event: &daemonpb.EnsureImageResponse_Ok{Ok: true},
				})
			},
		})
		defer srv.Stop()

		client, err := NewUnixSocketGRPCClient(context.Background(), appDir)
		if err != nil {
			t.Fatalf("NewUnixSocketGRPCClient() error = %v", err)
		}
		defer client.Close()

		var progress bytes.Buffer
		if err := client.EnsureImage(context.Background(), "test-image:latest", &progress); err != nil {
			t.Fatalf("EnsureImage() error = %v", err)
		}
		if got := progress.String(); got != "pulling\rdone\n" {
			t.Fatalf("EnsureImage() progress = %q, want %q", got, "pulling\rdone\n")
		}
	})

	t.Run("stream err", func(t *testing.T) {
		appDir := t.TempDir()
		srv := startTestGRPCDaemon(t, appDir, &testGRPCDaemonService{
			EnsureImageFunc: func(req *daemonpb.EnsureImageRequest, stream daemonpb.DaemonService_EnsureImageServer) error {
				if err := stream.Send(&daemonpb.EnsureImageResponse{
					Event: &daemonpb.EnsureImageResponse_Progress{Progress: []byte("pulling\n")},
				}); err != nil {
					return err
				}
				return stream.Send(&daemonpb.EnsureImageResponse{
					Event: &daemonpb.EnsureImageResponse_Error{Error: "pull failed"},
				})
			},
		})
		defer srv.Stop()

		client, err := NewUnixSocketGRPCClient(context.Background(), appDir)
		if err != nil {
			t.Fatalf("NewUnixSocketGRPCClient() error = %v", err)
		}
		defer client.Close()

		var progress bytes.Buffer
		err = client.EnsureImage(context.Background(), "test-image:latest", &progress)
		if err == nil {
			t.Fatal("EnsureImage() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "pull failed") {
			t.Fatalf("EnsureImage() error = %q, want streamed error", err)
		}
		if got := progress.String(); got != "pulling\n" {
			t.Fatalf("EnsureImage() progress = %q, want %q", got, "pulling\n")
		}
	})
}

func TestGRPCStreamingClientSpansEnd(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() {
		_ = tracerProvider.Shutdown(context.Background())
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
	})
	appDir, err := os.MkdirTemp("", "t*")
	if err != nil {
		t.Error(err)
		return
	}
	defer os.RemoveAll(appDir)

	srv := startTestGRPCDaemon(t, appDir, &testGRPCDaemonService{
		CreateSandboxFunc: func(req *daemonpb.CreateSandboxRequest, stream daemonpb.DaemonService_CreateSandboxServer) error {
			boxJSON, err := json.Marshal(testSandboxBox)
			if err != nil {
				return err
			}
			return stream.Send(&daemonpb.CreateSandboxResponse{
				Event: &daemonpb.CreateSandboxResponse_BoxJson{BoxJson: boxJSON},
			})
		},
		EnsureImageFunc: func(req *daemonpb.EnsureImageRequest, stream daemonpb.DaemonService_EnsureImageServer) error {
			return stream.Send(&daemonpb.EnsureImageResponse{
				Event: &daemonpb.EnsureImageResponse_Ok{Ok: true},
			})
		},
	})
	defer srv.Stop()

	client, err := NewUnixSocketGRPCClient(context.Background(), appDir)
	if err != nil {
		t.Fatalf("NewUnixSocketGRPCClient() error = %v", err)
	}
	defer client.Close()

	if _, err := client.CreateSandbox(context.Background(), CreateSandboxOpts{ID: "test-box"}, nil); err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	if err := client.EnsureImage(context.Background(), "test-image:latest", nil); err != nil {
		t.Fatalf("EnsureImage() error = %v", err)
	}

	assertEndedSpan(t, spanRecorder, "sand.daemon.v1.DaemonService/CreateSandbox")
	assertEndedSpan(t, spanRecorder, "sand.daemon.v1.DaemonService/EnsureImage")
}

func TestCreateSandboxOptsProtoRoundTrip(t *testing.T) {
	opts := CreateSandboxOpts{
		Name:           "test-box",
		CloneFromDir:   "/src",
		ProfileName:    "dev",
		ImageName:      "test-image:latest",
		EnvFile:        "/src/.env",
		Agent:          "codex",
		SSHAgent:       true,
		Username:       "dev",
		Uid:            "501",
		AllowedDomains: []string{"example.com", "api.example.com"},
		Volumes:        []string{"/host:/container"},
		SharedCaches:   sandtypes.SharedCacheConfig{Mise: true, APK: true},
		CPUs:           4,
		Memory:         8192,
	}

	got := createSandboxOptsFromProto(createSandboxOptsToProto(opts))
	if got.Name != opts.Name ||
		got.CloneFromDir != opts.CloneFromDir ||
		got.ProfileName != opts.ProfileName ||
		got.ImageName != opts.ImageName ||
		got.EnvFile != opts.EnvFile ||
		got.Agent != opts.Agent ||
		got.SSHAgent != opts.SSHAgent ||
		got.Username != opts.Username ||
		got.Uid != opts.Uid ||
		got.SharedCaches != opts.SharedCaches ||
		got.CPUs != opts.CPUs ||
		got.Memory != opts.Memory {
		t.Fatalf("round trip opts = %+v, want %+v", got, opts)
	}
	if strings.Join(got.AllowedDomains, ",") != strings.Join(opts.AllowedDomains, ",") {
		t.Fatalf("round trip allowed domains = %+v, want %+v", got.AllowedDomains, opts.AllowedDomains)
	}
	if strings.Join(got.Volumes, ",") != strings.Join(opts.Volumes, ",") {
		t.Fatalf("round trip volumes = %+v, want %+v", got.Volumes, opts.Volumes)
	}
}

func assertEndedSpan(t *testing.T, spanRecorder *tracetest.SpanRecorder, name string) {
	t.Helper()
	for _, span := range spanRecorder.Ended() {
		if span.Name() == name {
			return
		}
	}
	t.Fatalf("ended spans do not include %q: %v", name, spanNames(spanRecorder.Ended()))
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}

func TestGRPCUnary(t *testing.T) {
	appDir := t.TempDir()
	service := &testGRPCDaemonService{
		LogSandboxFunc: func(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.LogSandboxResponse, error) {
			if req.GetId() != "test-box" {
				t.Fatalf("LogSandbox request ID = %q, want test-box", req.GetId())
			}
			return &daemonpb.LogSandboxResponse{Data: []byte("log line\n")}, nil
		},
		ListSandboxesFunc: func(ctx context.Context, req *daemonpb.ListSandboxesRequest) (*daemonpb.ListSandboxesResponse, error) {
			boxesJSON, err := json.Marshal([]sandtypes.Box{testSandboxBox})
			if err != nil {
				return nil, err
			}
			return &daemonpb.ListSandboxesResponse{BoxesJson: boxesJSON}, nil
		},
		GetSandboxFunc: func(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.GetSandboxResponse, error) {
			if req.GetId() != "test-box" {
				t.Fatalf("GetSandbox request ID = %q, want test-box", req.GetId())
			}
			boxJSON, err := json.Marshal(testSandboxBox)
			if err != nil {
				return nil, err
			}
			return &daemonpb.GetSandboxResponse{BoxJson: boxJSON}, nil
		},
		StartSandboxFunc: func(ctx context.Context, req *daemonpb.StartSandboxRequest) (*daemonpb.StatusResponse, error) {
			if req.GetId() != "test-box" {
				t.Fatalf("StartSandbox request ID = %q, want test-box", req.GetId())
			}
			if !req.GetSshAgent() {
				t.Fatal("StartSandbox request SSHAgent = false, want true")
			}
			return &daemonpb.StatusResponse{Status: "ok"}, nil
		},
		ResolveAgentLaunchEnvFunc: func(ctx context.Context, req *daemonpb.ResolveAgentLaunchEnvRequest) (*daemonpb.ResolveAgentLaunchEnvResponse, error) {
			if req.GetAgent() != "codex" {
				t.Fatalf("ResolveAgentLaunchEnv request agent = %q, want codex", req.GetAgent())
			}
			if req.GetEnvFile() != "/tmp/test.env" {
				t.Fatalf("ResolveAgentLaunchEnv request envFile = %q, want /tmp/test.env", req.GetEnvFile())
			}
			if req.GetProfileName() != "dev" {
				t.Fatalf("ResolveAgentLaunchEnv request profile = %q, want dev", req.GetProfileName())
			}
			if len(req.GetProfileEnv().GetFiles()) != 1 || req.GetProfileEnv().GetFiles()[0].GetScope() != "auth" {
				t.Fatalf("ResolveAgentLaunchEnv profile env = %+v, want one auth file", req.GetProfileEnv())
			}
			if !req.GetProfileEnvConfigured() {
				t.Fatal("ResolveAgentLaunchEnv profile env configured = false, want true")
			}
			return &daemonpb.ResolveAgentLaunchEnvResponse{Env: map[string]string{"OPENAI_API_KEY": "sk-test"}}, nil
		},
		StatsFunc: func(ctx context.Context, req *daemonpb.StatsRequest) (*daemonpb.StatsResponse, error) {
			if strings.Join(req.GetIds(), ",") != "test-box,other-box" {
				t.Fatalf("Stats request IDs = %+v", req.GetIds())
			}
			statsJSON, err := json.Marshal([]types.ContainerStats{{ID: "test-box"}})
			if err != nil {
				return nil, err
			}
			return &daemonpb.StatsResponse{StatsJson: statsJSON}, nil
		},
		RemoveSandboxFunc: func(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
			if req.GetId() != "test-box" {
				t.Fatalf("RemoveSandbox request ID = %q, want test-box", req.GetId())
			}
			return &daemonpb.StatusResponse{Status: "ok"}, nil
		},
		StopSandboxFunc: func(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
			if req.GetId() != "test-box" {
				t.Fatalf("StopSandbox request ID = %q, want test-box", req.GetId())
			}
			return &daemonpb.StatusResponse{Status: "ok"}, nil
		},
		ExportImageFunc: func(ctx context.Context, req *daemonpb.ExportImageRequest) (*daemonpb.StatusResponse, error) {
			if req.GetId() != "test-box" {
				t.Fatalf("ExportImage request ID = %q, want test-box", req.GetId())
			}
			if req.GetDestinationPath() != "archive.tar" {
				t.Fatalf("ExportImage destination = %q, want archive.tar", req.GetDestinationPath())
			}
			return &daemonpb.StatusResponse{Status: "ok"}, nil
		},
		VSCFunc: func(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
			if req.GetId() != "test-box" {
				t.Fatalf("VSC request ID = %q, want test-box", req.GetId())
			}
			return &daemonpb.StatusResponse{Status: "ok"}, nil
		},
	}
	srv := startTestGRPCDaemon(t, appDir, service)
	defer srv.Stop()

	client, err := NewUnixSocketGRPCClient(context.Background(), appDir)
	if err != nil {
		t.Fatalf("NewUnixSocketGRPCClient() error = %v", err)
	}
	defer client.Close()

	var logs bytes.Buffer
	if err := client.LogSandbox(context.Background(), "test-box", &logs); err != nil {
		t.Fatalf("LogSandbox() error = %v", err)
	}
	if got := logs.String(); got != "log line\n" {
		t.Fatalf("LogSandbox() wrote %q, want log line", got)
	}

	boxes, err := client.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes() error = %v", err)
	}
	if len(boxes) != 1 || boxes[0].ID != "test-box" {
		t.Fatalf("ListSandboxes() = %+v, want test-box", boxes)
	}

	box, err := client.GetSandbox(context.Background(), "test-box")
	if err != nil {
		t.Fatalf("GetSandbox() error = %v", err)
	}
	if box.ID != "test-box" {
		t.Fatalf("GetSandbox() ID = %q, want test-box", box.ID)
	}

	if err := client.StartSandbox(context.Background(), StartSandboxOpts{ID: "test-box", SSHAgent: true}); err != nil {
		t.Fatalf("StartSandbox() error = %v", err)
	}
	env, err := client.ResolveAgentLaunchEnv(context.Background(), ResolveAgentLaunchEnvOpts{
		Agent:       "codex",
		EnvFile:     "/tmp/test.env",
		ProfileName: "dev",
		ProfileEnv: sandtypes.EnvPolicy{
			Files: []sandtypes.EnvFileRef{{Path: "/tmp/test.env", Scope: sandtypes.EnvScopeAuth}},
		},
		ProfileEnvConfigured: true,
	})
	if err != nil {
		t.Fatalf("ResolveAgentLaunchEnv() error = %v", err)
	}
	if env["OPENAI_API_KEY"] != "sk-test" {
		t.Fatalf("ResolveAgentLaunchEnv() env = %+v, want OPENAI_API_KEY", env)
	}
	stats, err := client.Stats(context.Background(), "test-box", "other-box")
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if len(stats) != 1 || stats[0].ID != "test-box" {
		t.Fatalf("Stats() = %+v, want test-box", stats)
	}
	if err := client.RemoveSandbox(context.Background(), "test-box"); err != nil {
		t.Fatalf("RemoveSandbox() error = %v", err)
	}
	if err := client.StopSandbox(context.Background(), "test-box"); err != nil {
		t.Fatalf("StopSandbox() error = %v", err)
	}
	if err := client.ExportImage(context.Background(), "test-box", "archive.tar"); err != nil {
		t.Fatalf("ExportImage() error = %v", err)
	}
	if err := client.VSC(context.Background(), "test-box"); err != nil {
		t.Fatalf("VSC() error = %v", err)
	}
}

var testSandboxBox = sandBox()

func sandBox() sandtypes.Box {
	return sandtypes.Box{
		ID:          "test-box",
		ContainerID: "ctr-test-box",
		ImageName:   "test-image:latest",
	}
}

type testGRPCDaemonService struct {
	daemonpb.UnimplementedDaemonServiceServer
	LogSandboxFunc            func(context.Context, *daemonpb.IDRequest) (*daemonpb.LogSandboxResponse, error)
	ListSandboxesFunc         func(context.Context, *daemonpb.ListSandboxesRequest) (*daemonpb.ListSandboxesResponse, error)
	GetSandboxFunc            func(context.Context, *daemonpb.IDRequest) (*daemonpb.GetSandboxResponse, error)
	RemoveSandboxFunc         func(context.Context, *daemonpb.IDRequest) (*daemonpb.StatusResponse, error)
	StopSandboxFunc           func(context.Context, *daemonpb.IDRequest) (*daemonpb.StatusResponse, error)
	StartSandboxFunc          func(context.Context, *daemonpb.StartSandboxRequest) (*daemonpb.StatusResponse, error)
	SyncHostGitMirrorFunc     func(context.Context, *daemonpb.IDRequest) (*daemonpb.SyncHostGitMirrorResponse, error)
	ResolveAgentLaunchEnvFunc func(context.Context, *daemonpb.ResolveAgentLaunchEnvRequest) (*daemonpb.ResolveAgentLaunchEnvResponse, error)
	ExportImageFunc           func(context.Context, *daemonpb.ExportImageRequest) (*daemonpb.StatusResponse, error)
	StatsFunc                 func(context.Context, *daemonpb.StatsRequest) (*daemonpb.StatsResponse, error)
	VSCFunc                   func(context.Context, *daemonpb.IDRequest) (*daemonpb.StatusResponse, error)
	CreateSandboxFunc         func(*daemonpb.CreateSandboxRequest, daemonpb.DaemonService_CreateSandboxServer) error
	EnsureImageFunc           func(*daemonpb.EnsureImageRequest, daemonpb.DaemonService_EnsureImageServer) error
}

func (s *testGRPCDaemonService) LogSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.LogSandboxResponse, error) {
	return s.LogSandboxFunc(ctx, req)
}

func (s *testGRPCDaemonService) ListSandboxes(ctx context.Context, req *daemonpb.ListSandboxesRequest) (*daemonpb.ListSandboxesResponse, error) {
	return s.ListSandboxesFunc(ctx, req)
}

func (s *testGRPCDaemonService) GetSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.GetSandboxResponse, error) {
	return s.GetSandboxFunc(ctx, req)
}

func (s *testGRPCDaemonService) RemoveSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	return s.RemoveSandboxFunc(ctx, req)
}

func (s *testGRPCDaemonService) StopSandbox(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	return s.StopSandboxFunc(ctx, req)
}

func (s *testGRPCDaemonService) StartSandbox(ctx context.Context, req *daemonpb.StartSandboxRequest) (*daemonpb.StatusResponse, error) {
	return s.StartSandboxFunc(ctx, req)
}

func (s *testGRPCDaemonService) SyncHostGitMirror(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.SyncHostGitMirrorResponse, error) {
	return s.SyncHostGitMirrorFunc(ctx, req)
}

func (s *testGRPCDaemonService) ResolveAgentLaunchEnv(ctx context.Context, req *daemonpb.ResolveAgentLaunchEnvRequest) (*daemonpb.ResolveAgentLaunchEnvResponse, error) {
	return s.ResolveAgentLaunchEnvFunc(ctx, req)
}

func (s *testGRPCDaemonService) ExportImage(ctx context.Context, req *daemonpb.ExportImageRequest) (*daemonpb.StatusResponse, error) {
	return s.ExportImageFunc(ctx, req)
}

func (s *testGRPCDaemonService) Stats(ctx context.Context, req *daemonpb.StatsRequest) (*daemonpb.StatsResponse, error) {
	return s.StatsFunc(ctx, req)
}

func (s *testGRPCDaemonService) VSC(ctx context.Context, req *daemonpb.IDRequest) (*daemonpb.StatusResponse, error) {
	return s.VSCFunc(ctx, req)
}

func (s *testGRPCDaemonService) CreateSandbox(req *daemonpb.CreateSandboxRequest, stream daemonpb.DaemonService_CreateSandboxServer) error {
	return s.CreateSandboxFunc(req, stream)
}

func (s *testGRPCDaemonService) EnsureImage(req *daemonpb.EnsureImageRequest, stream daemonpb.DaemonService_EnsureImageServer) error {
	return s.EnsureImageFunc(req, stream)
}

func startTestGRPCDaemon(t *testing.T, appDir string, service daemonpb.DaemonServiceServer) *grpc.Server {
	t.Helper()
	socketPath := filepath.Join(appDir, DefaultGRPCSocketFile)
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove socket: %v", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	server := grpc.NewServer()
	daemonpb.RegisterDaemonServiceServer(server, service)
	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("test gRPC server stopped: %v", err)
		}
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})
	return server
}
