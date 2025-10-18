package sand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type OpenCodeWorkspaceCloner struct {
	baseCloner     WorkspaceCloner
	cloneRoot      string
	terminalWriter io.Writer
}

func NewOpenCodeWorkspaceCloner(baseCloner WorkspaceCloner, appRoot string, terminalWriter io.Writer) WorkspaceCloner {
	return &OpenCodeWorkspaceCloner{
		baseCloner:     baseCloner,
		cloneRoot:      filepath.Join(appRoot, "clones"),
		terminalWriter: terminalWriter,
	}
}

func (c *OpenCodeWorkspaceCloner) Prepare(ctx context.Context, req CloneRequest) (*CloneResult, error) {
	slog.InfoContext(ctx, "OpenCodeWorkspaceCloner.Prepare", "req", req)
	result, err := c.baseCloner.Prepare(ctx, req)
	if err != nil {
		return nil, err
	}

	cloneOpenCodeDir := filepath.Join(c.cloneRoot, req.ID, "dotfiles", ".local", "share", "opencode")
	if err := os.MkdirAll(cloneOpenCodeDir, 0o750); err != nil {
		return nil, err
	}
	if err := c.cloneOpenCodeAuth(ctx, req.HostWorkDir, req.ID); err != nil {
		return nil, err
	}

	return result, nil
}

func (c *OpenCodeWorkspaceCloner) Hydrate(ctx context.Context, box *Box) error {
	slog.InfoContext(ctx, "OpenCodeWorkspaceCloner.Hydrate", "box", box)

	return c.baseCloner.Hydrate(ctx, box)
}

func (c *OpenCodeWorkspaceCloner) userMsg(ctx context.Context, msg string) {
	if c.terminalWriter == nil {
		slog.DebugContext(ctx, "opencode cloner userMsg (no terminalWriter)", "msg", msg)
		return
	}
	fmt.Fprintln(c.terminalWriter, "\033[90m"+msg+"\033[0m")
}

func (c *OpenCodeWorkspaceCloner) cloneOpenCodeConfigDir(ctx context.Context, hostWorkDir, id string) error {
	/*
		TODO: process the opencode config settings from the user environment.
		In order of increasing prescedence:
		- ~/.config/opencode/opencode.json
		- ./opencode.json
		- OPENCODE_CONFIG env var
	*/

	return nil
}

func (c *OpenCodeWorkspaceCloner) cloneOpenCodeAuth(ctx context.Context, cwd, id string) error {
	cloneOpenCodeAuth := filepath.Join(c.cloneRoot, id, "dotfiles", ".local", "share", "opencode", "auth.json")
	openCodeAuth := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "auth.json")

	if _, err := os.Stat(openCodeAuth); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneOpenCodeAuth)
		if err != nil {
			return err
		}
		defer f.Close()
		return nil
	}
	cmd := exec.CommandContext(ctx, "cp", "-Rc", openCodeAuth, cloneOpenCodeAuth)
	slog.InfoContext(ctx, "cloneOpenCodeAuth", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneOpenCodeAuth", "error", err, "output", string(output))
		return err
	}

	return err
}
