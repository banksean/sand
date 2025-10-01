package sandbox

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ac "github.com/banksean/apple-container"
	"github.com/banksean/apple-container/options"
	"github.com/google/uuid"
)

var (
	// Clone these from ~/ into the container's /hosthome directory.
	rcFiles = []string{".gitconfig", ".p10k.zsh", ".ssh"}
)

type SandBoxer struct {
	cloneRoot       string
	sandBoxes       map[string]*SandBox
	imageName       string
	dockerFile      string
	sandboxUsername string
}

func NewSandBoxer(cloneRoot, imageName, dockerFile string) *SandBoxer {
	return &SandBoxer{
		cloneRoot:       cloneRoot,
		sandBoxes:       map[string]*SandBox{},
		imageName:       imageName,
		dockerFile:      dockerFile,
		sandboxUsername: "node", // TODO: make a parameter, pass it in from flags.
	}
}

func (sb *SandBoxer) Init(ctx context.Context) error {
	if err := sb.checkForImage(ctx); err != nil {
		return err
	}
	return nil
}

func (sb *SandBoxer) NewSandbox(ctx context.Context, hostWorkDir string) (*SandBox, error) {
	id := uuid.NewString()
	slog.InfoContext(ctx, "SandBoxer.NewSandbox", "hostWorkDir", hostWorkDir, "id", id)

	if err := sb.cloneWorkDir(ctx, id, hostWorkDir); err != nil {
		return nil, err
	}

	if err := sb.cloneHomeDirStuff(ctx, id); err != nil {
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

// AttachSandbox re-connects to an existing container and sandboxWorkDir instead of creating a new one.
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
	slog.InfoContext(ctx, "SandBoxer.Cleanup", "id", sbox.id)

	out, err := ac.Containers.Stop(ctx, nil, sbox.containerID)
	if err != nil {
		slog.ErrorContext(ctx, "SandBoxer.Cleanup", "error", err, "out", out)
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

// Clone various known user config settings from $HOME into $cloneRoot/$id/hosthome/...
// TODO: fix this to work with symlinks (e.g. .zshrc -> code/dotfiles/zsh/.zshrc)
func (sb *SandBoxer) cloneHomeDirStuff(ctx context.Context, id string) error {
	if err := os.MkdirAll(filepath.Join(sb.cloneRoot, id, "home"), 0750); err != nil {
		return err
	}
	home := os.Getenv("HOME")

	for _, rcFile := range rcFiles {
		path := filepath.Join(home, rcFile)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolvedTarget, err := os.Readlink(path)
			if err != nil {
				slog.InfoContext(ctx, "cloneHomeDirStuff", "error", err)
				continue
			}
			slog.InfoContext(ctx, "cloneHomeDirStuff", "path", path, "resolvedTarget", resolvedTarget)
			path = resolvedTarget
			if !filepath.IsAbs(path) {
				path = filepath.Join(home, path)
			}
		}
		cmd := exec.CommandContext(ctx, "cp", "-Rc", path, filepath.Join(sb.cloneRoot, "/", id, "home", rcFile))
		slog.InfoContext(ctx, "cloneHomeDirStuff", "cmd", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.InfoContext(ctx, "cloneHomeDirStuff", "error", err, "output", output)
			return err
		}
	}

	return nil
}

func (sb *SandBoxer) buildDefaultImage(ctx context.Context) error {
	outLogs, errLogs, wait, err := ac.Images.Build(ctx, &options.BuildOptions{
		File:     sb.dockerFile,
		Tag:      DefaultImageName,
		BuildArg: map[string]string{"USERNAME": sb.sandboxUsername},
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

func (sb *SandBoxer) checkForImage(ctx context.Context) error {
	manifests, err := ac.Images.Inspect(ctx, sb.imageName)
	if err != nil {
		slog.ErrorContext(ctx, "buildSandboxImage: Images.Build", "error", err, "manifests", len(manifests))
		if sb.imageName != DefaultImageName {
			return err
		}
		return sb.buildDefaultImage(ctx)
	}
	if len(manifests) == 0 {
		return fmt.Errorf("no images named %s ", sb.imageName)
	}
	return nil
}
