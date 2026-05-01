package daemon

import (
	"context"
	"net"
	"path/filepath"

	"github.com/banksean/sand/internal/daemon/daemonpb"
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
