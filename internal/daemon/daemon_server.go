package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/banksean/sand/internal/daemon/daemonpb"
	"github.com/banksean/sand/internal/daemon/internal/boxer"
	"github.com/banksean/sand/internal/runtimepaths"
	"github.com/banksean/sand/internal/sandboxlog"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/version"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
)

const (
	DefaultGRPCSocketFile = "sandd.grpc.sock"
	defaultHTTPSocketFile = "sandd.sock"
	defaultLockFile       = "sandd.lock"
	envMCPEnable          = "SAND_MCP"
	socketFileMode        = 0o666
)

type Daemon struct {
	AppBaseDir     string
	GRPCSocketPath string
	LocalDomain    string
	LogFile        string

	hostMCP *HostMCP
	boxer   *boxer.Boxer

	outieGRPCListener net.Listener

	innieServersMu sync.Mutex
	// TODO: sync with container lifecycle for cases like restarting sandd with sandboxes running.
	// Note that this is particularly tricky because apple/container appears to treat these volume
	// mounted socket files as special cases by using some kind of VSOCK magic. Whatever it is,
	// it doesn't appear to get re-established by apple/container automatically.
	//
	// For now, just be aware that restarting the daemon means that any running sandbox containers
	// won't be able to talk to sandd on the host machine any more (unless they restart the sandbox
	// manually, after the daemon restart).
	innieServers     map[string]*http.Server
	innieGRPCServers map[string]*grpc.Server

	lockFile *os.File
	shutdown chan any
	grpcSrv  *grpc.Server
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
		AppBaseDir:     appBaseDir,
		GRPCSocketPath: filepath.Join(appBaseDir, DefaultGRPCSocketFile),
		LocalDomain:    localDomain,
		hostMCP: &HostMCP{
			ChromeDevToolsPort: 9222,
			ChromeUserDataDir:  "/tmp/chrome-profile-stable",
		},
		innieServers:     map[string]*http.Server{},
		innieGRPCServers: map[string]*grpc.Server{},
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
	slog.InfoContext(ctx, "Daemon.startDaemonServer", "grpcSocketPath", d.GRPCSocketPath)
	// Remove old socket if it exists
	os.Remove(filepath.Join(d.AppBaseDir, defaultHTTPSocketFile))
	os.Remove(d.GRPCSocketPath)

	grpcListener, err := listenUnixSocket(d.GRPCSocketPath)
	if err != nil {
		return err
	}
	d.outieGRPCListener = grpcListener

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

	go d.serveOutieGRPCSocket(ctx)

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
	if d.outieGRPCListener != nil {
		d.outieGRPCListener.Close()
	}
	if d.grpcSrv != nil {
		d.grpcSrv.Stop()
	}

	if d.hostMCP != nil {
		if err := d.hostMCP.Cleanup(ctx); err != nil {
			slog.ErrorContext(ctx, "Daemon.Shutdown: MCP.Cleanup", "error", err)
		}
	}

	// Remove socket file. This mail fail in many cases since closing the listener appears
	// to remove the file automatically on MacOS. Therefore we ignore the err return value.
	os.Remove(filepath.Join(d.AppBaseDir, defaultHTTPSocketFile))
	os.Remove(d.GRPCSocketPath)

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

func (d *Daemon) serveOutieGRPCSocket(ctx context.Context) {
	server := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	daemonpb.RegisterDaemonServiceServer(server, &daemonGRPCServer{daemon: d})
	d.grpcSrv = server

	slog.InfoContext(ctx, "Daemon.serveUnixSocketGRPC starting up", "socketPath", d.GRPCSocketPath)
	if err := server.Serve(d.outieGRPCListener); err != nil && !isExpectedServeClose(err) {
		slog.ErrorContext(ctx, "Daemon.serveUnixSocketGRPC", "error", err)
	}
}

type daemonGRPCServer struct {
	daemonpb.UnimplementedDaemonServiceServer
	daemon *Daemon
}

func (s *daemonGRPCServer) Ping(context.Context, *daemonpb.PingRequest) (*daemonpb.PingResponse, error) {
	return &daemonpb.PingResponse{Status: "pong"}, nil
}

func (s *daemonGRPCServer) Version(context.Context, *daemonpb.VersionRequest) (*daemonpb.VersionResponse, error) {
	info := version.Get()
	return versionInfoToProto(info), nil
}

func versionInfoToProto(info version.Info) *daemonpb.VersionResponse {
	resp := &daemonpb.VersionResponse{
		GitRepo:   info.GitRepo,
		GitBranch: info.GitBranch,
		GitCommit: info.GitCommit,
		BuildTime: info.BuildTime,
	}
	if info.BuildInfo != nil {
		buildInfoJSON, err := json.Marshal(info.BuildInfo)
		if err == nil {
			resp.BuildInfoJson = buildInfoJSON
		}
	}
	return resp
}

func slogHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.InfoContext(r.Context(), "http request", "url", r.URL.String())
		h(w, r)
	}
}

func fromSandbox(sandboxID string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := sandboxlog.WithSandboxID(r.Context(), sandboxID)
		r = r.WithContext(ctx)
		slog.InfoContext(ctx, "http request", "url", r.URL.String())
		h(w, r)
	}
}

func (d *Daemon) stopInnieServer(ctx context.Context, id string) error {
	d.innieServersMu.Lock()
	defer d.innieServersMu.Unlock()
	if srv, ok := d.innieServers[id]; ok {
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("StopSandbox could not shut down sandbox's http server: %w", err)
		}
		delete(d.innieServers, id)
	}
	if srv, ok := d.innieGRPCServers[id]; ok {
		srv.Stop()
		delete(d.innieGRPCServers, id)
	}
	return nil
}

func (d *Daemon) serveInnieHttpSocket(ctx context.Context, sandboxID string, unixListener net.Listener) {
	ctx = sandboxlog.WithSandboxID(ctx, sandboxID)
	mux := http.NewServeMux()

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

	slog.InfoContext(ctx, "Daemon.serveInnieSocket starting up")
	err := server.Serve(unixListener)
	if err != nil && !isExpectedServeClose(err) {
		slog.ErrorContext(ctx, "Daemon.serveInnieSocket", "error", err)
	}
}

func (d *Daemon) serveInnieGRPCSocket(ctx context.Context, sandboxID string, unixListener net.Listener) {
	ctx = sandboxlog.WithSandboxID(ctx, sandboxID)
	server := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithSpanAttributes(attribute.String("sand.sandbox_id", sandboxID)),
		)),
		grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(sandboxlog.WithSandboxID(ctx, sandboxID), req)
		}),
		grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, &contextServerStream{
				ServerStream: stream,
				ctx:          sandboxlog.WithSandboxID(stream.Context(), sandboxID),
			})
		}),
	)
	daemonpb.RegisterDaemonServiceServer(server, &daemonGRPCServer{daemon: d})

	d.innieServersMu.Lock()
	d.innieGRPCServers[sandboxID] = server
	d.innieServersMu.Unlock()

	defer unixListener.Close()

	slog.InfoContext(ctx, "Daemon.serveInnieGRPCSocket starting up")
	if err := server.Serve(unixListener); err != nil && !isExpectedServeClose(err) {
		slog.ErrorContext(ctx, "Daemon.serveInnieGRPCSocket", "error", err)
	}
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context {
	return s.ctx
}

func isExpectedServeClose(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, http.ErrServerClosed) ||
		errors.Is(err, grpc.ErrServerStopped)
}

func listenUnixSocket(socketPath string) (net.Listener, error) {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, socketFileMode); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
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
func (d *Daemon) StopSandbox(ctx context.Context, name string) error {
	sbox, err := d.boxer.Get(ctx, name)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", name)
	}
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)
	if err := d.stopInnieServer(ctx, sbox.ID); err != nil {
		return err
	}

	return d.boxer.StopContainer(ctx, sbox)
}

func (d *Daemon) createContainerSocket(ctx context.Context, id string) (net.Listener, error) {
	ctx = sandboxlog.WithSandboxID(ctx, id)
	socketsDir := runtimepaths.ContainerHTTPSocketDir()
	slog.InfoContext(ctx, "Daemon.createContainerSocket", "socketsDir", socketsDir)
	if err := os.MkdirAll(socketsDir, 0o777); err != nil {
		return nil, err
	}
	socketPath := runtimepaths.ContainerHTTPSocketPath(id)
	slog.InfoContext(ctx, "Daemon.createContainerSocket", "socketPath", socketPath)
	// Don't care about errors, e.g. socketPath already does not exist:
	os.Remove(socketPath)
	unixListener, err := listenUnixSocket(socketPath)
	if err != nil {
		return nil, fmt.Errorf("createSandbox couldn't open container socket %s: %w", socketPath, err)
	}
	return unixListener, nil
}

func (d *Daemon) createContainerGRPCSocket(ctx context.Context, id string) (net.Listener, error) {
	ctx = sandboxlog.WithSandboxID(ctx, id)
	socketsDir := runtimepaths.ContainerGRPCSocketDir()
	slog.InfoContext(ctx, "Daemon.createContainerGRPCSocket", "socketsDir", socketsDir)
	if err := os.MkdirAll(socketsDir, 0o777); err != nil {
		return nil, err
	}
	socketPath := runtimepaths.ContainerGRPCSocketPath(id)
	slog.InfoContext(ctx, "Daemon.createContainerGRPCSocket", "socketPath", socketPath)
	// Don't care about errors, e.g. socketPath already does not exist:
	os.Remove(socketPath)
	unixListener, err := listenUnixSocket(socketPath)
	if err != nil {
		return nil, fmt.Errorf("createSandbox couldn't open container grpc socket %s: %w", socketPath, err)
	}
	return unixListener, nil
}

func (d *Daemon) createContainerSockets(ctx context.Context, id string) (httpListener net.Listener, grpcListener net.Listener, err error) {
	httpListener, err = d.createContainerSocket(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	grpcListener, err = d.createContainerGRPCSocket(ctx, id)
	if err != nil {
		_ = httpListener.Close()
		return nil, nil, err
	}
	return httpListener, grpcListener, nil
}

// StartSandbox starts a single sandbox container.
func (d *Daemon) StartSandbox(ctx context.Context, opts StartSandboxOpts) error {
	name := opts.Name
	if name == "" {
		name = opts.ID
	}
	sbox, err := d.boxer.Get(ctx, name)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", name)
	}
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)

	if sbox.OriginalGitDetails != nil && sbox.OriginalGitDetails.Commit != "" {
		if _, err := d.boxer.SyncHostGitMirror(ctx, sbox); err != nil {
			return err
		}
	}

	needsRecreate := false
	ctr, err := d.boxer.GetContainer(ctx, sbox.ContainerID)
	if err != nil {
		return err
	}
	sbox.Container = ctr
	if opts.SSHAgent {
		if ctr != nil && !ctr.Configuration.SSH {
			if ctr.Status == "running" {
				return fmt.Errorf("sandbox %s is already running without ssh-agent forwarding", sbox.ID)
			}
			needsRecreate = true
		}
	}

	httpListener, grpcListener, err := d.createContainerSockets(ctx, sbox.ID)
	if err != nil {
		return err
	}

	if needsRecreate {
		if err := d.boxer.RecreateContainer(ctx, sbox, true); err != nil {
			_ = httpListener.Close()
			_ = grpcListener.Close()
			return err
		}
	}

	go d.serveInnieHttpSocket(ctx, sbox.ID, httpListener)
	go d.serveInnieGRPCSocket(ctx, sbox.ID, grpcListener)

	var startErr error
	if needsRecreate {
		startErr = d.boxer.StartNewContainer(ctx, sbox, nil)
	} else {
		startErr = d.boxer.StartExistingContainer(ctx, sbox)
	}
	if startErr != nil {
		_ = httpListener.Close()
		_ = grpcListener.Close()
		return startErr
	}
	return nil
}

func (d *Daemon) SyncHostGitMirror(ctx context.Context, name string) (string, error) {
	sbox, err := d.boxer.Get(ctx, name)
	if err != nil {
		return "", err
	}
	if sbox == nil {
		return "", fmt.Errorf("sandbox not found: %s", name)
	}
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)
	return d.boxer.SyncHostGitMirror(ctx, sbox)
}

type CreateSandboxOpts struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	CloneFromDir string `json:"cloneFromDir,omitempty"`
	ProfileName  string `json:"profileName,omitempty"`
	ImageName    string `json:"imageName,omitempty"`
	EnvFile      string `json:"envFile,omitempty"`
	Agent        string `json:"agent,omitempty"`
	SSHAgent     bool   `json:"sshAgent,omitempty"`
	Username     string `json:"username,omitempty"`
	Uid          string `json:"uid,omitempty"`

	AllowedDomains []string                    `json:"allowedDomains,omitempty"`
	Volumes        []string                    `json:"volumes,omitempty"`
	SharedCaches   sandtypes.SharedCacheConfig `json:"sharedCaches,omitempty"`
	CPUs           int                         `json:"cpus"`
	Memory         int                         `json:"memory"`
}

type StartSandboxOpts struct {
	Name     string `json:"name,omitempty"`
	ID       string `json:"id,omitempty"`
	SSHAgent bool   `json:"sshAgent,omitempty"`
}

// createSandbox creates a new sandbox and starts its container.
func (d *Daemon) createSandbox(ctx context.Context, opts CreateSandboxOpts, progress io.Writer) (*sandtypes.Box, error) {
	if opts.Name == "" {
		opts.Name = opts.ID
	}
	if opts.ID == "" {
		opts.ID = uuid.NewString()
	}
	ctx = sandboxlog.WithSandboxID(ctx, opts.ID)
	agentType := opts.Agent
	if agentType == "" {
		agentType = "default"
	}
	profileName := opts.ProfileName
	if profileName == "" {
		profileName = sandtypes.DefaultProfileName
	}
	slog.InfoContext(ctx, "createSandbox", "agentType", agentType, "opts", opts)

	if err := d.validateSelectableAgent(opts.Agent); err != nil {
		return nil, err
	}

	sbox, err := d.boxer.NewSandbox(ctx, boxer.NewSandboxOpts{
		AgentType:      agentType,
		ID:             opts.ID,
		Name:           opts.Name,
		HostWorkDir:    opts.CloneFromDir,
		ProfileName:    profileName,
		ImageName:      opts.ImageName,
		EnvFile:        opts.EnvFile,
		Username:       opts.Username,
		Uid:            opts.Uid,
		AllowedDomains: opts.AllowedDomains,
		Volumes:        opts.Volumes,
		SharedCaches:   opts.SharedCaches,
		CPUs:           opts.CPUs,
		Memory:         opts.Memory,
		LocalDomain:    d.LocalDomain,
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "createSandbox", "sbox", sbox)

	// TODO: move all this container creation logic into boxer.StartContainer.
	ctr, err := d.boxer.GetContainer(ctx, sbox.ContainerID)

	if ctr == nil {
		unixListener, grpcListener, err := d.createContainerSockets(ctx, sbox.ID)
		if err != nil {
			return nil, err
		}
		go d.serveInnieHttpSocket(ctx, sbox.ID, unixListener)
		go d.serveInnieGRPCSocket(ctx, sbox.ID, grpcListener)

		err = d.boxer.CreateContainer(ctx, sbox, opts.SSHAgent)
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
		err := d.boxer.StartNewContainer(ctx, sbox, progress)
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

// GetSandbox retrieves an active sandbox by user-facing name.
func (d *Daemon) GetSandbox(ctx context.Context, name string) (*sandtypes.Box, error) {
	return d.boxer.Get(ctx, name)
}

func (d *Daemon) LogSandbox(ctx context.Context, name string, w io.Writer) error {
	sbox, err := d.boxer.Get(ctx, name)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", name)
	}
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)
	return copySandboxLog(d.LogFile, sbox.ID, w)
}

// RemoveSandbox soft-deletes a single active sandbox by name.
func (d *Daemon) RemoveSandbox(ctx context.Context, name string) error {
	sbox, err := d.boxer.Get(ctx, name)
	if err != nil {
		return err
	}
	if sbox == nil {
		return fmt.Errorf("sandbox not found: %s", name)
	}
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)
	if err := d.stopInnieServer(ctx, sbox.ID); err != nil {
		return err
	}
	// Remove the container socket, if there is one.
	socketPath := runtimepaths.ContainerHTTPSocketPath(sbox.ID)
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			return err
		}
	}
	grpcSocketPath := runtimepaths.ContainerGRPCSocketPath(sbox.ID)
	if _, err := os.Stat(grpcSocketPath); err == nil {
		if err := os.Remove(grpcSocketPath); err != nil {
			return err
		}
	}
	return d.boxer.SoftDelete(ctx, sbox)
}
