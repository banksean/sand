package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"runtime/debug"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/version"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCClient communicates with sandd over the daemon gRPC Unix socket.
type GRPCClient struct {
	conn   *grpc.ClientConn
	client daemonpb.DaemonServiceClient
}

func NewUnixSocketGRPCClient(ctx context.Context, appBaseDir string) (*GRPCClient, error) {
	_ = ctx
	socketPath := filepath.Join(appBaseDir, DefaultGRPCSocketFile)
	conn, err := grpc.NewClient(
		"passthrough:///sandd-grpc",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		}),
	)
	if err != nil {
		return nil, err
	}
	return &GRPCClient{
		conn:   conn,
		client: daemonpb.NewDaemonServiceClient(conn),
	}, nil
}

func (c *GRPCClient) Close() error {
	return c.conn.Close()
}

func (c *GRPCClient) Ping(ctx context.Context) error {
	_, err := c.client.Ping(ctx, &daemonpb.PingRequest{})
	return err
}

func (c *GRPCClient) Version(ctx context.Context) (version.Info, error) {
	resp, err := c.client.Version(ctx, &daemonpb.VersionRequest{})
	if err != nil {
		return version.Info{}, err
	}
	info := version.Info{
		GitRepo:   resp.GetGitRepo(),
		GitBranch: resp.GetGitBranch(),
		GitCommit: resp.GetGitCommit(),
		BuildTime: resp.GetBuildTime(),
	}
	if len(resp.GetBuildInfoJson()) > 0 {
		var buildInfo debug.BuildInfo
		if err := json.Unmarshal(resp.GetBuildInfoJson(), &buildInfo); err != nil {
			return version.Info{}, fmt.Errorf("decode build info: %w", err)
		}
		info.BuildInfo = &buildInfo
	}
	return info, nil
}

func (c *GRPCClient) Shutdown(ctx context.Context) error {
	_, err := c.client.Shutdown(ctx, &daemonpb.ShutdownRequest{})
	return err
}

func (c *GRPCClient) LogSandbox(ctx context.Context, id string, w io.Writer) error {
	resp, err := c.client.LogSandbox(ctx, &daemonpb.IDRequest{Id: id})
	if err != nil {
		return err
	}
	if w == nil {
		w = io.Discard
	}
	_, err = w.Write(resp.GetData())
	return err
}

func (c *GRPCClient) ListSandboxes(ctx context.Context) ([]sandtypes.Box, error) {
	resp, err := c.client.ListSandboxes(ctx, &daemonpb.ListSandboxesRequest{})
	if err != nil {
		return nil, err
	}
	var boxes []sandtypes.Box
	if err := json.Unmarshal(resp.GetBoxesJson(), &boxes); err != nil {
		return nil, fmt.Errorf("decode sandbox list: %w", err)
	}
	return boxes, nil
}

func (c *GRPCClient) GetSandbox(ctx context.Context, id string) (*sandtypes.Box, error) {
	resp, err := c.client.GetSandbox(ctx, &daemonpb.IDRequest{Id: id})
	if err != nil {
		return nil, err
	}
	var box sandtypes.Box
	if err := json.Unmarshal(resp.GetBoxJson(), &box); err != nil {
		return nil, fmt.Errorf("decode sandbox: %w", err)
	}
	return &box, nil
}

func (c *GRPCClient) RemoveSandbox(ctx context.Context, id string) error {
	_, err := c.client.RemoveSandbox(ctx, &daemonpb.IDRequest{Id: id})
	return err
}

func (c *GRPCClient) StopSandbox(ctx context.Context, id string) error {
	_, err := c.client.StopSandbox(ctx, &daemonpb.IDRequest{Id: id})
	return err
}

func (c *GRPCClient) StartSandbox(ctx context.Context, opts StartSandboxOpts) error {
	_, err := c.client.StartSandbox(ctx, &daemonpb.StartSandboxRequest{
		Id:       opts.ID,
		SshAgent: opts.SSHAgent,
	})
	return err
}

func (c *GRPCClient) ResolveAgentLaunchEnv(ctx context.Context, agent, envFile string) (map[string]string, error) {
	resp, err := c.client.ResolveAgentLaunchEnv(ctx, &daemonpb.ResolveAgentLaunchEnvRequest{
		Agent:   agent,
		EnvFile: envFile,
	})
	if err != nil {
		return nil, err
	}
	if resp.GetEnv() == nil {
		return map[string]string{}, nil
	}
	return resp.GetEnv(), nil
}

func (c *GRPCClient) ExportImage(ctx context.Context, id, destinationPath string) error {
	_, err := c.client.ExportImage(ctx, &daemonpb.ExportImageRequest{
		Id:              id,
		DestinationPath: destinationPath,
	})
	return err
}

func (c *GRPCClient) Stats(ctx context.Context, ids ...string) ([]types.ContainerStats, error) {
	resp, err := c.client.Stats(ctx, &daemonpb.StatsRequest{Ids: ids})
	if err != nil {
		return nil, err
	}
	var stats []types.ContainerStats
	if err := json.Unmarshal(resp.GetStatsJson(), &stats); err != nil {
		return nil, fmt.Errorf("decode container stats: %w", err)
	}
	return stats, nil
}

func (c *GRPCClient) VSC(ctx context.Context, id string) error {
	_, err := c.client.VSC(ctx, &daemonpb.IDRequest{Id: id})
	return err
}

func (c *GRPCClient) CreateSandbox(ctx context.Context, opts CreateSandboxOpts, w io.Writer) (*sandtypes.Box, error) {
	stream, err := c.client.CreateSandbox(ctx, createSandboxOptsToProto(opts))
	if err != nil {
		return nil, err
	}
	for {
		event, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		switch e := event.GetEvent().(type) {
		case *daemonpb.CreateSandboxResponse_Progress:
			if w != nil {
				if _, err := io.WriteString(w, e.Progress); err != nil {
					return nil, err
				}
			}
		case *daemonpb.CreateSandboxResponse_BoxJson:
			var box sandtypes.Box
			if err := json.Unmarshal(e.BoxJson, &box); err != nil {
				return nil, fmt.Errorf("decode created sandbox: %w", err)
			}
			return &box, nil
		case *daemonpb.CreateSandboxResponse_Error:
			if e.Error == "" {
				return nil, fmt.Errorf("sandbox creation failed")
			}
			return nil, errors.New(e.Error)
		default:
			return nil, fmt.Errorf("unknown create sandbox stream event %T", e)
		}
	}
}

func (c *GRPCClient) EnsureImage(ctx context.Context, imageName string, w io.Writer) error {
	stream, err := c.client.EnsureImage(ctx, &daemonpb.EnsureImageRequest{ImageName: imageName})
	if err != nil {
		return err
	}
	if w == nil {
		w = io.Discard
	}
	for {
		event, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			return err
		}

		switch e := event.GetEvent().(type) {
		case *daemonpb.EnsureImageResponse_Progress:
			if _, err := w.Write(e.Progress); err != nil {
				return err
			}
		case *daemonpb.EnsureImageResponse_Error:
			if e.Error == "" {
				return fmt.Errorf("image ensure failed")
			}
			return errors.New(e.Error)
		case *daemonpb.EnsureImageResponse_Ok:
			return nil
		default:
			return fmt.Errorf("unknown ensure image stream event %T", e)
		}
	}
}
