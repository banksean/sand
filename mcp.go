package sand

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type MCP struct {
	ChromeDevToolsPort int
	ChromeProcess      *os.Process
}

func (m *MCP) StartMCPDeps(ctx context.Context) error {
	cmd := exec.Command("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		fmt.Sprintf("--remote-debugging-port=%d", m.ChromeDevToolsPort),
		"--user-data-dir=/tmp/chrome-profile-stable", "--silent-launch", "--window-name=Sandbox")
	if err := cmd.Start(); err != nil {
		return err
	}
	m.ChromeProcess = cmd.Process
	return nil
}

func (m *MCP) Cleanup(ctx context.Context) error {
	if m.ChromeProcess == nil {
		return nil
	}
	return m.ChromeProcess.Kill()
}
