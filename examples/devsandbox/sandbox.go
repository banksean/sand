// command devsandbox manages containerized dev sandobx environments on MacOS
//
// On startup, devsandbox will:
// - if --attach=${id} is not set:
//   - choose a new unused ${id}
//   - create a new copy-on-write clone of the current working directory in ~/sandboxen/${id} on the MacOS host
//   - create a new container instance with name ${id} and ~/sandboxen/${id} mounted to /app in the container, using bind-mode
// - start container named ${id}
// - exec a shell in the container and connect this process's stdio to that shell in the container
//
// On exit, devsandbox will
// - stop the container named ${id}
// - if --rm is set, delete the container

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	ac "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
)

type SandBoxer struct {
	cloneRoot string
	sandBoxes map[string]*SandBox
	imageName string
}

func NewSandBoxer(cloneRoot, imageName string) *SandBoxer {
	return &SandBoxer{
		cloneRoot: cloneRoot,
		sandBoxes: map[string]*SandBox{},
		imageName: imageName,
	}
}

func (sb *SandBoxer) NewSandbox(ctx context.Context, hostWorkDir string) (*SandBox, error) {
	id := fmt.Sprintf("%d", len(sb.sandBoxes))
	if err := sb.cloneWorkDir(ctx, id, hostWorkDir); err != nil {
		return nil, err
	}

	ret := &SandBox{
		id:             id,
		hostWorkDir:    hostWorkDir,
		sandboxWorkDir: filepath.Join(sb.cloneRoot, id),
		imageName:      sb.imageName,
	}
	sb.sandBoxes[id] = ret
	return ret, nil
}

func (sb *SandBoxer) AttachSandbox(ctx context.Context, id string) (*SandBox, error) {
	slog.InfoContext(ctx, "SandBoxer.AttachSandbox", "id", id)
	ret := &SandBox{
		id:             id,
		hostWorkDir:    "", // we don't know this any more.
		sandboxWorkDir: filepath.Join(sb.cloneRoot, id),
		imageName:      sb.imageName,
		containerID:    "sandbox-" + id,
	}

	return ret, nil
}

func (sb *SandBoxer) Cleanup(ctx context.Context, sbox *SandBox) error {
	out, err := ac.Containers.Stop(ctx, options.StopContainer{}, sbox.containerID)
	if err != nil {
		slog.ErrorContext(ctx, "SandBoxer.Cleanup", "error", err, "out", out)
	}
	return nil
}

// cloneWorkDir creates a recursive, copy-on-write copy of hostWorkDir, under the sandboxer's root directory.
// "cp -c" uses APFS's clonefile(2) function to make the destination dir contents be COW.
func (sb *SandBoxer) cloneWorkDir(ctx context.Context, id, hostWorkDir string) error {
	cmd := exec.CommandContext(ctx, "cp", "-Rc", hostWorkDir, filepath.Join(sb.cloneRoot, "/", id))
	slog.InfoContext(ctx, "cloneWorkDir", "cmd", cmd.Args)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir", "error", err, "output", output)
		return err
	}
	return nil
}

type SandBox struct {
	id          string
	containerID string
	// hostWorkDir is the origin of the sandbox, from which we clone its contents
	hostWorkDir    string
	sandboxWorkDir string
	imageName      string
}

func (sb *SandBox) createContainer(ctx context.Context) error {
	containerID, err := ac.Containers.Create(ctx,
		options.CreateContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
			},
			ManagementOptions: options.ManagementOptions{
				Name:   "sandbox-" + sb.id,
				Remove: *rm,
				Mount:  fmt.Sprintf(`type=bind,source=%s,target=/app`, sb.sandboxWorkDir),
			},
		},
		sb.imageName, nil)
	if err != nil {
		slog.ErrorContext(ctx, "createContainer", "error", err, "output", containerID)
		return err
	}
	sb.containerID = containerID
	return nil
}

func (sb *SandBox) startContainer(ctx context.Context) error {
	output, err := ac.Containers.Start(ctx, options.StartContainer{}, sb.containerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainer", "error", err, "output", output)
		return err
	}
	slog.InfoContext(ctx, "startContainer succeeded", "output", output)
	return nil
}

func (sb *SandBox) shellExec(ctx context.Context, shellCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	wait, err := ac.Containers.Exec(ctx,
		options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
			},
		}, sb.containerID, shellCmd, os.Environ(), stdin, stdout, stderr)
	if err != nil {
		slog.ErrorContext(ctx, "shell: ac.Containers.Exec", "error", err)
		return err
	}

	return wait()
}

var (
	rm          = flag.Bool("rm", false, "remove the container on exit")
	attachTo    = flag.String("attach", "", "sandbox ID to re-connect to")
	imageName   = flag.String("image", "ghcr.io/linuxcontainers/alpine:latest", "name of container image to use")
	shellCmd    = flag.String("shell", "/bin/sh", "shell command to exec in the container")
	logLevelStr = flag.String("loglevel", "error", "Set the logging level (debug, info, warn, error)")
	logFile     = flag.String("log", "", "location of log file (leave empty for a random tmp/ path)")
)

func initSlog() {
	var level slog.Level
	switch *logLevelStr {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo // Default to info if invalid
	}

	// Create a new logger with a JSON handler writing to standard error
	var f *os.File
	var err error
	if *logFile == "" {
		f, err = os.CreateTemp("/tmp", *logFile)
		if err != nil {
			panic(err)
		}
	} else {
		f, err = os.OpenFile(*logFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			panic(err)
		}
	}
	fmt.Printf("tail -f %s\n", f.Name())
	logger := slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
}

func main() {
	flag.Parse()
	initSlog()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sber := NewSandBoxer(
		filepath.Join(os.Getenv("HOME"), "sandboxen"),
		*imageName,
	)

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		os.Exit(1)
	}
	var sbox *SandBox
	if *attachTo == "" {
		sbox, err = sber.NewSandbox(ctx, cwd)
		if err != nil {
			slog.ErrorContext(ctx, "sber.NewSandbox", "error", err)
			os.Exit(1)
		}

		slog.InfoContext(ctx, "main: sbox.createContainer")
		if err := sbox.createContainer(ctx); err != nil {
			slog.ErrorContext(ctx, "sbox.createContainer", "error", err)
			os.Exit(1)
		}
	} else {
		sbox, err = sber.AttachSandbox(ctx, *attachTo)
		if err != nil {
			slog.ErrorContext(ctx, "sber.AttachSandbox", "error", err)
			os.Exit(1)
		}
	}

	slog.InfoContext(ctx, "main: sbox.startContainer")
	if err := sbox.startContainer(ctx); err != nil {
		slog.ErrorContext(ctx, "sbox.startContainer", "error", err)
		os.Exit(1)
	}

	slog.InfoContext(ctx, "main: sbox.shell starting")
	if err := sbox.shellExec(ctx, *shellCmd, os.Stdin, os.Stdout, os.Stderr); err != nil {
		slog.ErrorContext(ctx, "sbox.shell", "error", err)
	}

	slog.InfoContext(ctx, "sbox.shell finished, cleaning up...")
	if err := sber.Cleanup(ctx, sbox); err != nil {
		slog.ErrorContext(ctx, "sber.Cleanup", "error", err)
	}

	slog.InfoContext(ctx, "Cleanup complete. Exiting.")
}
