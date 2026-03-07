package mux

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

	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/box"
	"github.com/banksean/sand/mux/internal/boxer"
	"github.com/banksean/sand/version"
)

const (
	defaultSocketFile     = "sandmux.sock"
	defaultLockFile       = "sandmux.lock"
	internalContainerHost = "host.container.internal"
)

type Mux struct {
	AppBaseDir    string
	SocketPath    string
	LocalHTTPPort string

	hostMCP *HostMCP
	boxer   *boxer.Boxer

	listener net.Listener
	lockFile *os.File
	shutdown chan any
	httpSrv  http.Server
}

func NewMuxServer(appBaseDir, httpPort string) *Mux {
	return &Mux{
		AppBaseDir:    appBaseDir,
		SocketPath:    filepath.Join(appBaseDir, defaultSocketFile),
		LocalHTTPPort: httpPort,
		hostMCP: &HostMCP{
			ChromeDevToolsPort: 9222,
			ChromeUserDataDir:  "/tmp/chrome-profile-stable",
		},
	}
}

// ServeUnixSocket serves the unix domain socket that sandmux clients (the host-side CLI, e.g.) connect to.
func (m *Mux) ServeUnixSocket(ctx context.Context) error {
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

	m.listener = listener
	m.shutdown = make(chan any)
	sber, err := boxer.NewBoxer(m.AppBaseDir, os.Stderr)
	if err != nil {
		return err
	}
	if err := sber.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync Boxer db with current environment state: %v\n", err)
	}

	m.boxer = sber
	// Handle cleanup on shutdown
	go m.waitForShutdown(ctx)

	// Start unix domain socket HTTP server
	go m.serveUnixSocket(ctx)

	// Start net HTTP server
	go m.serveTCPSocket(ctx)

	go func() {
		if err := m.hostMCP.StartHostServices(ctx); err != nil {
			slog.ErrorContext(ctx, "startDaemonServer MCP.StartMCPDeps", "error", err)
		}
	}()
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

	if m.hostMCP != nil {
		if err := m.hostMCP.Cleanup(ctx); err != nil {
			slog.ErrorContext(ctx, "Mux.Shutdown: MCP.Cleanup", "error", err)
		}
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

func (m *Mux) serveUnixSocket(ctx context.Context) {
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/shutdown", m.handleShutdown)
	mux.HandleFunc("/ping", m.handlePing)
	mux.HandleFunc("/version", m.handleVersion)
	mux.HandleFunc("/list", m.handleList)
	mux.HandleFunc("/get", m.handleGet)
	mux.HandleFunc("/remove", m.handleRemove)
	mux.HandleFunc("/stop", m.handleStop)
	mux.HandleFunc("/create", m.handleCreate)

	server := &http.Server{
		Handler: mux,
	}
	slog.InfoContext(ctx, "Mux.serveUnixSocketHTTP starting up")

	err := server.Serve(m.listener)
	if err != nil {
		slog.ErrorContext(ctx, "Mux.serveUnixSocketHTTP", "error", err)
	}
}

func slogHandler(ctx context.Context, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.InfoContext(ctx, "http request", "url", r.URL.String())
		h(w, r)
	}
}

// TODO: auth these requests and limit access only to known sandbox instances.
// We're already creating keypairs for ssh, so we can probably use those to
// make sure that we know which sandbox container is making the call.
// As-is, sandboxes make requests to mutate eachother, as can anything else that
// has access to port 4242 on the host OS.
func (m *Mux) serveTCPSocket(ctx context.Context) {
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/shutdown", slogHandler(ctx, m.handleShutdown))
	mux.HandleFunc("/ping", slogHandler(ctx, m.handlePing))
	mux.HandleFunc("/version", slogHandler(ctx, m.handleVersion))
	mux.HandleFunc("/list", slogHandler(ctx, m.handleList))
	mux.HandleFunc("/get", slogHandler(ctx, m.handleGet))
	mux.HandleFunc("/remove", slogHandler(ctx, m.handleRemove))
	mux.HandleFunc("/stop", slogHandler(ctx, m.handleStop))
	mux.HandleFunc("/create", slogHandler(ctx, m.handleCreate))
	mux.HandleFunc("/vsc", slogHandler(ctx, m.handleVSC))

	server := &http.Server{
		Handler: mux,
		Addr:    ":" + m.LocalHTTPPort,
	}

	slog.InfoContext(ctx, "Mux.serveHTTP starting up")
	err := server.ListenAndServe()
	if err != nil {
		slog.ErrorContext(ctx, "Mux.serveHTTP", "error", err)
	}
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

func (m *Mux) handleList(w http.ResponseWriter, r *http.Request) {
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
		writeJSONError(w, fmt.Errorf("couldn't get sandbox ID %s", args.ID), http.StatusInternalServerError)
		return
	}
	if sbox == nil {
		writeJSONError(w, fmt.Errorf("got a nil sandbox for ID %s", args.ID), http.StatusInternalServerError)
		return
	}
	sbox.Sync(r.Context())

	if sbox == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	ctr, err := m.boxer.GetContainer(r.Context(), sbox.ContainerID)
	if err != nil {
		http.Error(w, "couldn't get container", http.StatusInternalServerError)
		return
	}
	sbox.Container = ctr
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

	sbox, err := m.createSandbox(r.Context(), opts)
	if err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, sbox)
}

func (m *Mux) handleVSC(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var c struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	if c.ID == "" {
		writeJSONError(w, fmt.Errorf("missing id"), http.StatusBadRequest)
		return
	}
	sbox, err := m.GetSandbox(ctx, c.ID)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "id", c.ID)
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	ctr := sbox.Container

	if ctr == nil || ctr.Status != "running" {
		writeJSONError(w, fmt.Errorf("cannot connect to sandbox %q becacuse it is not currently running", c.ID), http.StatusInternalServerError)
		return
	}

	hostname := types.GetContainerHostname(ctr)
	vscCmd := exec.Command("code", "--remote", fmt.Sprintf("ssh-remote+root@%s", hostname), "/app", "-n")
	slog.InfoContext(ctx, "main: running vsc with", "cmd", strings.Join(vscCmd.Args, " "))
	out, err := vscCmd.CombinedOutput()
	if err != nil {
		writeJSONError(w, fmt.Errorf("failed to start vsc for %q: %w", c.ID, err), http.StatusInternalServerError)
		slog.ErrorContext(ctx, "VscCmd.Run cmd", "out", out, "error", err)
	}
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

// StopSandbox stops a single sandbox container.
func (m *Mux) StopSandbox(ctx context.Context, id string) error {
	sbox, err := m.boxer.Get(ctx, id)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", id)
	}
	return m.boxer.StopContainer(ctx, sbox)
}

type CreateSandboxOpts struct {
	ID           string `json:"id,omitempty"`
	CloneFromDir string `json:"cloneFromDir,omitempty"`
	ImageName    string `json:"imageName,omitempty"`
	EnvFile      string `json:"envFile,omitempty"`
	Cloner       string `json:"cloner,omitempty"`
}

// createSandbox creates a new sandbox and starts its container.
func (m *Mux) createSandbox(ctx context.Context, opts CreateSandboxOpts) (*box.Box, error) {
	agentType := opts.Cloner
	if agentType == "" {
		agentType = "default"
	}
	slog.InfoContext(ctx, "CreateSandbox", "agentType", agentType)

	sbox, err := m.boxer.NewSandbox(ctx, agentType, opts.ID, opts.CloneFromDir, opts.ImageName, opts.EnvFile)
	if err != nil {
		return nil, err
	}

	ctr, err := m.boxer.GetContainer(ctx, sbox.ContainerID)

	if ctr == nil {
		err := m.boxer.CreateContainer(ctx, sbox)
		if err != nil {
			return nil, err
		}
		if err := m.boxer.UpdateContainerID(ctx, sbox, sbox.ContainerID); err != nil {
			return nil, err
		}
		ctr, err = m.boxer.GetContainer(ctx, sbox.ContainerID)
		if err != nil || ctr == nil {
			return nil, fmt.Errorf("failed to get container after creation")
		}
	}

	if ctr.Status != "running" {
		err := m.boxer.StartContainer(ctx, sbox)
		if err != nil {
			return nil, err
		}
	}
	sbox.Container = ctr
	return sbox, nil
}
