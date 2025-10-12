package sand

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/banksean/sand/version"
)

const (
	defaultSocketFile = "sandmux.sock"
	defaultLockFile   = "sandmux.lock"
)

type Mux struct {
	AppBaseDir string
	SocketPath string

	sber *Boxer

	listener net.Listener
	lockFile *os.File
	shutdown chan any
}

func NewMuxServer(appBaseDir string, sber *Boxer) *Mux {
	return &Mux{
		AppBaseDir: appBaseDir,
		SocketPath: filepath.Join(appBaseDir, defaultSocketFile),
		sber:       sber,
	}
}

func (m *Mux) NewClient(ctx context.Context) (*MuxClient, error) {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", m.SocketPath)
			},
		},
	}
	return &MuxClient{
		Mux:        m,
		httpClient: httpClient,
	}, nil
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
	slog.InfoContext(ctx, "Mux.startDaemonServer", "socketPath", m.SocketPath)
	// Remove old socket if it exists
	os.Remove(m.SocketPath)

	listener, err := net.Listen("unix", m.SocketPath)
	if err != nil {
		return err
	}

	if false { // TODO: do we need this?
		// Set permissions so CLI can connect
		if err := os.Chmod(m.SocketPath, 0o600); err != nil {
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
	lockFilePath := filepath.Join(m.AppBaseDir, defaultLockFile)

	slog.InfoContext(ctx, "Mux.Shutdown", "pid", os.Getpid())
	// Close listener (stops accepting new connections)
	if m.listener != nil {
		m.listener.Close()
	}

	// Remove socket file. This mail fail in many cases since closing the listener appears
	// to remove the file automatically on MacOS. Therefore we ignore the err return value.
	os.Remove(m.SocketPath)

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
	mux.HandleFunc("/version", m.handleVersion)
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

func (m *Mux) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, version.Get())
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
