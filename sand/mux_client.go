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
	return boxes, nil
}

func (m *MuxClient) GetSandbox(ctx context.Context, id string) (*Box, error) {
	var box Box
	if err := m.doRequest(ctx, http.MethodPost, "/get", map[string]string{"id": id}, &box); err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
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
	return &box, nil
}

// ListSandboxes returns all sandboxes.
func (m *Mux) ListSandboxes(ctx context.Context) ([]Box, error) {
	return m.sber.List(ctx)
}

// GetSandbox retrieves a sandbox by ID.
func (m *Mux) GetSandbox(ctx context.Context, id string) (*Box, error) {
	return m.sber.Get(ctx, id)
}

// RemoveSandbox removes a single sandbox.
func (m *Mux) RemoveSandbox(ctx context.Context, id string) error {
	sbox, err := m.sber.Get(ctx, id)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", id)
	}
	return m.sber.Cleanup(ctx, sbox)
}

// StopSandbox stops a single sandbox container.
func (m *Mux) StopSandbox(ctx context.Context, id string) error {
	sbox, err := m.sber.Get(ctx, id)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", id)
	}
	return m.sber.StopContainer(ctx, sbox)
}

type CreateSandboxOpts struct {
	ID            string `json:"id,omitempty"`
	CloneFromDir  string `json:"cloneFromDir,omitempty"`
	ImageName     string `json:"imageName,omitempty"`
	DockerFileDir string `json:"dockerFileDir,omitempty"`
	EnvFile       string `json:"envFile,omitempty"`
}

// CreateSandbox creates a new sandbox and starts its container.
func (m *Mux) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*Box, error) {
	sbox, err := m.sber.NewSandbox(ctx, opts.ID, opts.CloneFromDir, opts.ImageName, opts.DockerFileDir, opts.EnvFile)
	if err != nil {
		return nil, err
	}

	ctr, err := sbox.GetContainer(ctx)
	if err != nil {
		return nil, err
	}

	if ctr == nil {
		if err := sbox.CreateContainer(ctx); err != nil {
			return nil, err
		}
		if err := m.sber.UpdateContainerID(ctx, sbox, sbox.ContainerID); err != nil {
			return nil, err
		}
		ctr, err = sbox.GetContainer(ctx)
		if err != nil || ctr == nil {
			return nil, fmt.Errorf("failed to get container after creation")
		}
	}

	if ctr.Status != "running" {
		if err := sbox.StartContainer(ctx); err != nil {
			return nil, err
		}
	}

	return sbox, nil
}

func EnsureDaemon(appBaseDir, logFile string) error {
	socketPath := filepath.Join(appBaseDir, defaultSocketFile)
	slog.Info("EnsureDaemon", "socketPath", socketPath)

	// Try to connect to existing daemon
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err == nil {
		conn.Close()
		return nil // Daemon already running
	}

	// Start daemon in background
	cmd := exec.Command(os.Args[0], "daemon", "start", "--log-file", logFile, "--clone-root", filepath.Join(appBaseDir, "boxen"))
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
