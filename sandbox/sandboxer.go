package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ac "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
)

type SandBoxer struct {
	cloneRoot string
	sandBoxes map[string]*Sandbox
}

func NewSandBoxer(cloneRoot string) *SandBoxer {
	return &SandBoxer{
		cloneRoot: cloneRoot,
		sandBoxes: map[string]*Sandbox{},
	}
}

func (sb *SandBoxer) EnsureDefaultImage(ctx context.Context, imageName, dockerfileDir, sandboxUsername string) error {
	if err := sb.checkForImage(ctx, imageName, dockerfileDir, sandboxUsername); err != nil {
		return err
	}
	return nil
}

func (sb *SandBoxer) NewSandbox(ctx context.Context, id, hostWorkDir, imageName, dockerFileDir, dnsDomain string) (*Sandbox, error) {
	slog.InfoContext(ctx, "SandBoxer.NewSandbox", "hostWorkDir", hostWorkDir, "id", id)

	if err := sb.cloneWorkDir(ctx, id, hostWorkDir); err != nil {
		return nil, err
	}

	ret := &Sandbox{
		ID:             id,
		HostOriginDir:  hostWorkDir,
		SandboxWorkDir: filepath.Join(sb.cloneRoot, id),
		ImageName:      imageName,
		DNSDomain:      dnsDomain,
	}
	sb.sandBoxes[id] = ret
	return ret, nil
}

// AttachSandbox re-connects to an existing container and sandboxWorkDir instead of creating a new one.
func (sb *SandBoxer) AttachSandbox(ctx context.Context, id string) (*Sandbox, error) {
	slog.InfoContext(ctx, "SandBoxer.AttachSandbox", "id", id)
	ret := &Sandbox{
		ID:             id,
		SandboxWorkDir: filepath.Join(sb.cloneRoot, id),
		ContainerID:    id,
		HostOriginDir:  "", // we don't know this any more since we don't store it anywhere
		ImageName:      "",
	}

	return ret, nil
}

func (sb *SandBoxer) List(ctx context.Context) ([]Sandbox, error) {
	dir := os.DirFS(sb.cloneRoot)
	ret := []Sandbox{}
	err := fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.ErrorContext(ctx, "SandBoxer.List", "err", err)
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			sbox, err := sb.AttachSandbox(ctx, path)
			if err != nil {
				return err
			}
			ret = append(ret, *sbox)
			return fs.SkipDir
		}
		return nil
	})
	return ret, err
}

func (sb *SandBoxer) Get(ctx context.Context, id string) (*Sandbox, error) {
	dir := filepath.Join(sb.cloneRoot, id)
	slog.InfoContext(ctx, "SandBoxer.Get", "id", id)
	f, err := os.Stat(dir)
	if err != nil {
		slog.ErrorContext(ctx, "SandBoxer.Get", "error", err)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !f.IsDir() {
		return nil, fmt.Errorf("path exists but is not a directory: %s", dir)
	}

	ret := &Sandbox{
		ID:             id,
		SandboxWorkDir: filepath.Join(sb.cloneRoot, id),
		ContainerID:    id,
		HostOriginDir:  "", // we don't know this any more since we don't store it anywhere
		ImageName:      "",
	}
	slog.InfoContext(ctx, "SandBoxer.Get", "ret", ret)
	return ret, nil
}

func (sb *SandBoxer) Cleanup(ctx context.Context, sbox *Sandbox) error {
	slog.InfoContext(ctx, "SandBoxer.Cleanup", "id", sbox.ID)

	out, err := ac.Containers.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "SandBoxer Containers.Stop", "error", err, "out", out)
	}

	out, err = ac.Containers.Delete(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "SandBoxer Containers.Delete", "error", err, "out", out)
	}

	return nil
}

// cloneWorkDir creates a recursive, copy-on-write copy of hostWorkDir, under the sandboxer's root directory.
// "cp -c" uses APFS's clonefile(2) function to make the destination dir contents be COW.
func (sb *SandBoxer) cloneWorkDir(ctx context.Context, id, hostWorkDir string) error {
	if err := os.MkdirAll(filepath.Join(sb.cloneRoot, id), 0750); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "cp", "-Rc", hostWorkDir, filepath.Join(sb.cloneRoot, "/", id, "app"))
	slog.InfoContext(ctx, "cloneWorkDir", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir", "error", err, "output", output)
		return err
	}
	return nil
}

func (sb *SandBoxer) buildDefaultImage(ctx context.Context, dockerFileDir, sandboxUsername string) error {
	outLogs, errLogs, wait, err := ac.Images.Build(ctx, dockerFileDir,
		&options.BuildOptions{
			Tag:      DefaultImageName,
			BuildArg: map[string]string{"USERNAME": sandboxUsername},
		})
	if err != nil {
		slog.ErrorContext(ctx, "buildSandboxImage: Images.Build", "error", err)
		return err
	}
	defer outLogs.Close()

	go func() {
		logScanner := bufio.NewScanner(outLogs)
		for logScanner.Scan() {
			slog.InfoContext(ctx, "buildDefaultImage", "stdout", logScanner.Text())
		}
		if logScanner.Err() != nil {
			slog.ErrorContext(ctx, "buildDefaultImage", "error", err)
		}
	}()

	go func() {
		logScanner := bufio.NewScanner(errLogs)
		for logScanner.Scan() {
			slog.InfoContext(ctx, "buildDefaultImage", "stderr", logScanner.Text())
		}
		if logScanner.Err() != nil {
			slog.ErrorContext(ctx, "buildDefaultImage", "error", err)
		}
	}()

	return wait()
}

func (sb *SandBoxer) checkForImage(ctx context.Context, imageName, dockerfileDir, sandboxUsername string) error {
	manifests, err := ac.Images.Inspect(ctx, imageName)
	if err != nil {
		slog.ErrorContext(ctx, "buildSandboxImage: Images.Build", "error", err, "manifests", len(manifests))
		if imageName != DefaultImageName {
			return err
		}
		return sb.buildDefaultImage(ctx, dockerfileDir, sandboxUsername)
	}
	if len(manifests) == 0 {
		return fmt.Errorf("no images named %s ", imageName)
	}
	return nil
}
