package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
	cloneRoot      string
	sandBoxes      map[string]*Sandbox
	terminalWriter io.Writer
}

func NewSandBoxer(cloneRoot string, terminalWriter io.Writer) *SandBoxer {
	return &SandBoxer{
		cloneRoot:      cloneRoot,
		sandBoxes:      map[string]*Sandbox{},
		terminalWriter: terminalWriter,
	}
}

func (sb *SandBoxer) EnsureDefaultImage(ctx context.Context, imageName, dockerfileDir, sandboxUsername string) error {
	if err := sb.checkForImage(ctx, imageName, dockerfileDir, sandboxUsername); err != nil {
		return err
	}
	return nil
}

// NewSandbox creates a new sandbox based on a clone of hostWorkDir.
// TODO: clone envFile, if it exists, into sb.cloneRoot/id/env, so every command exec'd in that sandbox container
// uses the same env file, even if the original .env file has changed on the host machine.
func (sb *SandBoxer) NewSandbox(ctx context.Context, id, hostWorkDir, imageName, dockerFileDir, envFile string) (*Sandbox, error) {
	slog.InfoContext(ctx, "SandBoxer.NewSandbox", "hostWorkDir", hostWorkDir, "id", id)

	if err := sb.cloneWorkDir(ctx, id, hostWorkDir); err != nil {
		return nil, err
	}

	if err := sb.cloneClaudeDir(ctx, id); err != nil {
		return nil, err
	}

	if err := sb.cloneDotfiles(ctx, id); err != nil {
		return nil, err
	}

	ret := &Sandbox{
		ID:             id,
		HostOriginDir:  hostWorkDir,
		SandboxWorkDir: filepath.Join(sb.cloneRoot, id),
		ImageName:      imageName,
		EnvFile:        envFile,
	}
	sb.sandBoxes[id] = ret
	return ret, nil
}

// AttachSandbox re-connects to an existing container and sandboxWorkDir instead of creating a new one.
// TODO: persist all the struct fields somewhere since we can't reconstruct them from the clone dir alone.
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

	// TODO: deserialize this struct from some persistent storage, even if it's just a json file.
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

	if err := os.RemoveAll(sbox.SandboxWorkDir); err != nil {
		return err
	}
	return nil
}

// cloneWorkDir creates a recursive, copy-on-write copy of hostWorkDir, under the sandboxer's root directory.
// "cp -c" uses APFS's clonefile(2) function to make the destination dir contents be COW.
// Git stuff:
// Set up bi-drectional "remotes" to link the two checkouts:
// - in cloneRoot/id/app, remote "clonedfrom" -> hostWorkDir
// - in hostWorkDir, remote "sandbox-<id>" -> cloneRoot/id/app
// TODO: clean up these remotes when removing sandboxes.
// TODO: figure out how to deal with the inconsistency that the container's /app dir checkout now
// has remotes that point to host filesystem paths, not container filesystem paths.  This means
// "git fetch clonedfrom" works on the *host* OS, but not from inside the container.
// So if we want to let the agent see how it's changes differ from the host workdir, we have to
// let the agent ask something in the host OS to run the "git fetch clonedfrom" command in the
// cloneWorkDir on its behalf.
func (sb *SandBoxer) cloneWorkDir(ctx context.Context, id, hostWorkDir string) error {
	sb.userMsg(ctx, "Cloning "+hostWorkDir)
	if err := os.MkdirAll(filepath.Join(sb.cloneRoot, id), 0750); err != nil {
		return err
	}
	hostCloneDir := filepath.Join(sb.cloneRoot, "/", id, "app")
	cpCmd := exec.CommandContext(ctx, "cp", "-Rc", hostWorkDir, hostCloneDir)
	slog.InfoContext(ctx, "cloneWorkDir cpCmd", "cmd", strings.Join(cpCmd.Args, " "))
	output, err := cpCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir cpCmd", "error", err, "output", output)
		return err
	}

	gitRemoteCloneToWorkDirCmd := exec.CommandContext(ctx, "git", "remote", "add", "clonedfrom", hostWorkDir)
	gitRemoteCloneToWorkDirCmd.Dir = hostCloneDir
	slog.InfoContext(ctx, "cloneWorkDir gitRemoteCloneToWorkDirCmd", "cmd", strings.Join(gitRemoteCloneToWorkDirCmd.Args, " "))
	output, err = gitRemoteCloneToWorkDirCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitRemoteCloneToWorkDirCmd", "error", err, "output", output)
		return err
	}

	gitRemoteWorkDirToCloneCmd := exec.CommandContext(ctx, "git", "remote", "add", "clonedfrom", hostWorkDir)
	gitRemoteWorkDirToCloneCmd.Dir = hostWorkDir
	slog.InfoContext(ctx, "cloneWorkDir gitRemoteWorkDirToCloneCmd", "cmd", strings.Join(gitRemoteWorkDirToCloneCmd.Args, " "))
	output, err = gitRemoteWorkDirToCloneCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitRemoteWorkDirToCloneCmd", "error", err, "output", output)
		return err
	}

	return nil
}

func (sb *SandBoxer) cloneClaudeDir(ctx context.Context, id string) error {
	if err := os.MkdirAll(filepath.Join(sb.cloneRoot, id), 0750); err != nil {
		return err
	}
	cloneClaude := filepath.Join(sb.cloneRoot, "/", id, "dotfiles")
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

	return nil
}

func (sb *SandBoxer) cloneDotfiles(ctx context.Context, id string) error {
	sb.userMsg(ctx, "Cloning dotfiles...")
	dotfiles := []string{
		".claude.json",
		".gitconfig",
		".p10k.zsh",
		".zshrc",
	}
	if err := os.MkdirAll(filepath.Join(sb.cloneRoot, id, "dotfiles"), 0750); err != nil {
		return err
	}
	for _, dotfile := range dotfiles {
		clone := filepath.Join(sb.cloneRoot, "/", id, "dotfiles", dotfile)
		original := filepath.Join(os.Getenv("HOME"), dotfile)
		fi, err := os.Lstat(original)
		if errors.Is(err, os.ErrNotExist) {
			f, err := os.Create(clone)
			if err != nil {
				return err
			}
			f.Close()
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			destination, err := os.Readlink(original)
			if err != nil {
				slog.ErrorContext(ctx, "SandBoxer.cloneDotfiles error reading symbolic link", "original", original, "error", err)
				continue
			}
			if !filepath.IsAbs(destination) {
				destination = filepath.Join(os.Getenv("HOME"), destination)
			}
			// Now verify that the file that the symlink points to actually exists.
			_, err = os.Lstat(destination)
			if errors.Is(err, os.ErrNotExist) {
				slog.ErrorContext(ctx, "SandBoxer.cloneDotfiles symbolic link points to nonexistent file",
					"original", original, "destination", destination, "error", err)
				f, err := os.Create(clone)
				if err != nil {
					return err
				}
				f.Close()
				continue
			}
			slog.ErrorContext(ctx, "SandBoxer.cloneDotfiles resolved symbolic link",
				"original", original, "destination", destination)
			original = destination
		}
		cmd := exec.CommandContext(ctx, "cp", "-Rc", original, clone)
		slog.InfoContext(ctx, "cloneDotfiles", "cmd", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.InfoContext(ctx, "cloneDotfiles", "error", err, "output", output)
			return err
		}
	}

	return nil
}

func (sb *SandBoxer) buildDefaultImage(ctx context.Context, dockerFileDir, sandboxUsername string) error {
	slog.InfoContext(ctx, "SandBoxer.buildDefaultImage", "dockerFileDir", dockerFileDir, "sandboxUsername", sandboxUsername)
	sb.userMsg(ctx, "This may take a while, but we only do it once: building default container image...")

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
			sb.userMsg(ctx, logScanner.Text())
			slog.InfoContext(ctx, "buildDefaultImage", "stdout", logScanner.Text())
		}
		if logScanner.Err() != nil {
			slog.ErrorContext(ctx, "buildDefaultImage", "error", err)
		}
	}()

	go func() {
		logScanner := bufio.NewScanner(errLogs)
		for logScanner.Scan() {
			sb.userMsg(ctx, logScanner.Text())
			slog.ErrorContext(ctx, "buildDefaultImage", "stderr", logScanner.Text())
		}
		if logScanner.Err() != nil {
			slog.ErrorContext(ctx, "buildDefaultImage", "error", err)
		}
	}()
	err = wait()
	if err == nil {
		sb.userMsg(ctx, "\n\nDone building default container image.\n\n")
	}
	return err
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

func (sb *SandBoxer) userMsg(ctx context.Context, msg string) {
	if sb.terminalWriter == nil {
		slog.DebugContext(ctx, "userMsg (no terminalWriter)", "msg", msg)
		return
	}
	fmt.Fprintln(sb.terminalWriter, msg)
}
