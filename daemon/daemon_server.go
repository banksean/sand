package daemon

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
	"sync"
	"syscall"
	"time"

	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/daemon/internal/boxer"
	"github.com/banksean/sand/sandtypes"
	"github.com/banksean/sand/version"
)

const (
	defaultSocketFile = "sandd.sock"
	defaultLockFile   = "sandd.lock"
	containerIDKey    = "containerID"
)

type Daemon struct {
	AppBaseDir  string
	SocketPath  string
	LocalDomain string

	hostMCP *HostMCP
	boxer   *boxer.Boxer

	outieListener net.Listener

	innieServersMu sync.Mutex
	// TODO: sync with container lifecycle for cases like restarting sandd with sandboxes running.
	innieServers map[string]*http.Server

	lockFile *os.File
	shutdown chan any
	httpSrv  http.Server
}

func NewDaemon(appBaseDir, localDomain string) *Daemon {
	return &Daemon{
		AppBaseDir:  appBaseDir,
		SocketPath:  filepath.Join(appBaseDir, defaultSocketFile),
		LocalDomain: localDomain,
		hostMCP: &HostMCP{
			ChromeDevToolsPort: 9222,
			ChromeUserDataDir:  "/tmp/chrome-profile-stable",
		},
		innieServers: map[string]*http.Server{},
	}
}

// ServeUnixSocket serves the unix domain socket that sandd clients (the host-side CLI, e.g.) connect to.
func (d *Daemon) ServeUnixSocket(ctx context.Context) error {
	lockFilePath := filepath.Join(d.AppBaseDir, defaultLockFile)
	slog.InfoContext(ctx, "Daemon.ServeUnix", "mux", d, "pid", os.Getpid(), "lockFilePath", lockFilePath)
	lockFile, err := acquireLock(lockFilePath)
	if err != nil {
		return err
	}
	d.lockFile = lockFile

	if err := d.startDaemonServer(ctx); err != nil {
		slog.ErrorContext(ctx, "Daemon.Serve startDaemonServer", "error", err)
		return err
	}

	return nil
}

func (d *Daemon) startDaemonServer(ctx context.Context) error {
	slog.InfoContext(ctx, "Daemon.startDaemonServer", "socketPath", d.SocketPath)
	// Remove old socket if it exists
	os.Remove(d.SocketPath)

	unixListener, err := net.Listen("unix", d.SocketPath)
	if err != nil {
		return err
	}
	d.outieListener = unixListener

	d.shutdown = make(chan any)
	sber, err := boxer.NewBoxer(d.AppBaseDir, d.LocalDomain, os.Stderr)
	if err != nil {
		return err
	}
	if err := sber.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync Boxer db with current environment state: %v\n", err)
	}

	d.boxer = sber
	// Handle cleanup on shutdown
	go d.waitForShutdown(ctx)

	// Start unix domain socket HTTP server
	go d.serveOutieSocket(ctx)

	go func() {
		if err := d.hostMCP.StartHostServices(ctx); err != nil {
			slog.ErrorContext(ctx, "startDaemonServer MCP.StartMCPDeps", "error", err)
		}
	}()
	// Wait for shutdown signal
	<-d.shutdown

	return nil
}

func (d *Daemon) waitForShutdown(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done(): // TODO: is this really necessary here?
	case <-sigChan:
		d.Shutdown(ctx)
	case <-d.shutdown:
		// Shutdown already initiated
	}
}

func (d *Daemon) Shutdown(ctx context.Context) {
	lockFilePath := filepath.Join(d.AppBaseDir, defaultLockFile)

	slog.InfoContext(ctx, "Daemon.Shutdown", "pid", os.Getpid())
	// Close listener (stops accepting new connections)
	if d.outieListener != nil {
		d.outieListener.Close()
	}

	if d.hostMCP != nil {
		if err := d.hostMCP.Cleanup(ctx); err != nil {
			slog.ErrorContext(ctx, "Daemon.Shutdown: MCP.Cleanup", "error", err)
		}
	}

	// Remove socket file. This mail fail in many cases since closing the listener appears
	// to remove the file automatically on MacOS. Therefore we ignore the err return value.
	os.Remove(d.SocketPath)

	// Release and remove lock file
	if d.lockFile != nil {
		syscall.Flock(int(d.lockFile.Fd()), syscall.LOCK_UN)
		d.lockFile.Close()
		if err := os.Remove(lockFilePath); err != nil {
			slog.ErrorContext(ctx, "Daemon.Shutdown removing lockfile", "error", err, "LockFilePath", lockFilePath)
		}
	}

	// Signal shutdown complete
	close(d.shutdown)
}

func (d *Daemon) serveOutieSocket(ctx context.Context) {
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/shutdown", d.handleShutdown)
	mux.HandleFunc("/ping", d.handlePing)
	mux.HandleFunc("/version", d.handleVersion)
	mux.HandleFunc("/list", d.handleList)
	mux.HandleFunc("/get", d.handleGet)
	mux.HandleFunc("/remove", d.handleRemove)
	mux.HandleFunc("/stop", d.handleStop)
	mux.HandleFunc("/create", d.handleCreate)

	server := &http.Server{
		Handler: mux,
	}
	slog.InfoContext(ctx, "Daemon.serveUnixSocketHTTP starting up")

	err := server.Serve(d.outieListener)
	if err != nil {
		slog.ErrorContext(ctx, "Daemon.serveUnixSocketHTTP", "error", err)
	}
}

func slogHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.InfoContext(r.Context(), "http request", "url", r.URL.String())
		h(w, r)
	}
}

func fromSandbox(sandboxID string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), containerIDKey, sandboxID)
		r = r.WithContext(ctx)
		slog.InfoContext(ctx, "http request", "url", r.URL.String())
		h(w, r)
	}
}

func sandboxIDOf(r *http.Request) (string, error) {
	ctx := r.Context()
	ret, ok := ctx.Value(containerIDKey).(string)
	if ok {
		return ret, nil
	}
	var args struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		return ret, err
	}

	return args.ID, nil
}

func (d *Daemon) stopInnieServer(ctx context.Context, id string) error {
	d.innieServersMu.Lock()
	defer d.innieServersMu.Unlock()
	if srv, ok := d.innieServers[id]; ok {
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("StopSandbox could not shut down sandbox's http server: %w", err)
		}
	}
	return nil
}

func (d *Daemon) serveInnieSocket(ctx context.Context, sandboxID string, unixListener net.Listener) {
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/shutdown", slogHandler(fromSandbox(sandboxID, d.handleShutdown)))
	mux.HandleFunc("/ping", slogHandler(fromSandbox(sandboxID, d.handlePing)))
	mux.HandleFunc("/version", slogHandler(fromSandbox(sandboxID, d.handleVersion)))
	mux.HandleFunc("/list", slogHandler(fromSandbox(sandboxID, d.handleList)))
	mux.HandleFunc("/get", slogHandler(fromSandbox(sandboxID, d.handleGet)))
	mux.HandleFunc("/remove", slogHandler(fromSandbox(sandboxID, d.handleRemove)))
	mux.HandleFunc("/stop", slogHandler(fromSandbox(sandboxID, d.handleStop)))
	mux.HandleFunc("/create", slogHandler(fromSandbox(sandboxID, d.handleCreate)))
	mux.HandleFunc("/vsc", slogHandler(fromSandbox(sandboxID, d.handleVSC)))
	mux.HandleFunc("/sandbox-config", slogHandler(fromSandbox(sandboxID, d.handleSandboxConfig)))
	mux.HandleFunc("/", slogHandler(fromSandbox(sandboxID, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})))

	server := &http.Server{
		Handler: mux,
	}
	d.innieServersMu.Lock()
	d.innieServers[sandboxID] = server
	d.innieServersMu.Unlock()

	defer unixListener.Close()

	slog.InfoContext(ctx, "Daemon.serveInnieSocket starting up", "container ID", sandboxID)
	err := server.Serve(unixListener)
	if err != nil {
		slog.ErrorContext(ctx, "Daemon.serveInnieSocket", "error", err)
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
func (d *Daemon) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})

	// Shutdown daemon after response is sent
	go func() {
		time.Sleep(100 * time.Millisecond)
		d.Shutdown(r.Context())
	}()
}

func (d *Daemon) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]string{"status": "pong"})
}

func (d *Daemon) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, version.Get())
}

func (d *Daemon) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	boxes, err := d.ListSandboxes(r.Context())
	if err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, boxes)
}

func (d *Daemon) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sandboxID, err := sandboxIDOf(r)
	if err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}
	sbox, err := d.GetSandbox(r.Context(), sandboxID)
	if err != nil {
		writeJSONError(w, fmt.Errorf("couldn't get sandbox ID %s", sandboxID), http.StatusInternalServerError)
		return
	}
	if sbox == nil {
		writeJSONError(w, fmt.Errorf("got a nil sandbox for ID %s", sandboxID), http.StatusInternalServerError)
		return
	}

	if err := d.boxer.SyncBox(r.Context(), sbox); err != nil {
		slog.ErrorContext(r.Context(), "Daemon.handleGet boxer.SyncBox", "error", err)
		writeJSONError(w, fmt.Errorf("failed to sync sandbox for ID %s", sandboxID), http.StatusInternalServerError)
	}

	if sbox == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	ctr, err := d.boxer.GetContainer(r.Context(), sbox.ContainerID)
	if err != nil {
		http.Error(w, "couldn't get container", http.StatusInternalServerError)
		return
	}
	sbox.Container = ctr
	writeJSON(w, sbox)
}

func (d *Daemon) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sandboxID, err := sandboxIDOf(r)
	if err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	if err := d.RemoveSandbox(r.Context(), sandboxID); err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (d *Daemon) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sandboxID, err := sandboxIDOf(r)
	if err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	if err := d.StopSandbox(r.Context(), sandboxID); err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (d *Daemon) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var opts CreateSandboxOpts
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	sbox, err := d.createSandbox(r.Context(), opts)
	if err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, sbox)
}

func (d *Daemon) handleVSC(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sandboxID, err := sandboxIDOf(r)
	if err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}
	sbox, err := d.GetSandbox(ctx, sandboxID)
	if err != nil {
		slog.ErrorContext(ctx, "GetSandbox", "error", err, "id", sandboxID)
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	ctr := sbox.Container

	if ctr == nil || ctr.Status != "running" {
		writeJSONError(w, fmt.Errorf("cannot connect to sandbox %q becacuse it is not currently running", sandboxID), http.StatusInternalServerError)
		return
	}

	hostname := types.GetContainerHostname(ctr)
	vscCmd := exec.Command("code", "--remote", fmt.Sprintf("ssh-remote+root@%s", hostname), "/app", "-n")
	slog.InfoContext(ctx, "main: running vsc with", "cmd", strings.Join(vscCmd.Args, " "))
	out, err := vscCmd.CombinedOutput()
	if err != nil {
		writeJSONError(w, fmt.Errorf("failed to start vsc for %q: %w", sandboxID, err), http.StatusInternalServerError)
		slog.ErrorContext(ctx, "VscCmd.Run cmd", "out", out, "error", err)
	}
}

// handleSandboxConfig is called by the dnsproxy sidecar running in the VM to fetch
// the allowed-domains list for a given sandbox at startup time.
// We identify the sandbox by the source IP of the request rather than a name parameter,
// because the VM's hostname is baked into the init image and does not match the sandbox ID.
func (d *Daemon) handleSandboxConfig(w http.ResponseWriter, r *http.Request) {
	slog.InfoContext(r.Context(), "handleSandboxConfig")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callerIP, _, err := net.SplitHostPort(r.RemoteAddr)
	slog.InfoContext(r.Context(), "handleSandboxConfig", "callerIP", callerIP, "error", err)
	if err != nil {
		writeJSONError(w, fmt.Errorf("bad remote addr %q: %w", r.RemoteAddr, err), http.StatusBadRequest)
		return
	}

	boxes, err := d.boxer.List(r.Context())
	slog.InfoContext(r.Context(), "handleSandboxConfig", "boxes", boxes, "error", err)
	if err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	for _, box := range boxes {
		if box.Container == nil {
			continue
		}
		ctr, err := d.boxer.ContainerService.Inspect(r.Context(), box.ContainerID)
		if err != nil {
			slog.ErrorContext(r.Context(), "inspect container", "error", err)
			continue
		}
		for _, n := range ctr[0].Networks {
			// Address may be CIDR notation ("192.168.65.2/24") or a plain IP.
			ip := strings.SplitN(n.IPv4Address, "/", 2)[0]
			slog.InfoContext(r.Context(), "handleSandboxConfig checking box network", "n", n, "ip", ip, "callerIP", callerIP)
			if ip == callerIP {
				slog.InfoContext(r.Context(), "handleSandboxConfig matched", "sandbox", box.ID, "callerIP", callerIP)
				writeJSON(w, map[string]any{"domains": box.AllowedDomains})
				return
			}
		}
	}
	slog.InfoContext(r.Context(), "handleSandboxConfig NOT FOUND", "callerIP", callerIP)

	writeJSONError(w, fmt.Errorf("no sandbox found for caller IP %s", callerIP), http.StatusNotFound)
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
func (d *Daemon) StopSandbox(ctx context.Context, id string) error {
	sbox, err := d.boxer.Get(ctx, id)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", id)
	}
	if err := d.stopInnieServer(ctx, id); err != nil {
		return err
	}

	return d.boxer.StopContainer(ctx, sbox)
}

type CreateSandboxOpts struct {
	ID             string   `json:"id,omitempty"`
	CloneFromDir   string   `json:"cloneFromDir,omitempty"`
	ImageName      string   `json:"imageName,omitempty"`
	EnvFile        string   `json:"envFile,omitempty"`
	Cloner         string   `json:"cloner,omitempty"`
	AllowedDomains []string `json:"allowedDomains,omitempty"`
	Volumes        []string `json:"volumes,omitempty"`
}

// createSandbox creates a new sandbox and starts its container.
func (d *Daemon) createSandbox(ctx context.Context, opts CreateSandboxOpts) (*sandtypes.Box, error) {
	agentType := opts.Cloner
	if agentType == "" {
		agentType = "default"
	}
	slog.InfoContext(ctx, "CreateSandbox", "agentType", agentType)

	sbox, err := d.boxer.NewSandbox(ctx, agentType, opts.ID, opts.CloneFromDir, opts.ImageName, opts.EnvFile, opts.AllowedDomains, opts.Volumes)
	if err != nil {
		return nil, err
	}

	ctr, err := d.boxer.GetContainer(ctx, sbox.ContainerID)

	if ctr == nil {
		socketPath := filepath.Join(d.AppBaseDir, "containersockets", opts.ID)
		unixListener, err := net.Listen("unix", socketPath)
		if err != nil {
			return nil, fmt.Errorf("createSandbox couldn't open container socket %s: %w", socketPath, err)
		}
		go d.serveInnieSocket(ctx, opts.ID, unixListener)

		err = d.boxer.CreateContainer(ctx, sbox)
		if err != nil {
			return nil, err
		}
		if err := d.boxer.UpdateContainerID(ctx, sbox, sbox.ContainerID); err != nil {
			return nil, err
		}
		ctr, err = d.boxer.GetContainer(ctx, sbox.ContainerID)
		if err != nil || ctr == nil {
			return nil, fmt.Errorf("failed to get container after creation")
		}
	}

	if ctr.Status != "running" {
		err := d.boxer.StartContainer(ctx, sbox)
		if err != nil {
			return nil, err
		}
	}
	sbox.Container = ctr
	return sbox, nil
}

// ListSandboxes returns all sandboxes.
func (d *Daemon) ListSandboxes(ctx context.Context) ([]sandtypes.Box, error) {
	return d.boxer.List(ctx)
}

// GetSandbox retrieves a sandbox by ID.
func (d *Daemon) GetSandbox(ctx context.Context, id string) (*sandtypes.Box, error) {
	return d.boxer.Get(ctx, id)
}

// RemoveSandbox removes a single sandbox.
func (d *Daemon) RemoveSandbox(ctx context.Context, id string) error {
	sbox, err := d.boxer.Get(ctx, id)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", id)
	}
	if err := d.stopInnieServer(ctx, id); err != nil {
		return err
	}

	return d.boxer.Cleanup(ctx, sbox)
}
