package sand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ClaudeWorkspaceCloner struct {
	baseCloner     WorkspaceCloner
	cloneRoot      string
	terminalWriter io.Writer
}

func NewClaudeWorkspaceCloner(baseCloner WorkspaceCloner, appRoot string, terminalWriter io.Writer) WorkspaceCloner {
	return &ClaudeWorkspaceCloner{
		baseCloner:     baseCloner,
		cloneRoot:      filepath.Join(appRoot, "clones"),
		terminalWriter: terminalWriter,
	}
}

func (c *ClaudeWorkspaceCloner) Prepare(ctx context.Context, req CloneRequest) (*CloneResult, error) {
	slog.InfoContext(ctx, "ClaudeWorkspaceCloner.Prepare", "req", req)
	result, err := c.baseCloner.Prepare(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := c.cloneClaudeDir(ctx, req.HostWorkDir, req.ID); err != nil {
		return nil, err
	}

	if err := c.cloneClaudeJSON(ctx, req.HostWorkDir, req.ID); err != nil {
		return nil, err
	}

	return result, nil
}

func (c *ClaudeWorkspaceCloner) Hydrate(ctx context.Context, box *Box) error {
	slog.InfoContext(ctx, "ClaudeWorkspaceCloner.Hydrate", "box", box)

	return c.baseCloner.Hydrate(ctx, box)
}

func (c *ClaudeWorkspaceCloner) userMsg(ctx context.Context, msg string) {
	if c.terminalWriter == nil {
		slog.DebugContext(ctx, "claude cloner userMsg (no terminalWriter)", "msg", msg)
		return
	}
	fmt.Fprintln(c.terminalWriter, "\033[90m"+msg+"\033[0m")
}

func (c *ClaudeWorkspaceCloner) cloneClaudeDir(ctx context.Context, hostWorkDir, id string) error {
	cloneClaude := filepath.Join(c.cloneRoot, id, "dotfiles", ".claude")
	dotClaude := filepath.Join(os.Getenv("HOME"), ".claude")
	if _, err := os.Stat(dotClaude); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneClaude)
		if err != nil {
			return err
		}
		defer f.Close()
		return nil
	}
	cmd := exec.CommandContext(ctx, "cp", "-Rc", dotClaude, cloneClaude)
	slog.InfoContext(ctx, "cloneClaudeDir", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneClaudeDir", "error", err, "output", string(output))
		return err
	}

	projDirName := filepath.Join(cloneClaude, "projects", strings.Replace(hostWorkDir, string(filepath.Separator), "-", -1))
	slog.InfoContext(ctx, "cloneClaudDir: checking for project dir to rename", "projDirName", projDirName)
	if _, err := os.Stat(projDirName); err == nil {
		mvProjDirCmd := exec.CommandContext(ctx, "mv", projDirName, filepath.Join(cloneClaude, "projects", "-app"))
		slog.InfoContext(ctx, "cloneClaudeDir", "mvProjDirCmd", strings.Join(mvProjDirCmd.Args, " "))
		output, err = mvProjDirCmd.CombinedOutput()
		if err != nil {
			slog.InfoContext(ctx, "cloneClaudeDir", "error", err, "output", string(output))
			return err
		}
	}
	return nil
}

func (c *ClaudeWorkspaceCloner) cloneClaudeJSON(ctx context.Context, cwd, id string) error {
	claudeJSON, err := filterClaudeJSON(ctx, cwd)
	if err != nil {
		return err
	}
	clone := filepath.Join(c.cloneRoot, id, "dotfiles", ".claude.json")
	err = os.WriteFile(clone, claudeJSON, 0o700)
	return err
}

func filterClaudeJSON(ctx context.Context, cwd string) ([]byte, error) {
	slog.InfoContext(ctx, "filterClaudeJSON", "cwd", cwd)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	slog.InfoContext(ctx, "filterClaudeJSON", "cwd", cwd, "homeDir", homeDir)

	claudeJSONPath := filepath.Join(homeDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.InfoContext(ctx, "filterClaudeJSON missing file, using default", "path", claudeJSONPath)
			return json.Marshal(map[string]any{"projects": map[string]any{}})
		}
		return nil, fmt.Errorf("failed to read %s: %w", claudeJSONPath, err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	projectsMap := map[string]interface{}{}
	if projects, ok := config["projects"]; ok {
		if cast, ok := projects.(map[string]interface{}); ok {
			projectsMap = cast
		} else {
			slog.InfoContext(ctx, "filterClaudeJSON non-map projects field, resetting", "type", fmt.Sprintf("%T", projects))
		}
	}

	filteredProjects := map[string]interface{}{}
	if value, ok := projectsMap[cwd]; ok {
		filteredProjects["/app"] = value
	}

	config["projects"] = filteredProjects

	slog.InfoContext(ctx, "filterClaudeJSON success")
	result, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filtered JSON: %w", err)
	}

	return result, nil
}
