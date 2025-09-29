package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

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

// cloneWorkDir creates a recursive, copy-on-write copy of hostWorkDir, under the sandboxer's root directory.
// "cp -c" uses APFS's clonefile(2) function to make the destination dir contents be COW.
func (sb *SandBoxer) cloneWorkDir(ctx context.Context, id, hostWorkDir string) error {
	cmd := exec.CommandContext(ctx, "cp", "-Rc", hostWorkDir, filepath.Join(sb.sandboxHostRootDir, "/", id))
	slog.InfoContext(ctx, "cloneWorkDir", "cmd", cmd)
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

func (sb *SandBox) shell(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	wait, err := ac.Containers.Exec(ctx,
		options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
			},
		}, sb.containerID, "/bin/sh", os.Environ(), stdin, stdout, stderr)
	if err != nil {
		return err
	}
	return wait()
}

func main() {
	ctx := context.Background()
	sber := &SandBoxer{
		sandboxHostRootDir: filepath.Join(os.Getenv("HOME"), "sandboxen"),
		imageName:          "ghcr.io/linuxcontainers/alpine:latest",
	}
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	sbox, err := sber.NewSandbox(ctx, cwd)
	if err != nil {
		panic(err)
	}
	if err := sbox.createContainer(ctx); err != nil {
		panic(err)
	}
	if err := sbox.startContainer(ctx); err != nil {
		panic(err)
	}
	if err := sbox.shell(ctx, os.Stdin, os.Stdout, os.Stderr); err != nil {
		panic(err)
	}

}
