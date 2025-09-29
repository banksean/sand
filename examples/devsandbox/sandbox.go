// command sandbox creates and lanuches a containerized sandobx dev environment.
//
// On startup, it:
// - creates a copy-on-write clone of the current working directory in ~/sandboxen/${id} on the MacOS host
// - creates a new container instance with ~/sandboxen/${id} mounted to /app in the container, using bind-mode
// - starts the container
// - execs a shell in the container and connects this process's stdio to that shell in the container
//
// On shut down, it:
// - stops the container
// - deletes the container

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	ac "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
)

/*
HOST_WORKDIR is the directory from which we are working, on the host machine's filesystem.
SANDBOX_ROOT is a directory on the host machine's filesystem where we will store the sandboxes' roots.
SANDBOX_ID is an opaque identifier for a sandbox, e.g. a GUID
CONTAINER_IMAGE is the container image name, e.g. ghcr.io/linuxcontainers/alpine:latest

Steps to create a sandbox:
cp -Rc $HOST_WORKDIR $SANDBOX_ROOT/$SANDBOX_ID
container create --interactive --tty --mount type=bind,source=$SANDBOX_ROOT/$SANDBOX_ID,target=/app \
	--remove --name sandbox-$SANDBOX_ID $CONTAINER_IMAGE
*/

type SandBoxer struct {
	sandboxHostRootDir string
	sandBoxes          map[string]*SandBox
	imageName          string
}

func (sb *SandBoxer) NewSandbox(ctx context.Context, hostWorkDir string) (*SandBox, error) {
	id := fmt.Sprintf("%d", len(sb.sandBoxes))
	if err := sb.cloneWorkDir(ctx, id, hostWorkDir); err != nil {
		return nil, err
	}

	ret := &SandBox{
		id:             id,
		hostWorkDir:    hostWorkDir,
		sandboxWorkDir: filepath.Join(sb.sandboxHostRootDir, id),
		imageName:      sb.imageName,
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
	cmd := exec.CommandContext(ctx, "cp", "-Rc", hostWorkDir, filepath.Join(sb.sandboxHostRootDir, "/", id))
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
				Remove: true,
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

// Just print out a pasteable shell command to connect to this sandbox.
func (sb *SandBox) shellPrint(ctx context.Context, shellCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	execOpts :=
		options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
			},
		}
	fmt.Printf("container exec %s %s %s\n", strings.Join(options.ToArgs(execOpts), " "), sb.containerID, shellCmd)
	select {
	case <-time.After(time.Hour):
	case <-ctx.Done():
	}

	return nil
}

// TODO: fix the stdio-related hanging issue
func (sb *SandBox) shellExec(ctx context.Context, shellCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	// bufIn := bufio.NewReader(stdin)
	// bufOut := bufio.NewWriter(stdout)
	// bufErr := bufio.NewWriter(stderr)

	// Create a pipe to monitor stdin
	if true {
		prIn, pwIn := io.Pipe()
		prOut, pwOut := io.Pipe()
		prErr, pwErr := io.Pipe()

		// Monitor stdin in a goroutine
		// TODO: replace this with buffered IO. I suspect that is how this debug logging managed to fix stdin handling.
		go func() {
			defer pwIn.Close()
			buf := make([]byte, 1024)
			for {
				n, err := stdin.Read(buf)
				if err != nil {
					if err != io.EOF {
						slog.ErrorContext(ctx, "stdin read error", "error", err)
					}
					return
				}
				if _, err := pwIn.Write(buf[:n]); err != nil {
					slog.ErrorContext(ctx, "stdin write error", "error", err)
					return
				}
			}
		}()

		go func() {
			defer pwOut.Close()
			buf := make([]byte, 1024)
			for {
				n, err := prOut.Read(buf)
				if err != nil {
					if err != io.EOF {
						slog.ErrorContext(ctx, "stdout read error", "error", err)
					}
					return
				}
				if _, err := stdout.Write(buf[:n]); err != nil {
					slog.ErrorContext(ctx, "stdout write error", "error", err)
					return
				}
			}
		}()

		go func() {
			defer pwErr.Close()
			buf := make([]byte, 1024)
			for {
				n, err := prErr.Read(buf)
				if err != nil {
					if err != io.EOF {
						slog.ErrorContext(ctx, "stderr read error", "error", err)
					}
					return
				}
				if _, err := stderr.Write(buf[:n]); err != nil {
					slog.ErrorContext(ctx, "stderr write error", "error", err)
					return
				}
			}
		}()

		stdin = prIn
		// stdout = pwOut
		// stderr = pwErr
	}
	wait, err := ac.Containers.Exec(ctx,
		options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
			},
		}, sb.containerID, shellCmd, os.Environ(), stdin, stdout, stderr)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "shell: waiting for Exec to finish")
	errCh := make(chan error)
	go func() {
		errCh <- wait()
	}()
	select {
	case <-ctx.Done():
		slog.InfoContext(ctx, "shell: context canceled")
		return ctx.Err()
	case err := <-errCh:
		slog.InfoContext(ctx, "shell: errCh", "error", err)
		return err
	}
}

var (
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
	sber := &SandBoxer{
		sandboxHostRootDir: filepath.Join(os.Getenv("HOME"), "sandboxen"),
		imageName:          *imageName,
	}
	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		os.Exit(1)
	}
	sbox, err := sber.NewSandbox(ctx, cwd)
	if err != nil {
		slog.ErrorContext(ctx, "sber.NewSandbox", "error", err)
		os.Exit(1)
	}

	// Create a channel to receive OS signals
	sigChan := make(chan os.Signal, 1)
	// Register the channel to receive SIGINT (Ctrl+C) and SIGTERM
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start a goroutine to handle the signal
	go func() {
		sig := <-sigChan
		slog.InfoContext(ctx, "Received signal; performing cleanup", "sig", sig)

		if err := sber.Cleanup(ctx, sbox); err != nil {
			slog.ErrorContext(ctx, "sber.Cleanup", "error", err)
		}
		slog.InfoContext(ctx, "Cleanup complete. Exiting.")
		cancel()
		os.Exit(1)
	}()

	// Now create and start the container, then exec the shell command and attach stdio to it:
	if err := sbox.createContainer(ctx); err != nil {
		slog.ErrorContext(ctx, "sbox.createContainer", "error", err)
		os.Exit(1)
	}

	if err := sbox.startContainer(ctx); err != nil {
		slog.ErrorContext(ctx, "sbox.startContainer", "error", err)
		os.Exit(1)
	}

	slog.InfoContext(ctx, "main: sbox.shell starting")
	if err := sbox.shellPrint(ctx, *shellCmd, os.Stdin, os.Stdout, os.Stderr); err != nil {
		slog.ErrorContext(ctx, "sbox.shell", "error", err)
	}

	slog.InfoContext(ctx, "sbox.shell finished, cleaning up...")
	if err := sber.Cleanup(ctx, sbox); err != nil {
		slog.ErrorContext(ctx, "sber.Cleanup", "error", err)
	}

	slog.InfoContext(ctx, "Cleanup complete. Exiting.")
}
