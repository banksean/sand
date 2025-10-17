package sand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MountSpec describes a bind mount that should be attached to a container.
type MountSpec struct {
	Source   string
	Target   string
	ReadOnly bool
}

// String renders the mount specification into the container runtime format.
func (m MountSpec) String() string {
	parts := []string{
		"type=bind",
		fmt.Sprintf("source=%s", m.Source),
		fmt.Sprintf("target=%s", m.Target),
	}
	if m.ReadOnly {
		parts = append(parts, "readonly")
	}
	return strings.Join(parts, ",")
}

// ContainerHook allows callers to inject container startup customisation.
type ContainerHook interface {
	Name() string
	OnStart(ctx context.Context, b *Box) error
}

type containerHook struct {
	name string
	fn   func(ctx context.Context, b *Box) error
}

func (h containerHook) Name() string {
	return h.name
}

func (h containerHook) OnStart(ctx context.Context, b *Box) error {
	return h.fn(ctx, b)
}

// NewContainerHook helpers callers construct hook instances without exporting internals.
func NewContainerHook(name string, fn func(ctx context.Context, b *Box) error) ContainerHook {
	return containerHook{name: name, fn: fn}
}

// ProvisionRequest captures the inputs necessary to prepare a sandbox workspace.
type ProvisionRequest struct {
	ID          string
	HostWorkDir string
	EnvFile     string
}

// ProvisionResult describes the assets created for a sandbox and how to mount/configure them.
type ProvisionResult struct {
	SandboxWorkDir string
	Mounts         []MountSpec
	ContainerHooks []ContainerHook
}

// WorkspaceProvisioner abstracts the steps for preparing sandbox host resources.
type WorkspaceProvisioner interface {
	Prepare(ctx context.Context, req ProvisionRequest) (*ProvisionResult, error)
	Hydrate(ctx context.Context, box *Box) error
}

const (
	originalWorkdDirRemoteName = "origin-host-workdir"
	ClonedWorkDirRemotePrefix  = "sand/"
)

// DefaultWorkspaceProvisioner reproduces the current cloning behaviour.
type DefaultWorkspaceProvisioner struct {
	appRoot        string
	cloneRoot      string
	terminalWriter io.Writer
}

// NewDefaultWorkspaceProvisioner constructs the default provisioner used by Boxer.
func NewDefaultWorkspaceProvisioner(appRoot string, terminalWriter io.Writer) *DefaultWorkspaceProvisioner {
	return &DefaultWorkspaceProvisioner{
		appRoot:        appRoot,
		cloneRoot:      filepath.Join(appRoot, "clones"),
		terminalWriter: terminalWriter,
	}
}

// Prepare performs the same cloning steps previously implemented inline in Boxer.
func (p *DefaultWorkspaceProvisioner) Prepare(ctx context.Context, req ProvisionRequest) (*ProvisionResult, error) {
	if err := os.MkdirAll(filepath.Join(p.cloneRoot, req.ID), 0o750); err != nil {
		return nil, err
	}

	if err := p.cloneWorkDir(ctx, req.ID, req.HostWorkDir); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Join(p.cloneRoot, req.ID, "dotfiles"), 0o750); err != nil {
		return nil, err
	}

	if err := p.cloneClaudeDir(ctx, req.HostWorkDir, req.ID); err != nil {
		return nil, err
	}

	if err := p.cloneDotfiles(ctx, req.ID); err != nil {
		return nil, err
	}

	if err := p.cloneClaudeJSON(ctx, req.HostWorkDir, req.ID); err != nil {
		return nil, err
	}

	if err := p.cloneHostKeyPair(ctx, req.ID); err != nil {
		return nil, err
	}

	sandboxWorkDir := filepath.Join(p.cloneRoot, req.ID)

	mounts := p.mountPlanFor(sandboxWorkDir)
	hooks := []ContainerHook{
		p.defaultContainerHook(),
	}

	return &ProvisionResult{
		SandboxWorkDir: sandboxWorkDir,
		Mounts:         mounts,
		ContainerHooks: hooks,
	}, nil
}

// Hydrate populates runtime-only fields on a Box that was loaded from persistent storage.
func (p *DefaultWorkspaceProvisioner) Hydrate(ctx context.Context, box *Box) error {
	if box == nil {
		return fmt.Errorf("nil box provided to hydrate")
	}
	sandboxWorkDir := box.SandboxWorkDir
	if sandboxWorkDir == "" {
		return fmt.Errorf("sandbox %s missing workdir", box.ID)
	}
	box.Mounts = p.mountPlanFor(sandboxWorkDir)
	box.ContainerHooks = []ContainerHook{p.defaultContainerHook()}
	return nil
}

func (p *DefaultWorkspaceProvisioner) mountPlanFor(sandboxWorkDir string) []MountSpec {
	return []MountSpec{
		{
			Source:   filepath.Join(sandboxWorkDir, "hostkeys"),
			Target:   "/hostkeys",
			ReadOnly: true,
		},
		{
			Source:   filepath.Join(sandboxWorkDir, "dotfiles"),
			Target:   "/dotfiles",
			ReadOnly: true,
		},
		{
			Source: filepath.Join(sandboxWorkDir, "app"),
			Target: "/app",
		},
	}
}

func (p *DefaultWorkspaceProvisioner) userMsg(ctx context.Context, msg string) {
	if p.terminalWriter == nil {
		slog.DebugContext(ctx, "provisioner userMsg (no terminalWriter)", "msg", msg)
		return
	}
	fmt.Fprintln(p.terminalWriter, "\033[90m"+msg+"\033[0m")
}

func (p *DefaultWorkspaceProvisioner) cloneWorkDir(ctx context.Context, id, hostWorkDir string) error {
	p.userMsg(ctx, "Cloning "+hostWorkDir)
	if err := os.MkdirAll(filepath.Join(p.cloneRoot, id), 0o750); err != nil {
		return err
	}
	hostCloneDir := filepath.Join(p.cloneRoot, id, "app")
	cpCmd := exec.CommandContext(ctx, "cp", "-Rc", hostWorkDir, hostCloneDir)
	slog.InfoContext(ctx, "cloneWorkDir cpCmd", "cmd", strings.Join(cpCmd.Args, " "))
	output, err := cpCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir cpCmd", "error", err, "output", string(output))
		return err
	}

	gitRemoteCloneToWorkDirCmd := exec.CommandContext(ctx, "git", "remote", "add", originalWorkdDirRemoteName, hostWorkDir)
	gitRemoteCloneToWorkDirCmd.Dir = hostCloneDir
	slog.InfoContext(ctx, "cloneWorkDir gitRemoteCloneToWorkDirCmd", "cmd", strings.Join(gitRemoteCloneToWorkDirCmd.Args, " "))
	output, err = gitRemoteCloneToWorkDirCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitRemoteCloneToWorkDirCmd", "error", err, "output", string(output))
		return err
	}

	gitRemoteWorkDirToCloneCmd := exec.CommandContext(ctx, "git", "remote", "add", ClonedWorkDirRemotePrefix+id, hostCloneDir)
	gitRemoteWorkDirToCloneCmd.Dir = hostWorkDir
	slog.InfoContext(ctx, "cloneWorkDir gitRemoteWorkDirToCloneCmd", "cmd", strings.Join(gitRemoteWorkDirToCloneCmd.Args, " "))
	output, err = gitRemoteWorkDirToCloneCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitRemoteWorkDirToCloneCmd", "error", err, "output", string(output))
		return err
	}

	gitFetchCloneToWorkDirCmd := exec.CommandContext(ctx, "git", "fetch", originalWorkdDirRemoteName)
	gitFetchCloneToWorkDirCmd.Dir = hostCloneDir
	slog.InfoContext(ctx, "cloneWorkDir gitFetchCloneToWorkDirCmd", "cmd", strings.Join(gitFetchCloneToWorkDirCmd.Args, " "))
	output, err = gitFetchCloneToWorkDirCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitFetchCloneToWorkDirCmd", "error", err, "output", string(output))
		return err
	}

	gitFetchWorkDirToCloneCmd := exec.CommandContext(ctx, "git", "fetch", ClonedWorkDirRemotePrefix+id)
	gitFetchWorkDirToCloneCmd.Dir = hostWorkDir
	slog.InfoContext(ctx, "cloneWorkDir gitFetchWorkDirToCloneCmd", "cmd", strings.Join(gitFetchWorkDirToCloneCmd.Args, " "))
	output, err = gitFetchWorkDirToCloneCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitFetchWorkDirToCloneCmd", "error", err, "output", string(output))
		return err
	}

	return nil
}

func (p *DefaultWorkspaceProvisioner) cloneHostKeyPair(ctx context.Context, id string) error {
	hostKey := filepath.Join(p.appRoot, hostKeyFilename)
	hostKeyPub := filepath.Join(p.appRoot, hostKeyFilename+".pub")

	cloneHostKeyDir := filepath.Join(p.cloneRoot, id, "hostkeys")
	if err := os.MkdirAll(cloneHostKeyDir, 0o750); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "cp", "-Rc", hostKey, cloneHostKeyDir)
	slog.InfoContext(ctx, "cloneHostKeyPair", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneHostKeyPair", "error", err, "output", string(output))
		return err
	}

	cmd = exec.CommandContext(ctx, "cp", "-Rc", hostKeyPub, cloneHostKeyDir)
	slog.InfoContext(ctx, "cloneHostKeyPair", "cmd", strings.Join(cmd.Args, " "))
	output, err = cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneHostKeyPair", "error", err, "output", string(output))
		return err
	}

	return nil
}

func (p *DefaultWorkspaceProvisioner) cloneClaudeDir(ctx context.Context, hostWorkDir, id string) error {
	if err := os.MkdirAll(filepath.Join(p.cloneRoot, id), 0o750); err != nil {
		return err
	}
	cloneClaude := filepath.Join(p.cloneRoot, id, "dotfiles", ".claude")
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

// Clones .claude.json but filters the "projects" map to just the one we're interested in,
// and updates its key to be "/app" which is the container-side path for it.
func (p *DefaultWorkspaceProvisioner) cloneClaudeJSON(ctx context.Context, cwd, id string) error {
	claudeJSON, err := filterClaudeJSON(ctx, cwd)
	if err != nil {
		return err
	}
	clone := filepath.Join(p.cloneRoot, id, "dotfiles", ".claude.json")
	err = os.WriteFile(clone, claudeJSON, 0o700)
	return err
}

func (p *DefaultWorkspaceProvisioner) cloneDotfiles(ctx context.Context, id string) error {
	p.userMsg(ctx, "Cloning dotfiles...")
	dotfiles := []string{
		".gitconfig",
		".p10k.zsh",
		".zshrc",
		".omp.json",
		".ssh/id_ed25519.pub",
	}
	for _, dotfile := range dotfiles {
		clone := filepath.Join(p.cloneRoot, id, "dotfiles", dotfile)
		original := filepath.Join(os.Getenv("HOME"), dotfile)
		fi, err := os.Lstat(original)
		if errors.Is(err, os.ErrNotExist) {
			p.userMsg(ctx, "skipping "+original)
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
				slog.ErrorContext(ctx, "Boxer.cloneDotfiles error reading symbolic link", "original", original, "error", err)
				continue
			}
			if !filepath.IsAbs(destination) {
				destination = filepath.Join(os.Getenv("HOME"), destination)
			}
			// Now verify that the file that the symlink points to actually exists.
			_, err = os.Lstat(destination)
			if errors.Is(err, os.ErrNotExist) {
				slog.ErrorContext(ctx, "Boxer.cloneDotfiles symbolic link points to nonexistent file",
					"original", original, "destination", destination, "error", err)
				f, err := os.Create(clone)
				if err != nil {
					return err
				}
				f.Close()
				continue
			}
			slog.InfoContext(ctx, "Boxer.cloneDotfiles resolved symbolic link",
				"original", original, "destination", destination)
			original = destination
		}
		cloneDir := filepath.Dir(clone)
		if err := os.MkdirAll(cloneDir, 0o750); err != nil {
			slog.ErrorContext(ctx, "cloneDotfiles couldn't make clone dir", "cloneDir", cloneDir, "error", err)
			return err
		}
		cmd := exec.CommandContext(ctx, "cp", "-Rc", original, clone)
		slog.InfoContext(ctx, "cloneDotfiles", "cmd", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.InfoContext(ctx, "cloneDotfiles", "error", err, "output", string(output))
			return err
		}
		p.userMsg(ctx, "cloned "+original)
	}

	return nil
}

func (p *DefaultWorkspaceProvisioner) defaultContainerHook() ContainerHook {
	return NewContainerHook("default container bootstrap", func(ctx context.Context, b *Box) error {
		var errs []error

		cpOut, err := b.Exec(ctx, "cp", "-r", "/dotfiles/.", "/root/.")
		if err != nil {
			slog.ErrorContext(ctx, "DefaultContainerHook copying dotfiles", "error", err, "cpOut", cpOut)
			errs = append(errs, fmt.Errorf("copy dotfiles: %w", err))
		}

		authorizedKeysOut, err := b.Exec(ctx, "cp", "-r", "/root/.ssh/id_ed25519.pub", "/root/.ssh/authorized_keys")
		if err != nil {
			slog.ErrorContext(ctx, "DefaultContainerHook copying authorized_keys", "error", err, "authorizedKeysOut", authorizedKeysOut)
			errs = append(errs, fmt.Errorf("copy authorized keys: %w", err))
		}

		hostKeysOut, err := b.Exec(ctx, "cp", "-r", "/hostkeys/.", "/etc/ssh/.")
		if err != nil {
			slog.ErrorContext(ctx, "DefaultContainerHook copying host keys", "error", err, "hostKeysOut", hostKeysOut)
			errs = append(errs, fmt.Errorf("copy host keys: %w", err))
		}

		sshdOut, err := b.Exec(ctx, "/usr/sbin/sshd", "-f", "/etc/ssh/sshd_config")
		if err != nil {
			slog.ErrorContext(ctx, "DefaultContainerHook starting sshd", "error", err, "sshdOut", sshdOut)
			errs = append(errs, fmt.Errorf("start sshd: %w", err))
		}

		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		slog.InfoContext(ctx, "DefaultContainerHook completed", "hook", "default container bootstrap")
		return nil
	})
}
