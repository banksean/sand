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
	StartSandbox(ctx context.Context, opts StartSandboxOpts) error
	ExportImage(ctx context.Context, id, imageName string) error
	Stats(ctx context.Context, id ...string) ([]types.ContainerStats, error)
	VSC(ctx context.Context, id string) error
	CreateSandbox(ctx context.Context, opts CreateSandboxOpts, w io.Writer) (*sandtypes.Box, error)
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

func (m *defaultClient) StartSandbox(ctx context.Context, opts StartSandboxOpts) error {
	return m.doRequest(ctx, http.MethodPost, "/start", StartSandboxRequest{
		ID:       opts.ID,
		SSHAgent: opts.SSHAgent,
	}, nil)
}

func (m *defaultClient) VSC(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/vsc", IDRequest{ID: id}, nil)
}

func (m *defaultClient) CreateSandbox(ctx context.Context, opts CreateSandboxOpts, w io.Writer) (*sandtypes.Box, error) {
	slog.InfoContext(ctx, "defaultClient.CreateSandbox", "opts", opts)

	body, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+"/create-stream", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't complete request to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if json.NewDecoder(resp.Body).Decode(&errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var event CreateSandboxEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		switch event.Type {
		case "progress":
			if w != nil {
				if _, err := io.WriteString(w, event.Data); err != nil {
					return nil, err
				}
			}
		case "result":
			if event.Box == nil {
				return nil, fmt.Errorf("create-stream response missing sandbox result")
			}
			return event.Box, nil
		case "error":
			if event.Error == "" {
				return nil, fmt.Errorf("sandbox creation failed")
			}
			return nil, errors.New(event.Error)
		default:
			return nil, fmt.Errorf("unknown create-stream event type %q", event.Type)
		}
	}
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

	// Read the streaming plain-text body.
	// The daemon writes "OK\n" on success or "ERR <message>\n" on failure as the final line.
	// PTY progress output uses \r for in-place terminal updates, which would make the
	// default ScanLines accumulate a token > 64 KB. scanLinesOrCR splits on \r, \n, or \r\n
	// and includes the terminator in the token so the caller receives each progress chunk
	// immediately and \r-terminated lines overwrite correctly in the user's terminal.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(scanLinesOrCR)
	scanner.Buffer(make([]byte, 64*1024), 512*1024)
	for scanner.Scan() {
		raw := scanner.Bytes()
		s := strings.TrimRight(string(raw), "\r\n")
		if s == "OK" {
			return nil
		}
		if strings.HasPrefix(s, "ERR ") {
			return errors.New(s[4:])
		}
		w.Write(raw)
	}
	return scanner.Err()
}

// scanLinesOrCR is a bufio.SplitFunc that splits on \r, \n, or \r\n and
// includes the terminator in the returned token. This lets callers forward
// each chunk to a terminal immediately and preserves \r-based in-place
// progress updates (as used by 'container image pull').
func scanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		switch b {
		case '\n':
			return i + 1, data[:i+1], nil
		case '\r':
			if i+1 < len(data) {
				if data[i+1] == '\n' {
					return i + 2, data[:i+2], nil // CRLF: consume both
				}
				return i + 1, data[:i+1], nil // lone CR
			}
			if !atEOF {
				// CR at end of buffer but not EOF: might be CRLF, request more data
				return 0, nil, nil
			}
			return i + 1, data[:i+1], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func (m *defaultClient) ExportImage(ctx context.Context, id string, destinationPath string) error {
	return m.doRequest(ctx, http.MethodPost, "/export", ExportRequest{ID: id, DestinationPath: destinationPath}, nil)
}

// Stats implements [Client].
func (m *defaultClient) Stats(ctx context.Context, ids ...string) ([]types.ContainerStats, error) {
	var stats []types.ContainerStats
	if err := m.doRequest(ctx, http.MethodPost, "/stats", StatsRequest{IDs: ids}, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}
