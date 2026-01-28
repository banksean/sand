package sand

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/banksean/sand/version"
)

type MuxClient struct {
	Mux        *Mux
	httpClient *http.Client
}

func (m *MuxClient) doRequest(ctx context.Context, method, path string, body any, result any) error {
	var req *http.Request
	var err error

	if body != nil {
		reqBody, err := json.Marshal(body)
		if err != nil {
			return err
		}
		req, err = http.NewRequestWithContext(ctx, method, "http://unix"+path, strings.NewReader(string(reqBody)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, "http://unix"+path, nil)
		if err != nil {
			return err
		}
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
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
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return err
		}
	}

	return nil
}

func (m *MuxClient) Ping(ctx context.Context) error {
	var resp map[string]string
	if err := m.doRequest(ctx, http.MethodGet, "/ping", nil, &resp); err != nil {
		return err
	}
	return nil
}

func (m *MuxClient) Version(ctx context.Context) (version.Info, error) {
	var info version.Info
	if err := m.doRequest(ctx, http.MethodGet, "/version", nil, &info); err != nil {
		return version.Info{}, err
	}
	return info, nil
}

func (m *MuxClient) Shutdown(ctx context.Context) error {
	var resp map[string]string
	if err := m.doRequest(ctx, http.MethodPost, "/shutdown", nil, &resp); err != nil {
		return err
	}

	// Wait briefly to verify daemon stopped
	time.Sleep(200 * time.Millisecond)

	// Verify socket is gone
	if _, err := os.Stat(m.Mux.SocketPath); err == nil {
		return fmt.Errorf("daemon may not have shut down cleanly")
	}

	return nil
}

func (m *MuxClient) ListSandboxes(ctx context.Context) ([]Box, error) {
	var boxes []Box
	if err := m.doRequest(ctx, http.MethodGet, "/list", nil, &boxes); err != nil {
		return nil, err
	}
	for i := range boxes {
		boxes[i].containerService = m.Mux.boxer.containerService
	}
	return boxes, nil
}

func (m *MuxClient) GetSandbox(ctx context.Context, id string) (*Box, error) {
	var box Box
	if err := m.doRequest(ctx, http.MethodPost, "/get", map[string]string{"id": id}, &box); err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, fmt.Errorf("id not found: %q", id)
		}
		return nil, err
	}
	box.containerService = m.Mux.boxer.containerService
	return &box, nil
}

func (m *MuxClient) RemoveSandbox(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/remove", map[string]string{"id": id}, nil)
}

func (m *MuxClient) StopSandbox(ctx context.Context, id string) error {
	return m.doRequest(ctx, http.MethodPost, "/stop", map[string]string{"id": id}, nil)
}

func (m *MuxClient) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*Box, error) {
	var box Box
	if err := m.doRequest(ctx, http.MethodPost, "/create", opts, &box); err != nil {
		return nil, err
	}
	box.containerService = m.Mux.boxer.containerService
	return &box, nil
}

// ListSandboxes returns all sandboxes.
func (m *Mux) ListSandboxes(ctx context.Context) ([]Box, error) {
	return m.boxer.List(ctx)
}

// GetSandbox retrieves a sandbox by ID.
func (m *Mux) GetSandbox(ctx context.Context, id string) (*Box, error) {
	return m.boxer.Get(ctx, id)
}

// RemoveSandbox removes a single sandbox.
func (m *Mux) RemoveSandbox(ctx context.Context, id string) error {
	sbox, err := m.boxer.Get(ctx, id)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", id)
	}
	return m.boxer.Cleanup(ctx, sbox)
}

func EnsureDaemon(ctx context.Context, appBaseDir, logFile string) error {
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
	cmd := exec.Command(os.Args[0], "daemon", "start", "--log-file", logFile, "--app-base-dir", appBaseDir)
	slog.Info("EnsureDaemon", "cmd", strings.Join(cmd.Args, " "))
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

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
	mux := NewMuxServer(appBaseDir, nil)
	client, err := mux.NewClient(ctx)
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

	mux := NewMuxServer(appBaseDir, nil)
	client, err := mux.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	return client.Shutdown(ctx)
}
