package applecontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"golang.org/x/term"
)

type ContainerSvc struct{}

// Containers is a service interface to interact with apple containers.
var Containers ContainerSvc

// List returns all containers, or an error.
func (c *ContainerSvc) List(ctx context.Context) ([]types.Container, error) {
	var containers []types.Container
	cmd := exec.CommandContext(ctx, "container", "list", "--all", "--format", "json")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, err
	}

	return containers, nil
}

// Inspect returns details about the requested container IDs, or an error.
func (c *ContainerSvc) Inspect(ctx context.Context, id ...string) ([]types.Container, error) {
	cmd := exec.CommandContext(ctx, "container", append([]string{"inspect"}, id...)...)
	slog.InfoContext(ctx, "ContainerSvc.Inspect", "cmd", strings.Join(cmd.Args, " "))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	rawJSON, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	ret := []types.Container{}
	if err := json.Unmarshal(rawJSON, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

// Logs returns an io.ReadCloser for streaming log output and a wait func that blocks on the command's completion, or an error.
func (c *ContainerSvc) Logs(ctx context.Context, opts *options.ContainerLogs, id string) (io.ReadCloser, func() error, error) {
	args := options.ToArgs(opts)
	args = append([]string{"logs"}, append(args, id)...)
	cmd := exec.CommandContext(ctx, "container", args...)
	// This Setpgid business is basically PTSD-induced superstition learned through Linux debugging nightmares.
	// It may not be necessary on MacOS at all.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	return out, cmd.Wait, nil
}

// Create creates a new container with the given options, name and init args. It returns the ID of the new container instance.
func (c *ContainerSvc) Create(ctx context.Context, opts *options.CreateContainer, imageName string, initArgs []string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"create"}, append(args, imageName)...)
	cmd := exec.CommandContext(ctx, "container", append(args, initArgs...)...)
	slog.InfoContext(ctx, "ContainerSvc.Create", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(output))
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return out, nil
}

// Start starts a container instance with a given ID. It returns the start command output, or an error.
func (c *ContainerSvc) Start(ctx context.Context, opts *options.StartContainer, id string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"start"}, append(args, id)...)
	cmd := exec.CommandContext(ctx, "container", args...)
	slog.InfoContext(ctx, "ContainerSvc.Start", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ContainerSvc.Start error: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// Stop stops a container instance with a given ID. It returns the stop command output, or an error.
func (c *ContainerSvc) Stop(ctx context.Context, opts *options.StopContainer, id string) (string, error) {
	slog.InfoContext(ctx, "ContainerSvc.Stop", "opts", opts, "id", id)
	args := options.ToArgs(opts)
	args = append([]string{"stop"}, append(args, id)...)
	cmd := exec.CommandContext(ctx, "container", args...)
	slog.InfoContext(ctx, "ContainerSvc.Stop", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.Output()
	if err != nil {
		slog.ErrorContext(ctx, "ContainerSvc.Stop", "error", err, "out", string(output))
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Delete deletes a container instance with a given ID. It returns the delete command output, or an error.
func (c *ContainerSvc) Delete(ctx context.Context, opts *options.DeleteContainer, id string) (string, error) {
	args := options.ToArgs(opts)
	args = append([]string{"delete"}, append(args, id)...)
	cmd := exec.CommandContext(ctx, "container", args...)
	slog.InfoContext(ctx, "ContainerSvc.Delete", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Run runs a command in a new container instance based on the given image.
func (c *ContainerSvc) Run(ctx context.Context, opts *options.RunContainer, imageName, command string, env []string, stdin io.Reader, stdout, stderr io.Writer, cmdArgs ...string) (func() error, error) {
	args := options.ToArgs(opts)
	args = append(args, append([]string{imageName, command}, cmdArgs...)...)
	cmd := exec.CommandContext(ctx, "container", append([]string{"run"}, args...)...)
	slog.InfoContext(ctx, "ContainerSvc.Run", "cmd", strings.Join(cmd.Args, " "))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd.Wait, nil
}

func (c *ContainerSvc) Exec(ctx context.Context, opts *options.ExecContainer, containerID, command string, env []string, cmdArgs ...string) (string, error) {
	args := options.ToArgs(opts)
	args = append(args, append([]string{containerID, command}, cmdArgs...)...)
	cmd := exec.CommandContext(ctx, "container", append([]string{"exec"}, args...)...)
	cmd.Env = env
	slog.InfoContext(ctx, "ContainerSvc.Exec", "cmd", strings.Join(cmd.Args, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "ContainerSvc.Exec", "error", err, "out", string(out))
		return string(out), err
	}

	return string(out), nil
}

// ExecStream executes a command in a running container instance, with stdio streams.
func (c *ContainerSvc) ExecStream(ctx context.Context, opts *options.ExecContainer, containerID, command string, env []string, stdin io.Reader, stdout, stderr io.Writer, cmdArgs ...string) (func() error, error) {
	args := options.ToArgs(opts)
	args = append(args, append([]string{containerID, command}, cmdArgs...)...)
	cmd := exec.CommandContext(ctx, "container", append([]string{"exec"}, args...)...)
	slog.InfoContext(ctx, "ContainerSvc.ExecStream", "cmd", strings.Join(cmd.Args, " "))
	cmd.Env = env
	slog.InfoContext(ctx, "ContainerSvc.ExecStream: normal terminal passthrough")
	// If stdin is a real terminal, put it in raw mode before handing off.
	// container exec --tty needs the real terminal to be raw so its own
	// PTY proxying doesn't get double-processed by the terminal driver.
	var savedState *term.State
	if stdinFile, ok := stdin.(*os.File); ok && term.IsTerminal(int(stdinFile.Fd())) {
		var err error
		savedState, err = term.MakeRaw(int(stdinFile.Fd()))
		if err != nil {
			return nil, fmt.Errorf("making terminal raw: %w", err)
		}
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		if savedState != nil {
			term.Restore(int(stdin.(*os.File).Fd()), savedState)
		}
		return nil, err
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			slog.InfoContext(ctx, "ContainerSvc.ExecStream signal handler", "signal", sig)
			switch sig {
			case syscall.SIGWINCH:
				syscall.Kill(-cmd.Process.Pid, syscall.SIGWINCH)
			case syscall.SIGINT, syscall.SIGTERM:
				if savedState != nil {
					term.Restore(int(stdin.(*os.File).Fd()), savedState)
				}
			}
		}
	}()

	return func() error {
		err := cmd.Wait()
		if savedState != nil {
			term.Restore(int(stdin.(*os.File).Fd()), savedState)
		}
		return err
	}, nil
}

// Kill kills containers
func (c *ContainerSvc) Kill(ctx context.Context, opts *options.KillContainer, id ...string) (string, error) {
	slog.InfoContext(ctx, "ContainerSvc.Kill", "opts", opts, "id", id)
	args := options.ToArgs(opts)
	args = append([]string{"kill"}, append(args, id...)...)
	cmd := exec.CommandContext(ctx, "container", args...)
	slog.InfoContext(ctx, "ContainerSvc.Kill", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (c *ContainerSvc) Export(ctx context.Context, opts *options.ExportContainer, id string) (string, error) {
	args := options.ToArgs(opts)
	args = append(args, id)
	cmd := exec.CommandContext(ctx, "container", append([]string{"export"}, args...)...)
	slog.InfoContext(ctx, "ContainerSvc.Export", "cmd", strings.Join(cmd.Args, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "ContainerSvc.Export", "error", err, "out", string(out))
		return string(out), err
	}

	return string(out), nil
}

func (c *ContainerSvc) Stats(ctx context.Context, id ...string) ([]types.ContainerStats, error) {
	args := statsArgs(id...)
	cmd := exec.CommandContext(ctx, "container", args...)
	slog.InfoContext(ctx, "ContainerSvc.Stats", "cmd", strings.Join(cmd.Args, " "), "ids", len(id))
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "ContainerSvc.Stats", "error", err, "out", string(out))
		return nil, err
	}

	ret := []types.ContainerStats{}
	if err := json.Unmarshal(out, &ret); err != nil {
		return nil, err
	}

	return ret, nil
}

func statsArgs(id ...string) []string {
	args := []string{"stats", "--format", "json", "--no-stream"}
	if len(id) > 0 {
		args = append(args, id...)
	}
	return args
}
