package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/version"
)

// Client is the interface for invoking methods on the sandd process via IPC, whether the
// client is running on the same MacOS instance as sandd, or inside a linux container.
type Client interface {
	Ping(ctx context.Context) error
	Version(ctx context.Context) (version.Info, error)
	Shutdown(ctx context.Context) error
	ListSandboxes(ctx context.Context) ([]sandtypes.Box, error)
	GetSandbox(ctx context.Context, id string) (*sandtypes.Box, error)
	RemoveSandbox(ctx context.Context, id string) error
	StopSandbox(ctx context.Context, id string) error
	StartSandbox(ctx context.Context, id string) error
	ExportImage(ctx context.Context, id, imageName string) error
	Stats(ctx context.Context, id ...string) ([]types.ContainerStats, error)
	VSC(ctx context.Context, id string) error
	CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*sandtypes.Box, error)
	// EnsureImage ensures imageName is present locally and up to date, pulling if needed.
	// Progress lines from the daemon are written to w as they arrive.
	EnsureImage(ctx context.Context, imageName string, w io.Writer) error
}

// defaultClient is the concrete implementation of Client that communicates
// with the sandd daemon over HTTP (unix socket or TCP).
type defaultClient struct {
	base       string
	httpClient *http.Client
}

func NewUnixSocketClient(ctx context.Context, appBaseDir string) (Client, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", filepath.Join(appBaseDir, DefaultSocketFile))
			},
		},
	}
	return &defaultClient{
		base:       "http://unix",
		httpClient: httpClient,
	}, nil
}

func (m *defaultClient) doRequest(ctx context.Context, method, path string, body any, result any) error {
	var req *http.Request
	var err error
	slog.InfoContext(ctx, "defaultClient.doRequest", "method", method, "path", path)
	if body != nil {
		reqBody, err := json.Marshal(body)
		if err != nil {
			slog.ErrorContext(ctx, "defaultClient.doRequest", "error", err)
			return err
		}
		req, err = http.NewRequestWithContext(ctx, method, m.base+path, strings.NewReader(string(reqBody)))
		if err != nil {
			slog.ErrorContext(ctx, "defaultClient.doRequest", "error", err)
			return err
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, m.base+path, nil)
		if err != nil {
			slog.ErrorContext(ctx, "defaultClient.doRequest", "error", err)
			return err
		}
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "defaultClient.doRequest", "req", req, "error", err)
		return fmt.Errorf("couldn't complete request to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if json.NewDecoder(resp.Body).Decode(&errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		slog.ErrorContext(ctx, "defaultClient.doRequest", "errorResp", errResp)

		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	slog.InfoContext(ctx, "defaultClient.doRequest", "method", method, "path", path, "resp.StatusCode", resp.StatusCode)

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return err
		}
	}

	return nil
}

func (m *defaultClient) Ping(ctx context.Context) error {
	return m.doRequest(ctx, http.MethodGet, "/ping", nil, nil)
}

func (m *defaultClient) Version(ctx context.Context) (version.Info, error) {
	var info version.Info
	if err := m.doRequest(ctx, http.MethodGet, "/version", nil, &info); err != nil {
		return version.Info{}, err
	}
	return info, nil
}

func (m *defaultClient) Shutdown(ctx context.Context) error {
	return m.doRequest(ctx, http.MethodPost, "/shutdown", nil, nil)
}

func (m *defaultClient) ListSandboxes(ctx context.Context) ([]sandtypes.Box, error) {
	var boxes []sandtypes.Box
	if err := m.doRequest(ctx, http.MethodGet, "/list", nil, &boxes); err != nil {
		return nil, err
	}
	return boxes, nil
}

func (m *defaultClient) GetSandbox(ctx context.Context, id string) (*sandtypes.Box, error) {
	var box sandtypes.Box
	if err := m.doRequest(ctx, http.MethodPost, "/get", IDRequest{ID: id}, &box); err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, fmt.Errorf("id not found: %q", id)
		}
		return nil, err
	}
	return &box, nil
}

func (m *defaultClient) RemoveSandbox(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/remove", IDRequest{ID: id}, nil)
}

func (m *defaultClient) StopSandbox(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/stop", IDRequest{ID: id}, nil)
}

func (m *defaultClient) StartSandbox(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/start", IDRequest{ID: id}, nil)
}

func (m *defaultClient) VSC(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/vsc", IDRequest{ID: id}, nil)
}

func (m *defaultClient) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*sandtypes.Box, error) {
	slog.InfoContext(ctx, "defaultClient.CreateSandbox", "opts", opts)
	var box sandtypes.Box
	if err := m.doRequest(ctx, http.MethodPost, "/create", opts, &box); err != nil {
		return nil, err
	}
	return &box, nil
}

// EnsureImage streams progress from the daemon's /ensure-image endpoint to w.
// The daemon uses "OK\n" as the success sentinel and "ERR <msg>\n" for failures,
// allowing the method to distinguish terminal state from progress text.
func (m *defaultClient) EnsureImage(ctx context.Context, imageName string, w io.Writer) error {
	slog.InfoContext(ctx, "defaultClient.EnsureImage", "imageName", imageName)

	body, err := json.Marshal(EnsureImageRequest{ImageName: imageName})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+"/ensure-image", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't complete request to daemon: %w", err)
	}
	defer resp.Body.Close()

	// Pre-streaming error (e.g. bad request): parse as JSON error response.
	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if json.NewDecoder(resp.Body).Decode(&errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read the streaming plain-text body line by line.
	// The daemon writes "OK\n" on success or "ERR <message>\n" on failure as the final line.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "OK" {
			return nil
		}
		if strings.HasPrefix(line, "ERR ") {
			return errors.New(strings.TrimPrefix(line, "ERR "))
		}
		fmt.Fprintln(w, line)
	}
	return scanner.Err()
}

func (m *defaultClient) ExportImage(ctx context.Context, id string, imageName string) error {
	return m.doRequest(ctx, http.MethodPost, "/export", ExportRequest{ID: id, ImageName: imageName}, nil)
}

// Stats implements [Client].
func (m *defaultClient) Stats(ctx context.Context, ids ...string) ([]types.ContainerStats, error) {
	var stats []types.ContainerStats
	if err := m.doRequest(ctx, http.MethodPost, "/stats", StatsRequest{IDs: ids}, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}
