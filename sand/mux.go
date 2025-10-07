package sand

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
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
		if err := os.Chmod(socketPath, 0600); err != nil {
			return err
		}
	}

	m.listener = listener
	m.shutdown = make(chan any)

	// Handle cleanup on shutdown
	go m.handleShutdown(ctx)

	// Accept connections
	go m.acceptConnections(ctx)

	// Wait for shutdown signal
	<-m.shutdown

	return nil
}

func (m *Mux) handleShutdown(ctx context.Context) {
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

func (m *Mux) acceptConnections(ctx context.Context) {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return // Listener closed
		}
		go m.handleConnection(ctx, conn)
	}
}

type MuxCommand struct {
	Type string         `json:"type"`
	Args map[string]any `json:"args,omitempty"`
}

type MuxResponse struct {
	Status string `json:"status"`
	Data   any    `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (m *Mux) handleConnection(ctx context.Context, conn net.Conn) {
	slog.InfoContext(ctx, "Mux.handleConnection", "conn", conn)
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	var cmd MuxCommand
	if err := decoder.Decode(&cmd); err != nil {
		return
	}

	switch cmd.Type {
	case "shutdown":
		// Send acknowledgment before shutting down
		json.NewEncoder(conn).Encode(MuxResponse{Status: "ok"})
		conn.Close()

		// Shutdown daemon
		go func() {
			time.Sleep(100 * time.Millisecond) // Give response time to send
			m.Shutdown(ctx)
		}()

	case "ping":
		json.NewEncoder(conn).Encode(MuxResponse{Status: "pong"})

	case "list":
		m.handleList(ctx, conn, cmd)

	case "get":
		m.handleGet(ctx, conn, cmd)

	case "remove":
		m.handleRemove(ctx, conn, cmd)

	case "stop":
		m.handleStop(ctx, conn, cmd)

	case "create":
		m.handleCreate(ctx, conn, cmd)

	default:
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: "unknown command type"})
	}
}

func acquireLock(lockFile string) (*os.File, error) {
	file, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0600)
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
	Mux *Mux
}

func (m *Mux) NewClient(ctx context.Context) (*MuxClient, error) {
	return &MuxClient{Mux: m}, nil
}

func (m *MuxClient) sendCommand(ctx context.Context, cmd MuxCommand) (*MuxResponse, error) {
	socketPath := filepath.Join(m.Mux.AppBaseDir, defaultSocketFile)
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("daemon not running")
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(cmd); err != nil {
		return nil, err
	}

	var resp MuxResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func (m *MuxClient) Shutdown(ctx context.Context) error {
	resp, err := m.sendCommand(ctx, MuxCommand{Type: "shutdown"})
	if err != nil {
		return err
	}

	if resp.Status != "ok" {
		return fmt.Errorf("shutdown failed: %s", resp.Error)
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
	resp, err := m.sendCommand(ctx, MuxCommand{Type: "list"})
	if err != nil {
		return nil, err
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("list failed: %s", resp.Error)
	}

	// The Data field contains []Box but as []interface{}
	// We need to re-marshal and unmarshal to get the proper type
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, err
	}

	var boxes []Box
	if err := json.Unmarshal(data, &boxes); err != nil {
		return nil, err
	}

	return boxes, nil
}

func (m *MuxClient) GetSandbox(ctx context.Context, id string) (*Box, error) {
	resp, err := m.sendCommand(ctx, MuxCommand{
		Type: "get",
		Args: map[string]any{"id": id},
	})
	if err != nil {
		return nil, err
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("get failed: %s", resp.Error)
	}

	if resp.Data == nil {
		return nil, nil
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, err
	}

	var box Box
	if err := json.Unmarshal(data, &box); err != nil {
		return nil, err
	}

	return &box, nil
}

func (m *MuxClient) RemoveSandbox(ctx context.Context, id string) error {
	resp, err := m.sendCommand(ctx, MuxCommand{
		Type: "remove",
		Args: map[string]any{"id": id},
	})
	if err != nil {
		return err
	}

	if resp.Status != "ok" {
		return fmt.Errorf("remove failed: %s", resp.Error)
	}

	return nil
}

func (m *MuxClient) StopSandbox(ctx context.Context, id string) error {
	resp, err := m.sendCommand(ctx, MuxCommand{
		Type: "stop",
		Args: map[string]any{"id": id},
	})
	if err != nil {
		return err
	}

	if resp.Status != "ok" {
		return fmt.Errorf("stop failed: %s", resp.Error)
	}

	return nil
}

func (m *MuxClient) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*Box, error) {
	resp, err := m.sendCommand(ctx, MuxCommand{
		Type: "create",
		Args: map[string]any{
			"id":            opts.ID,
			"cloneFromDir":  opts.CloneFromDir,
			"imageName":     opts.ImageName,
			"dockerFileDir": opts.DockerFileDir,
			"envFile":       opts.EnvFile,
		},
	})
	if err != nil {
		return nil, err
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("create failed: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, err
	}

	var box Box
	if err := json.Unmarshal(data, &box); err != nil {
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
	ID            string
	CloneFromDir  string
	ImageName     string
	DockerFileDir string
	EnvFile       string
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

// Handler functions for socket commands

func (m *Mux) handleList(ctx context.Context, conn net.Conn, cmd MuxCommand) {
	boxes, err := m.ListSandboxes(ctx)
	if err != nil {
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: err.Error()})
		return
	}
	json.NewEncoder(conn).Encode(MuxResponse{Status: "ok", Data: boxes})
}

func (m *Mux) handleGet(ctx context.Context, conn net.Conn, cmd MuxCommand) {
	id, ok := cmd.Args["id"].(string)
	if !ok {
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: "missing or invalid id"})
		return
	}

	sbox, err := m.GetSandbox(ctx, id)
	if err != nil {
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: err.Error()})
		return
	}
	json.NewEncoder(conn).Encode(MuxResponse{Status: "ok", Data: sbox})
}

func (m *Mux) handleRemove(ctx context.Context, conn net.Conn, cmd MuxCommand) {
	id, ok := cmd.Args["id"].(string)
	if !ok {
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: "missing or invalid id"})
		return
	}

	err := m.RemoveSandbox(ctx, id)
	if err != nil {
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: err.Error()})
		return
	}
	json.NewEncoder(conn).Encode(MuxResponse{Status: "ok"})
}

func (m *Mux) handleStop(ctx context.Context, conn net.Conn, cmd MuxCommand) {
	id, ok := cmd.Args["id"].(string)
	if !ok {
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: "missing or invalid id"})
		return
	}

	err := m.StopSandbox(ctx, id)
	if err != nil {
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: err.Error()})
		return
	}
	json.NewEncoder(conn).Encode(MuxResponse{Status: "ok"})
}

func (m *Mux) handleCreate(ctx context.Context, conn net.Conn, cmd MuxCommand) {
	var opts CreateSandboxOpts
	if id, ok := cmd.Args["id"].(string); ok {
		opts.ID = id
	}
	if cloneFromDir, ok := cmd.Args["cloneFromDir"].(string); ok {
		opts.CloneFromDir = cloneFromDir
	}
	if imageName, ok := cmd.Args["imageName"].(string); ok {
		opts.ImageName = imageName
	}
	if dockerFileDir, ok := cmd.Args["dockerFileDir"].(string); ok {
		opts.DockerFileDir = dockerFileDir
	}
	if envFile, ok := cmd.Args["envFile"].(string); ok {
		opts.EnvFile = envFile
	}

	sbox, err := m.CreateSandbox(ctx, opts)
	if err != nil {
		json.NewEncoder(conn).Encode(MuxResponse{Status: "error", Error: err.Error()})
		return
	}
	json.NewEncoder(conn).Encode(MuxResponse{Status: "ok", Data: sbox})
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
	cmd := exec.Command(os.Args[0], "daemon")
	if len(os.Args) > 2 {
		cmd.Args = append(cmd.Args, os.Args[2:]...)
	}
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
