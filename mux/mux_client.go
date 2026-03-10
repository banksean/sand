package mux

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/banksean/sand/sandtypes"
	"github.com/banksean/sand/version"
)

// MuxClient is the interface for invoking methods on the sandd process via IPC, whether the
// client is running on the same MacOS instance as sandd, or inside a linux container.
type MuxClient interface {
	Ping(ctx context.Context) error
	Version(ctx context.Context) (version.Info, error)
	Shutdown(ctx context.Context) error
	ListSandboxes(ctx context.Context) ([]sandtypes.Box, error)
	GetSandbox(ctx context.Context, id string) (*sandtypes.Box, error)
	RemoveSandbox(ctx context.Context, id string) error
	StopSandbox(ctx context.Context, id string) error
	VSC(ctx context.Context, id string) error
	CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*sandtypes.Box, error)
}

// defaultClient is the concrete implementation of MuxClient that communicates
// with the sandd daemon over HTTP (unix socket or TCP).
type defaultClient struct {
	base       string
	httpClient *http.Client
}

func NewHTTPClient(ctx context.Context, containerHostPort string) (MuxClient, error) {
	return &defaultClient{
		base:       "http://" + internalContainerHost + ":" + containerHostPort,
		httpClient: http.DefaultClient,
	}, nil
}

func NewUnixSocketClient(ctx context.Context, appBaseDir string) (MuxClient, error) {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", filepath.Join(appBaseDir, defaultSocketFile))
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
		slog.ErrorContext(ctx, "defaultClient.doRequest", "error", err)
		return fmt.Errorf("daemon not running: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
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
	var resp map[string]string
	if err := m.doRequest(ctx, http.MethodGet, "/ping", nil, &resp); err != nil {
		return err
	}
	return nil
}

func (m *defaultClient) Version(ctx context.Context) (version.Info, error) {
	var info version.Info
	if err := m.doRequest(ctx, http.MethodGet, "/version", nil, &info); err != nil {
		return version.Info{}, err
	}
	return info, nil
}

func (m *defaultClient) Shutdown(ctx context.Context) error {
	var resp map[string]string
	if err := m.doRequest(ctx, http.MethodPost, "/shutdown", nil, &resp); err != nil {
		return err
	}

	return nil
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
	if err := m.doRequest(ctx, http.MethodPost, "/get", map[string]string{"id": id}, &box); err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, fmt.Errorf("id not found: %q", id)
		}
		return nil, err
	}
	return &box, nil
}

func (m *defaultClient) RemoveSandbox(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/remove", map[string]string{"id": id}, nil)
}

func (m *defaultClient) StopSandbox(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/stop", map[string]string{"id": id}, nil)
}

func (m *defaultClient) VSC(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/vsc", map[string]string{"id": id}, nil)
}

func (m *defaultClient) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*sandtypes.Box, error) {
	var box sandtypes.Box
	if err := m.doRequest(ctx, http.MethodPost, "/create", opts, &box); err != nil {
		return nil, err
	}
	return &box, nil
}

// EnsureDaemon attempts to verify that the sandd daemon is running, and if not,
// starting a new instance of it.
//
// TODO: Make sure this doesn't get called from an innie.  That probably means moving
// this function to somewhere under mux/internal/...
func EnsureDaemon(ctx context.Context, appBaseDir string) error {
	socketPath := filepath.Join(appBaseDir, defaultSocketFile)
	slog.Info("EnsureDaemon", "socketPath", socketPath)

	// Try to connect to existing daemon
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err == nil {
		conn.Close()
		// Daemon is running, check if version matches
		if err := checkDaemonVersion(ctx, appBaseDir); err != nil {
			slog.Info("EnsureDaemon", "versionMismatch", err.Error())
			// Version mismatch, shut down old daemon
			if err := shutdownDaemon(appBaseDir); err != nil {
				slog.Warn("EnsureDaemon", "shutdownError", err.Error())
				// Continue to try starting new daemon anyway
			}
			// Fall through to start new daemon
		} else {
			return nil // Daemon running with correct version
		}
	}

	// Start daemon in background
	cmd := exec.Command("sandd", "start", "--app-base-dir", appBaseDir)
	slog.Info("EnsureDaemon", "cmd", strings.Join(cmd.Args, " "))
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.Dir = appBaseDir

	// Detach from parent process
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Wait for daemon to be ready
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
	}

	return fmt.Errorf("daemon failed to start")
}

func checkDaemonVersion(ctx context.Context, appBaseDir string) error {
	client, err := NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	daemonVersion, err := client.Version(ctx)
	if err != nil {
		return fmt.Errorf("failed to get daemon version: %w", err)
	}

	cliVersion := version.Get()
	if !cliVersion.Equal(daemonVersion) {
		return fmt.Errorf("version mismatch: CLI=%s, Daemon=%s", cliVersion.GitCommit, daemonVersion.GitCommit)
	}

	return nil
}

func shutdownDaemon(appBaseDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewUnixSocketClient(ctx, appBaseDir)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	return client.Shutdown(ctx)
}
