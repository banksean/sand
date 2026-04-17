package applecontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"

	"github.com/banksean/sand/internal/applecontainer/types"
)

type ImagesSvc struct{}

// Images is a service interface to interact with apple container images.
var Images ImagesSvc

// List returns all images, or an error.
func (i *ImagesSvc) List(ctx context.Context) ([]types.ImageEntry, error) {
	var images []types.ImageEntry

	output, err := exec.CommandContext(ctx, "container", "image", "list", "--format", "json").Output()
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(output, &images); err != nil {
		return nil, err
	}

	return images, nil
}

// Inspect returns details about the image with the given name, or an error.
func (i *ImagesSvc) Inspect(ctx context.Context, name string) ([]*types.ImageManifest, error) {
	slog.InfoContext(ctx, "ImageSvc.Inspect", "cmd", "container image inspect "+name)
	rawJSON, err := exec.CommandContext(ctx, "container", "image", "inspect", name).Output()
	if err != nil {
		return nil, err
	}

	var entries []*types.ImageManifest
	if err := json.Unmarshal([]byte(rawJSON), &entries); err != nil {
		slog.ErrorContext(ctx, "ImageSvc.Inspect json parse error", "image", name, "error", err)
		return nil, fmt.Errorf("failed to parse image JSON: %w", err)
	}
	return entries, nil
}

// Pull pulls the image with the given name and returns a wait func to release the cmd resources, or returns an error.
// It runs the command under a PTY so the 'container' subprocess sees a TTY and emits live progress output.
// All subprocess output is copied to w. The returned wait func is idempotent.
func (i *ImagesSvc) Pull(ctx context.Context, name string, w io.Writer) (func() error, error) {
	cmd := exec.CommandContext(ctx, "container", "image", "pull", name)
	slog.InfoContext(ctx, "ImagesSvc.Pull", "cmd.Dir", cmd.Dir, "cmd", strings.Join(cmd.Args, " "))

	// This Setpgid business is basically PTSD-induced superstition learned through Linux debugging nightmares.
	// It may not be necessary on MacOS at all.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Attach a PTY so 'container image pull' sees a TTY and emits live progress.
	// pty.Start sets cmd.Stdin/Stdout/Stderr to the PTY slave before calling cmd.Start.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	// Copy PTY master output to w until the master is closed.
	// When the subprocess exits the slave closes, causing reads from the master
	// to return EIO (macOS) or EOF. Either is expected and silently discarded.
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		io.Copy(w, ptmx) //nolint:errcheck // EIO/EOF on process exit is expected
	}()

	// The wait func is idempotent via sync.Once: boxer.pullImage has both a
	// defer and an explicit call, so we must tolerate being called twice.
	var once sync.Once
	var waitErr error
	waitFn := func() error {
		once.Do(func() {
			waitErr = cmd.Wait()
			ptmx.Close()
			<-copyDone
		})
		return waitErr
	}
	return waitFn, nil
}
