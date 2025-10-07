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
	"syscall"
	"time"
)

const (
	defaultSocketFile = "sandmux.sock"
	defaultLockFile   = "sandmux.lock"
)

type Mux struct {
	AppBaseDir string

	listener net.Listener
	lockFile *os.File
	shutdown chan any
}

func NewMux(appBaseDir string) *Mux {
	return &Mux{
		AppBaseDir: appBaseDir,
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

		// ... other commands
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

func (m *MuxClient) Shutdown(ctx context.Context) error {
	socketPath := filepath.Join(m.Mux.AppBaseDir, defaultSocketFile)
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("daemon not running")
	}
	defer conn.Close()

	// Send shutdown command
	cmd := MuxCommand{Type: "shutdown"}
	if err := json.NewEncoder(conn).Encode(cmd); err != nil {
		return err
	}

	// Read acknowledgment
	var resp MuxResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}

	if resp.Status != "ok" {
		return fmt.Errorf("shutdown failed: %s", resp.Error)
	}

	// Wait briefly to verify daemon stopped
	time.Sleep(200 * time.Millisecond)

	// Verify socket is gone
	if _, err := os.Stat(socketPath); err == nil {
		return fmt.Errorf("daemon may not have shut down cleanly")
	}

	return nil
}

func EnsureDaemon(appBaseDir string) error {
	socketPath := filepath.Join(appBaseDir, defaultSocketFile)

	// Try to connect to existing daemon
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err == nil {
		conn.Close()
		return nil // Daemon already running
	}

	// Start daemon in background
	cmd := exec.Command(os.Args[0], "daemon")
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
