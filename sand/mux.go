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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	defaultSocketFile = "sandmux.sock"
	defaultLockFile   = "sandmux.lock"
)

type Mux struct {
	AppBaseDir string
	sber       *SandBoxer

	listener net.Listener
	lockFile *os.File
	shutdown chan any
}

func NewMux(appBaseDir string, sber *SandBoxer) *Mux {
	return &Mux{
		AppBaseDir: appBaseDir,
		sber:       sber,
	}
}

// ServeUnix serves the unix domain socket that sandmux clients (the CLI, e.g.) connect to.
func (m *Mux) ServeUnix(ctx context.Context) error {
	lockFilePath := filepath.Join(m.AppBaseDir, defaultLockFile)
	slog.InfoContext(ctx, "Mux.ServeUnix", "mux", m, "pid", os.Getpid(), "lockFilePath", lockFilePath)
	lockFile, err := acquireLock(lockFilePath)
	if err != nil {
		return err
	}
	m.lockFile = lockFile

	if err := m.startDaemonServer(ctx); err != nil {
		slog.ErrorContext(ctx, "Mux.Serve startDaemonServer", "error", err)
		return err
	}

	return nil
}

func (m *Mux) startDaemonServer(ctx context.Context) error {
	socketPath := filepath.Join(m.AppBaseDir, defaultSocketFile)
	slog.InfoContext(ctx, "Mux.startDaemonServer", "socketPath", socketPath)
	// Remove old socket if it exists
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}

	if false { // TODO: do we need this?
		// Set permissions so CLI can connect
		if err := os.Chmod(socketPath, 0o600); err != nil {
			return err
		}
	}

	m.listener = listener
	m.shutdown = make(chan any)

	// Handle cleanup on shutdown
	go m.waitForShutdown(ctx)

	// Start HTTP server
	go m.serveHTTP(ctx)

	// Wait for shutdown signal
	<-m.shutdown

	return nil
}

func (m *Mux) waitForShutdown(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done(): // TODO: is this really necessary here?
	case <-sigChan:
		m.Shutdown(ctx)
	case <-m.shutdown:
		// Shutdown already initiated
	}
}

func (m *Mux) Shutdown(ctx context.Context) {
	socketPath := filepath.Join(m.AppBaseDir, defaultSocketFile)
	lockFilePath := filepath.Join(m.AppBaseDir, defaultLockFile)

	slog.InfoContext(ctx, "Mux.Shutdown", "pid", os.Getpid())
	// Close listener (stops accepting new connections)
	if m.listener != nil {
		m.listener.Close()
	}

	// Remove socket file. This mail fail in many cases since closing the listener appears
	// to remove the file automatically on MacOS. Therefore we ignore the err return value.
	os.Remove(socketPath)

	// Release and remove lock file
	if m.lockFile != nil {
		syscall.Flock(int(m.lockFile.Fd()), syscall.LOCK_UN)
		m.lockFile.Close()
		if err := os.Remove(lockFilePath); err != nil {
			slog.ErrorContext(ctx, "Mux.Shutdown removing lockfile", "error", err, "LockFilePath", lockFilePath)
		}
	}

	// Signal shutdown complete
	close(m.shutdown)
}

func (m *Mux) serveHTTP(ctx context.Context) {
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/shutdown", m.handleShutdown)
	mux.HandleFunc("/ping", m.handlePing)
	mux.HandleFunc("/list", m.handleHTTPList)
	mux.HandleFunc("/get", m.handleGet)
	mux.HandleFunc("/remove", m.handleRemove)
	mux.HandleFunc("/stop", m.handleStop)
	mux.HandleFunc("/create", m.handleCreate)

	server := &http.Server{
		Handler: mux,
	}

	server.Serve(m.listener)
}

// HTTP handler helpers
func writeJSONError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// HTTP handlers
func (m *Mux) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})

	// Shutdown daemon after response is sent
	go func() {
		time.Sleep(100 * time.Millisecond)
		m.Shutdown(r.Context())
	}()
}

func (m *Mux) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]string{"status": "pong"})
}

func (m *Mux) handleHTTPList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	boxes, err := m.ListSandboxes(r.Context())
	if err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, boxes)
}

func (m *Mux) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var args struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	if args.ID == "" {
		writeJSONError(w, fmt.Errorf("missing id"), http.StatusBadRequest)
		return
	}

	sbox, err := m.GetSandbox(r.Context(), args.ID)
	if err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	if sbox == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	writeJSON(w, sbox)
}

func (m *Mux) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var args struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	if args.ID == "" {
		writeJSONError(w, fmt.Errorf("missing id"), http.StatusBadRequest)
		return
	}

	if err := m.RemoveSandbox(r.Context(), args.ID); err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (m *Mux) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var args struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	if args.ID == "" {
		writeJSONError(w, fmt.Errorf("missing id"), http.StatusBadRequest)
		return
	}

	if err := m.StopSandbox(r.Context(), args.ID); err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (m *Mux) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var opts CreateSandboxOpts
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	sbox, err := m.CreateSandbox(r.Context(), opts)
	if err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, sbox)
}

func acquireLock(lockFile string) (*os.File, error) {
	file, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}

	// Try to acquire exclusive lock (non-blocking)
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("daemon already running")
	}

	// Write PID to file
	file.Truncate(0)
	fmt.Fprintf(file, "%d", os.Getpid())

	return file, nil
}

type MuxClient struct {
	Mux        *Mux
	httpClient *http.Client
}

func (m *Mux) NewClient(ctx context.Context) (*MuxClient, error) {
	socketPath := filepath.Join(m.AppBaseDir, defaultSocketFile)
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
	return &MuxClient{
		Mux:        m,
		httpClient: httpClient,
	}, nil
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

func (m *MuxClient) Shutdown(ctx context.Context) error {
	var resp map[string]string
	if err := m.doRequest(ctx, http.MethodPost, "/shutdown", nil, &resp); err != nil {
		return err
	}

	// Wait briefly to verify daemon stopped
	time.Sleep(200 * time.Millisecond)

	// Verify socket is gone
	socketPath := filepath.Join(m.Mux.AppBaseDir, defaultSocketFile)
	if _, err := os.Stat(socketPath); err == nil {
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

func EnsureDaemon(appBaseDir string) error {
	socketPath := filepath.Join(appBaseDir, defaultSocketFile)
	slog.Info("EnsureDaemon", "socketPath", socketPath)

	// Try to connect to existing daemon
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err == nil {
		conn.Close()
		return nil // Daemon already running
	}

	// Start daemon in background
	cmd := exec.Command(os.Args[0], "daemon", "--clone-root", filepath.Join(appBaseDir, "boxen"))
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
