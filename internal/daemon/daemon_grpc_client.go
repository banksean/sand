package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"

	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GRPCClient is the initial gRPC client used while the daemon protocol is
// migrated incrementally from HTTP.
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
	return version.Info{
		GitRepo:   resp.GetGitRepo(),
		GitBranch: resp.GetGitBranch(),
		GitCommit: resp.GetGitCommit(),
		BuildTime: resp.GetBuildTime(),
	}, nil
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
