package cli

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/cli/agentlaunch"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/sshimmer"
	"github.com/goombaio/namegenerator"
)

type NewCmd struct {
	SandboxCreationFlags
	ProjectEnvFlag
	ShellFlags
	Agent       string `short:"a" placeholder:"<claude|codex|gemini|opencode>" help:"name of coding agent to use"`
	Branch      bool   `short:"b" default:"false" help:"create a new git branch, with the same name as the sandbox, inside the sandbox _container_ (not on your host workdir)"`
	Username    string `help:"name of default user to create (defaults to $USER)"`
	Uid         string `help:"id of default user to create (defaults to $UID)"`
	SandboxName string `arg:"" optional:"" help:"name of the sandbox to create"`
}

func (c *NewCmd) Run(k *kong.Kong, cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	slog.InfoContext(ctx, "NewCmd.Run")

	projCfg, userCfg, defaultsCfg, projCfgPath, userCfgPath, err := loadEffectiveConfigMaps(k)
	if err != nil {
		slog.WarnContext(ctx, "NewCmd: could not load effective config", "error", err)
	} else {
		walkMerge(nil, projCfg, userCfg, defaultsCfg, func(path []string, name string, projVal, userVal, defaultVal any) {
			var val any
			source := "default"
			if projVal != nil {
				val = projVal
				source = projCfgPath
			} else if userVal != nil {
				val = userVal
				source = userCfgPath
			} else if defaultVal != nil {
				val = defaultVal
			}
			if val != nil {
				slog.InfoContext(ctx, "effective config", "key", strings.Join(path, "."), "value", val, "source", source)
			}
		})
	}

	if err := runtimedeps.Verify(ctx, cctx.AppBaseDir, runtimedeps.GitDir); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		slog.ErrorContext(ctx, "os.Getwd", "error", err)
		return err
	}

	if c.CloneFromDir == "" {
		c.CloneFromDir = cwd
	}
	userInfo, err := user.Current()
	if err != nil {
		return err
	}
	if c.Username == "" {
		c.Username = userInfo.Username
	}
	if c.Uid == "" {
		c.Uid = userInfo.Uid
	}
	// Generate a new ID if one was not provided
	if c.SandboxName == "" {
		seed := time.Now().UTC().UnixNano()
		nameGenerator := namegenerator.NewNameGenerator(seed)
		c.SandboxName = nameGenerator.Generate()
	}

	if c.EnvFile != "" && !filepath.IsAbs(c.EnvFile) {
		c.EnvFile = filepath.Join(c.CloneFromDir, c.EnvFile)
	}

	if c.Branch {
		if err := validateNewSandboxBranch(ctx, hostops.NewDefaultGitOps(), c.CloneFromDir, c.SandboxName); err != nil {
			return err
		}
	}

	if c.ImageName == "" {
		c.ImageName = agentlaunch.DefaultImage(c.Agent, DefaultImageName)
	}
	if err := mc.EnsureImage(ctx, c.ImageName, os.Stdout); err != nil {
		return fmt.Errorf("ensuring image %s: %w", c.ImageName, err)
	}

	var allowedDomains []string
	if c.AllowedDomainsFile != "" {
		if err := runtimedeps.Verify(ctx, cctx.AppBaseDir, runtimedeps.CustomInitImagePulled, runtimedeps.CustomKernelInstalled); err != nil {
			return err
		}

		domains, err := loadDomainsFile(c.AllowedDomainsFile)
		if err != nil {
			return fmt.Errorf("reading allowed-domains-file: %w", err)
		}
		allowedDomains = domains
	}

	// Try to get existing sandbox.
	// TODO: Consider returning an error here, rather than trying to "do the right thing" despite what the user asked for.
	sbox, err := mc.GetSandbox(ctx, c.SandboxName)
	if sbox == nil || err != nil {
		// Sandbox doesn't exist, create it via daemon
		slog.InfoContext(ctx, "Creating new sandbox via daemon", "name", c.SandboxName)
		sbox, err = mc.CreateSandbox(ctx, daemon.CreateSandboxOpts{
			Name:           c.SandboxName,
			CloneFromDir:   c.CloneFromDir,
			ProfileName:    c.ProfileName,
			ImageName:      c.ImageName,
			EnvFile:        c.EnvFile,
			Agent:          c.Agent,
			SSHAgent:       c.SSHAgent,
			AllowedDomains: allowedDomains,
			Volumes:        c.Volume,
			SharedCaches:   cctx.SharedCaches,
			CPUs:           c.CPU,
			Memory:         c.Memory,
			Username:       c.Username,
			Uid:            c.Uid,
		}, os.Stdout)
		if err != nil {
			slog.ErrorContext(ctx, "CreateSandbox", "error", err)
			return err
		}
	}

	if sbox.ImageName == "" {
		sbox.ImageName = DefaultImageName
	}

	// At this point the sandbox and container exist and are running (created by daemon)
	// Now attach to the shell directly (not through daemon)
	if sbox.Container == nil {
		return fmt.Errorf("sandbox's container field is nil")
	}
	hostname := types.GetContainerHostname(sbox.Container)

	slog.InfoContext(ctx, "main: sbox.new starting")

	fmt.Printf("Connecting you to %q (%s). CPUs: %d, Mem: %dMB, dns: %s\n", sbox.Name, sbox.ID,
		sbox.Container.Configuration.Resources.CPUs,
		sbox.Container.Configuration.Resources.MemoryInBytes>>20,
		hostname)

	if c.Branch {
		// Create and check out a git branch inside the container, named after the sandbox name.
		projectEnv, err := plainCommandProjectEnv(sbox, c.ProjectEnv)
		if err != nil {
			return err
		}
		defer projectEnv.Cleanup()
		if err := checkoutSandboxBranch(ctx, hostops.NewAppleContainerOps(), sbox, projectEnv); err != nil {
			return err
		}
	}

	updateSSHConfFunc, err := sshimmer.CheckSSHReachability(ctx, hostname)
	if err != nil {
		slog.ErrorContext(ctx, "sshimmer.CheckSSHReachability", "error", err)
	}
	if updateSSHConfFunc != nil {
		stdinReader := *bufio.NewReader(os.Stdin)
		fmt.Printf("\nTo enable you to use ssh to connect to local sand containers, we need to add one line to the top of your ssh config. Proceed [y/N]? ")
		text, err := stdinReader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("couldn't read from stdin: %w", err)
		}
		text = strings.TrimSpace(strings.ToLower(text))
		if text != "y" && text != "Y" {
			return fmt.Errorf("User declined to edit ssh config file")
		}
		if err := updateSSHConfFunc(); err != nil {
			return err
		}
	}

	// TODO: Sort out how "new" and "shell" should work when invoked inside a container.
	shell, args, err := agentlaunch.BuildInteractiveExec(c.Agent, c.Shell, sbox.Name, hostname, c.Tmux, c.Atch)
	if err != nil {
		return err
	}

	var agentEnv map[string]string
	if c.Agent != "" {
		agentEnv, err = resolveAgentLaunchEnv(ctx, mc, c.Agent, sbox)
		if err != nil {
			return err
		}
	}

	var shellEnv plainCommandEnv
	if c.Agent == "" {
		shellEnv, err = plainCommandProjectEnv(sbox, c.ProjectEnv)
		if err != nil {
			return err
		}
		defer shellEnv.Cleanup()
	}

	if err := runShell(ctx, sbox, shell, args, c.Agent != "", shellEnv.EnvFile, mergeEnv(shellEnv.Env, agentEnv)); err != nil {
		return err
	}

	if c.Rm {
		slog.InfoContext(ctx, "sbox.new finished, cleaning up...")
		if err := mc.RemoveSandbox(ctx, sbox.Name); err != nil {
			slog.ErrorContext(ctx, "RemoveSandbox", "error", err)
		}
		slog.InfoContext(ctx, "Cleanup complete. Exiting.")
	}
	return nil
}

func validateNewSandboxBranch(ctx context.Context, gitOps hostops.GitOps, cloneFromDir, sandboxName string) error {
	if !gitOps.LocalBranchExists(ctx, cloneFromDir, sandboxName) {
		return nil
	}
	return fmt.Errorf("branch name %q is already taken in %q", sandboxName, cloneFromDir)
}

func checkoutSandboxBranch(ctx context.Context, containerSvc hostops.ContainerOps, sbox *sandtypes.Box, projectEnv plainCommandEnv) error {
	execOpts := &options.ExecContainer{
		ProcessOptions: options.ProcessOptions{
			WorkDir: "/app",
			Env:     projectEnv.Env,
			EnvFile: projectEnv.EnvFile,
			User:    sbox.Username,
			UID:     sbox.Uid,
		},
	}

	out, err := containerSvc.Exec(ctx, execOpts, sbox.ContainerID, "git", os.Environ(),
		"config", "--global", "--add", "safe.directory", "/app")
	if err != nil {
		slog.ErrorContext(ctx, "sbox.new git checkout, config", "error", err, "out", out)
		return fmt.Errorf("configuring git in sandbox %q: %w", sbox.ID, err)
	}

	out, err = containerSvc.Exec(ctx, execOpts, sbox.ContainerID, "git", os.Environ(),
		"checkout", "-b", sbox.Name)
	if err != nil {
		slog.ErrorContext(ctx, "sbox.new git checkout", "error", err, "out", out)
		if out != "" {
			return fmt.Errorf("creating branch %q in sandbox %q: %w: %s", sbox.Name, sbox.Name, err, strings.TrimSpace(out))
		}
		return fmt.Errorf("creating branch %q in sandbox %q: %w", sbox.Name, sbox.Name, err)
	}
	return nil
}

func loadDomainsFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var domains []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if d := sc.Text(); d != "" && d[0] != '#' {
			domains = append(domains, d)
		}
	}
	return domains, sc.Err()
}
