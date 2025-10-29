package sand

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

type HostMCP struct {
	ChromeDevToolsPort int
	ChromeUserDataDir  string // e.g. /tmp/chrome-profile-stable

	chromeProcess *os.Process
}

// StartHostServices starts MCP-supporting processes on the host machine. Since these processes
// are not specific to a particular sandbox, they are shared across sandboxes.
func (m *HostMCP) StartHostServices(ctx context.Context) error {
	cmd := exec.Command("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		fmt.Sprintf("--remote-debugging-port=%d", m.ChromeDevToolsPort),
		"--user-data-dir="+m.ChromeUserDataDir,
		"--no-startup-window",
	)
	slog.InfoContext(ctx, "HostMCP.StartHostServices", "cmd", strings.Join(cmd.Args, " "))
	if err := cmd.Start(); err != nil {
		slog.ErrorContext(ctx, "HostMCP.StartHostServices", "error", err)
		return err
	}
	m.chromeProcess = cmd.Process
	return nil
}

func (m *HostMCP) Cleanup(ctx context.Context) error {
	if m.chromeProcess == nil {
		return nil
	}
	return m.chromeProcess.Kill()
}
