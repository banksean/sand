package sand

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/banksean/sand/applecontainer/types"
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

	if err := c.cloneOpenCodeDirs(ctx, req.HostWorkDir, req.ID); err != nil {
		return nil, err
	}

	if err := c.configureOpenCode(ctx, req.HostWorkDir, req.ID); err != nil {
		return nil, err
	}
	return result, nil
}

func getContainerHostname(ctr *types.Container) string {
	for _, n := range ctr.Networks {
		return strings.TrimSuffix(n.Hostname, ".")
	}
	return ctr.Configuration.ID
}

func (c *OpenCodeWorkspaceCloner) Hydrate(ctx context.Context, box *Box) error {
	box.ContainerHooks = append(box.ContainerHooks,
		NewContainerStartupHook("Copy opencode binary to /usr/local/bin", func(ctx context.Context, b *Box) error {
			cpOut, err := b.Exec(ctx, "cp", "-r", "/root/.opencode/bin/opencode", "/usr/local/bin/opencode")
			if err != nil {
				slog.ErrorContext(ctx, "DefaultContainerHook copying dotfiles", "error", err, "cpOut", cpOut)
				return err
			}

			return nil
		}))

	box.ContainerHooks = append(box.ContainerHooks,
		NewContainerStartupHook("open remote ssh tunnel for chrome-devtools mcp", func(ctx context.Context, b *Box) error {
			ctr, err := b.GetContainer(ctx)
			if err != nil {
				return err
			}
			// No context - this should run in a separate process that outlives the cloner startup hook invocations.
			cmd := exec.Command("ssh", "-R", "9222:127.0.0.1:9222", "-N", "-o", "ExitOnForwardFailure=yes", "-o", "BatchMode=yes", "root@"+getContainerHostname(ctr))
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
			}
			slog.InfoContext(ctx, "OpenCodeWorkspaceCloner opening ssh remote port forward", "cmd", strings.Join(cmd.Args, " "))
			if err := cmd.Start(); err != nil {
				slog.ErrorContext(ctx, "OpenCodeWorkspaceCloner opening ssh remote port forward", "error", err)
				return err
			}
			slog.InfoContext(ctx, "OpenCodeWorkspaceCloner ssh remote port forward", "pid", cmd.Process.Pid)
			go func() {
				if err := cmd.Wait(); err != nil {
					slog.ErrorContext(ctx, "OpenCodeWorkspaceCloner ssh remote port forward cmd", "error", err)
				}
			}()
			// TODO: save the pid of the ssh tunnel process somewhere in the db so we can kill it later during cleanup,
			// which may occur in a different process (sand cli invocation) than this one.
			return nil
		}))
	slog.InfoContext(ctx, "OpenCodeWorkspaceCloner.Hydrate", "box", box)
	return c.baseCloner.Hydrate(ctx, box)
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

func (c *OpenCodeWorkspaceCloner) cloneOpenCodeDirs(ctx context.Context, cwd, id string) error {
	cloneOpenCodeStorage := filepath.Join(c.cloneRoot, id, "dotfiles", ".local", "share", "opencode", "storage")
	openCodeStorage := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "storage")

	if _, err := os.Stat(openCodeStorage); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneOpenCodeStorage)
		if err != nil {
			return err
		}
		defer f.Close()
		return nil
	}
	cmd := exec.CommandContext(ctx, "cp", "-Rc", openCodeStorage, cloneOpenCodeStorage)
	slog.InfoContext(ctx, "cloneOpenCodeStorage", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "cloneOpenCodeStorage", "error", err, "output", string(output))
		return err
	}

	cloneOpenCodeLog := filepath.Join(c.cloneRoot, id, "dotfiles", ".local", "share", "opencode", "log")
	openCodeLog := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "log")

	if _, err := os.Stat(openCodeLog); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneOpenCodeLog)
		if err != nil {
			return err
		}
		defer f.Close()
		return nil
	}
	cmd = exec.CommandContext(ctx, "cp", "-Rc", openCodeLog, cloneOpenCodeLog)
	slog.InfoContext(ctx, "cloneOpenCodeStorage", "cmd", strings.Join(cmd.Args, " "))
	output, err = cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "cloneOpenCodeStorage", "error", err, "output", string(output))
		return err
	}

	cloneOpenCodeSnapshot := filepath.Join(c.cloneRoot, id, "dotfiles", ".local", "share", "opencode", "snapshot")
	openCodeSnapshot := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "snapshot")

	if _, err := os.Stat(openCodeSnapshot); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneOpenCodeSnapshot)
		if err != nil {
			return err
		}
		defer f.Close()
		return nil
	}
	cmd = exec.CommandContext(ctx, "cp", "-Rc", openCodeSnapshot, cloneOpenCodeSnapshot)
	slog.InfoContext(ctx, "cloneOpenCodeStorage", "cmd", strings.Join(cmd.Args, " "))
	output, err = cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "cloneOpenCodeStorage", "error", err, "output", string(output))
		return err
	}

	return err
}

const opencodeJSON = `
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "chrome-devtools": {
      "type": "local",
      "command": [
        "npx",
        "-y",
        "chrome-devtools-mcp@latest",
        "--browserUrl",
        "http://127.0.0.1:9222"
      ],
      "enabled": true,
      "environment": {
      }
    }
  }
}`

func (c *OpenCodeWorkspaceCloner) configureOpenCode(ctx context.Context, cwd, id string) error {
	// TODO: read existing opencode.json files and merge them with the settings we want to override here.
	cloneOpenCodeConfig := filepath.Join(c.cloneRoot, id, "dotfiles", ".config", "opencode", "opencode.json")

	cloneOpenCodeDir := filepath.Dir(cloneOpenCodeConfig)
	if err := os.MkdirAll(cloneOpenCodeDir, 0o750); err != nil {
		return err
	}
	err := os.WriteFile(cloneOpenCodeConfig, []byte(opencodeJSON), 0o700)
	if err != nil {
		slog.ErrorContext(ctx, "configureOpenCode", "error", err)
		return err
	}

	return err
}
