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

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/version"
)

const (
	DefaultSocketFile = "sandd.sock"
	defaultLockFile   = "sandd.lock"
	containerIDKey    = "containerID"
	envMCPEnable      = "SAND_MCP"
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
	// Note that this is particularly tricky because apple/container appears to treat these volume
	// mounted socket files as special cases by using some kind of VSOCK magic. Whatever it is,
	// it doesn't appear to get re-established by apple/container automatically.
	//
	// For now, just be aware that restarting the daemon means that any running sandbox containers
	// won't be able to talk to sandd on the host machine any more (unless they restart the sandbox
	// manually, after the daemon restart).
	innieServers map[string]*http.Server

	lockFile *os.File
	shutdown chan any
	httpSrv  http.Server
}

// NewDaemonWithBoxer creates a Daemon with a pre-built Boxer injected.
// The boxer is used as-is; the daemon will not create a new one at startup.
// This is the recommended constructor for tests and for callers that need
// control over the Boxer's dependencies.
func NewDaemonWithBoxer(appBaseDir, localDomain string, b *boxer.Boxer) *Daemon {
	d := NewDaemon(appBaseDir, localDomain)
	d.boxer = b
	return d
}

func NewDaemon(appBaseDir, localDomain string) *Daemon {
	return &Daemon{
		AppBaseDir:  appBaseDir,
		SocketPath:  filepath.Join(appBaseDir, DefaultSocketFile),
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
	slog.InfoContext(ctx, "Daemon.ServeUnix", "daemon", d, "pid", os.Getpid(), "lockFilePath", lockFilePath)
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
	if d.boxer == nil {
		sber, err := boxer.NewBoxer(d.AppBaseDir, d.LocalDomain, os.Stderr)
		if err != nil {
			return err
		}
		d.boxer = sber
	}
	if err := d.boxer.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync Boxer db with current environment state: %v\n", err)
	}
	// Handle cleanup on shutdown
	go d.waitForShutdown(ctx)

	// Start unix domain socket HTTP server
	go d.serveOutieSocket(ctx)

	if os.Getenv(envMCPEnable) != "" {
		go func() {
			if err := d.hostMCP.StartHostServices(ctx); err != nil {
				slog.ErrorContext(ctx, "startDaemonServer MCP.StartMCPDeps", "error", err)
			}
		}()
	}
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
	mux.HandleFunc("/shutdown", slogHandler(d.handleShutdown))
	mux.HandleFunc("/ping", slogHandler(d.handlePing))
	mux.HandleFunc("/version", slogHandler(d.handleVersion))
	mux.HandleFunc("/list", slogHandler(d.handleList))
	mux.HandleFunc("/get", slogHandler(d.handleGet))
	mux.HandleFunc("/remove", slogHandler(d.handleRemove))
	mux.HandleFunc("/stop", slogHandler(d.handleStop))
	mux.HandleFunc("/start", slogHandler(d.handleStart))
	mux.HandleFunc("/create", slogHandler(d.handleCreate))
	mux.HandleFunc("/export", slogHandler(d.handleExport))
	mux.HandleFunc("/stats", slogHandler(d.handleStats))

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
	if id, ok := ctx.Value(containerIDKey).(string); ok {
		return id, nil
	}
	var req IDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", err
	}
	return req.ID, nil
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
	mux.HandleFunc("/export", slogHandler(fromSandbox(sandboxID, d.handleExport)))
	mux.HandleFunc("/stats", slogHandler(fromSandbox(sandboxID, d.handleStats)))

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
	json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
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

	writeJSON(w, StatusResponse{Status: "ok"})

	// Shutdown daemon after response is sent
	go func() {
		time.Sleep(100 * time.Millisecond)
		d.Shutdown(r.Context())
	}()
}

func (d *Daemon) handleExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var args ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}
	ctxID, ok := ctx.Value(containerIDKey).(string)
	if ok {
		args.ID = ctxID
	}

	d.boxer.ContainerService.Export(ctx, &options.ExportContainer{Image: args.ImageName}, args.ID)
}

func (d *Daemon) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, StatusResponse{Status: "pong"})
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
		slog.ErrorContext(r.Context(), "Daemon.handleGet d.GetSandbox", "error", err)
		writeJSONError(w, fmt.Errorf("couldn't get sandbox ID %s", sandboxID), http.StatusInternalServerError)
		return
	}
	if sbox == nil {
		slog.ErrorContext(r.Context(), "Daemon.handleGet d.GetSandbox returned nil", "id", sandboxID)
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
		slog.ErrorContext(r.Context(), "Daemon.handleGet boxer.GetContainer", "error", err)
		http.Error(w, "couldn't get container", http.StatusInternalServerError)
		return
	}
	sbox.Container = ctr
	writeJSON(w, sbox)
}

func (d *Daemon) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var args StatsRequest
	slog.InfoContext(r.Context(), "Daemon.handleStats json decode request", "args", args)

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		slog.ErrorContext(r.Context(), "Daemon.handleStats json decode request", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	stats, err := d.boxer.GetContainerStats(r.Context(), args.IDs...)
	if err != nil {
		slog.ErrorContext(r.Context(), "Daemon.handleStats ContainerService.Stats", "error", err)
		http.Error(w, "couldn't get container stats", http.StatusInternalServerError)
	}
	writeJSON(w, stats)
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
	writeJSON(w, StatusResponse{Status: "ok"})
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
	writeJSON(w, StatusResponse{Status: "ok"})
}

func (d *Daemon) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sandboxID, err := sandboxIDOf(r)
	if err != nil {
		writeJSONError(w, err, http.StatusBadRequest)
		return
	}

	if err := d.StartSandbox(r.Context(), sandboxID); err != nil {
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, StatusResponse{Status: "ok"})
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
	vscCmd := exec.Command("code", "--remote", fmt.Sprintf("ssh-remote+%s", hostname), "/app", "-n")
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
				writeJSON(w, SandboxConfigResponse{Domains: box.AllowedDomains})
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

func (d *Daemon) createContainerSocket(ctx context.Context, id string) (net.Listener, error) {
	socketsDir := filepath.Join(d.AppBaseDir, "containersockets")
	slog.InfoContext(ctx, "Daemon.createContainerSocket", "socketsDir", socketsDir)
	if err := os.MkdirAll(socketsDir, 0o777); err != nil {
		return nil, err
	}
	socketPath := filepath.Join(socketsDir, id)
	slog.InfoContext(ctx, "Daemon.createContainerSocket", "socketPath", socketPath)
	// Don't care about errors, e.g. socketPath already does not exist:
	os.Remove(socketPath)
	unixListener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("createSandbox couldn't open container socket %s: %w", socketPath, err)
	}
	return unixListener, nil
}

// StartSandbox starts a single sandbox container.
func (d *Daemon) StartSandbox(ctx context.Context, id string) error {
	sbox, err := d.boxer.Get(ctx, id)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", id)
	}
	unixListener, err := d.createContainerSocket(ctx, id)
	if err != nil {
		return err
	}
	go d.serveInnieSocket(ctx, id, unixListener)

	return d.boxer.StartExistingContainer(ctx, sbox)
}

type CreateSandboxOpts struct {
	ID           string `json:"id,omitempty"`
	CloneFromDir string `json:"cloneFromDir,omitempty"`
	ImageName    string `json:"imageName,omitempty"`
	EnvFile      string `json:"envFile,omitempty"`
	Agent        string `json:"agent,omitempty"`
	Username     string `json:"username,omitempty"`
	Uid          string `json:"uid,omitempty"`

	AllowedDomains []string `json:"allowedDomains,omitempty"`
	Volumes        []string `json:"volumes,omitempty"`
	CPUs           int      `json:"cpus"`
	Memory         int      `json:"memory"`
}

// createSandbox creates a new sandbox and starts its container.
func (d *Daemon) createSandbox(ctx context.Context, opts CreateSandboxOpts) (*sandtypes.Box, error) {
	agentType := opts.Agent
	if agentType == "" {
		agentType = "default"
	}
	slog.InfoContext(ctx, "createSandbox", "agentType", agentType, "opts", opts)

	sbox, err := d.boxer.NewSandbox(ctx, boxer.NewSandboxOpts{
		AgentType:      agentType,
		ID:             opts.ID,
		HostWorkDir:    opts.CloneFromDir,
		ImageName:      opts.ImageName,
		EnvFile:        opts.EnvFile,
		Username:       opts.Username,
		Uid:            opts.Uid,
		AllowedDomains: opts.AllowedDomains,
		Volumes:        opts.Volumes,
		CPUs:           opts.CPUs,
		Memory:         opts.Memory,
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "createSandbox", "sbox", sbox)

	// TODO: move all this container creation logic into boxer.StartContainer.
	ctr, err := d.boxer.GetContainer(ctx, sbox.ContainerID)

	if ctr == nil {
		unixListener, err := d.createContainerSocket(ctx, opts.ID)
		if err != nil {
			return nil, err
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
		err := d.boxer.StartNewContainer(ctx, sbox)
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
	// Remove the container socket, if there is one.
	socketPath := filepath.Join(d.AppBaseDir, "containersockets", id)
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			return err
		}
	}
	return d.boxer.Cleanup(ctx, sbox)
}
