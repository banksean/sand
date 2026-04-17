package cli

import (
	"bufio"
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
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/banksean/sand/internal/sshimmer"
	"github.com/goombaio/namegenerator"
)

type NewCmd struct {
	SandboxCreationFlags
	ShellFlags
	Agent       string `short:"a" placeholder:"<claude|codex|opencode>" help:"name of coding agent to use"`
	Branch      bool   `short:"b" help:"create a new git branch inside the sandbox _container_ (not on your host workdir)"`
	Prompt      string `short:"p" placeholder:"<prompt>" help:"start the agent with this prompt in non-interactive (one-shot) mode and return immediately"`
	Username    string `help:"name of default user to create (defaults to $USER)"`
	Uid         string `help:"id of default user to create (defaults to $UID)"`
	SandboxName string `arg:"" optional:"" help:"name of the sandbox to create"`
}

var defaultImageForAgent = map[string]string{
	"claude":   "ghcr.io/banksean/sand/claude:latest",
	"codex":    "ghcr.io/banksean/sand/codex:latest",
	"default":  "ghcr.io/banksean/sand/default:latest",
	"opencode": "ghcr.io/banksean/sand/opencode:latest",
}

func (c *NewCmd) Run(k *kong.Kong, cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	slog.InfoContext(ctx, "NewCmd.Run")

	projCfg, userCfg, defaultsCfg, userCfgPath, err := loadEffectiveConfigMaps(k)
	if err != nil {
		slog.WarnContext(ctx, "NewCmd: could not load effective config", "error", err)
	} else {
		walkMerge(nil, projCfg, userCfg, defaultsCfg, func(path []string, name string, projVal, userVal, defaultVal any) {
			var val any
			source := "default"
			if projVal != nil {
				val = projVal
				source = "./.sand.yaml"
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

	// When a prompt is given, default to claude for workspace preparation.
	if c.Prompt != "" && c.Agent == "default" {
		c.Agent = "claude"
	}

	if c.ImageName == "" {
		if c.Agent != "" {
			img, ok := defaultImageForAgent[c.Agent]
			if ok {
				c.ImageName = img
			} else {
				c.ImageName = DefaultImageName
			}
		} else {
			c.ImageName = DefaultImageName
		}
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
		slog.InfoContext(ctx, "Creating new sandbox via daemon", "id", c.SandboxName)
		sbox, err = mc.CreateSandbox(ctx, daemon.CreateSandboxOpts{
			ID:             c.SandboxName,
			CloneFromDir:   c.CloneFromDir,
			ImageName:      c.ImageName,
			EnvFile:        c.EnvFile,
			Agent:          c.Agent,
			AllowedDomains: allowedDomains,
			Volumes:        c.Volume,
			CPUs:           c.CPU,
			Memory:         c.Memory,
			Username:       c.Username,
			Uid:            c.Uid,
		})
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
	ctr := sbox.Container

	if ctr == nil {
		return fmt.Errorf("sandbox's container field is nil")
	}
	hostname := types.GetContainerHostname(ctr)
	env := map[string]string{
		"HOSTNAME": hostname,
		"LANG":     os.Getenv("LANG"),
		"TERM":     os.Getenv("TERM"),
	}

	slog.InfoContext(ctx, "main: sbox.new starting")

	fmt.Printf("Connecting you to %q. CPUs: %d, Mem: %dMB, dns: %s\n", sbox.ID,
		sbox.Container.Configuration.Resources.CPUs,
		sbox.Container.Configuration.Resources.MemoryInBytes>>20,
		hostname)

	if c.Branch {
		// Create and check out a git branch inside the container, named after the sandbox id
		containerSvc := hostops.NewAppleContainerOps()
		out, err := containerSvc.Exec(ctx,
			&options.ExecContainer{
				ProcessOptions: options.ProcessOptions{
					WorkDir: "/app",
					EnvFile: sbox.EnvFile,
					User:    c.Username,
					UID:     c.Uid,
				},
			}, sbox.ContainerID, "git", os.Environ(), "checkout", "-b", sbox.ID)
		if err != nil {
			slog.ErrorContext(ctx, "sbox.new git checkout", "error", err, "out", out)
		}
	}

	if c.Prompt != "" {
		// One-shot mode: run the agent inside the container, streaming output to stdio.
		// The prompt is passed via an env var to avoid shell quoting issues.
		containerSvc := hostops.NewAppleContainerOps()
		agentOneShotCmd := ""
		switch c.Agent {
		case "claude":
			agentOneShotCmd = `claude --permission-mode=bypassPermissions --print "$SAND_ONESHOT_PROMPT"`
		case "opencode":
			agentOneShotCmd = `opencode run "$SAND_ONESHOT_PROMPT"`
		default:
			return fmt.Errorf("one-shot prompt mode not supported for %q", c.Agent)
		}
		wait, err := containerSvc.ExecStream(ctx,
			&options.ExecContainer{
				ProcessOptions: options.ProcessOptions{
					WorkDir: "/app",
					EnvFile: sbox.EnvFile,
					Env:     map[string]string{"SAND_ONESHOT_PROMPT": c.Prompt},
					User:    c.Username,
					UID:     c.Uid,
				},
			}, sbox.ContainerID, "/bin/sh", os.Environ(),
			os.Stdin, os.Stdout, os.Stderr,
			"-c", agentOneShotCmd)
		if err != nil {
			slog.ErrorContext(ctx, "NewCmd: start agent oneshot", "error", err)
			return fmt.Errorf("failed to start agent in sandbox %s: %w", sbox.ID, err)
		}
		if err := wait(); err != nil {
			slog.ErrorContext(ctx, "NewCmd: agent oneshot wait", "error", err)
		}
		return nil
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

	// This will only work on the *host* OS, since it makes calls to apple's container service.
	// TODO: Sort out how "new" and "shell" should work when invoked inside a container.
	containerSvc := hostops.NewAppleContainerOps()
	ctrs, err := containerSvc.Inspect(ctx, sbox.ContainerID)
	if err != nil {
		return fmt.Errorf("could not inspect container for sandbox %s: %w", sbox.ContainerID, err)
	}
	if len(ctrs) == 0 {
		return fmt.Errorf("no container for sandbox %s", sbox.ContainerID)
	}
	var args []string
	switch c.Agent {
	case "claude":
		if c.Tmux {
			args = []string{"new-session", "-A", "-s", "claude-" + sbox.ID, "claude --permission-mode=bypassPermissions"}
		} else {
			args = []string{"-c", "claude --permission-mode=bypassPermissions"}
		}
	case "opencode":
		args = []string{"-c", "opencode --port 80 --hostname " + strings.TrimSuffix(ctrs[0].Networks[0].Hostname, ".")}
	}
	if c.Tmux {
		c.Shell = "/usr/bin/tmux"
		if c.Agent == "" {
			args = []string{"new-session", "-A", "-s", sbox.ID}
		}
	}

	cmdEnv := os.Environ()

	wait, err := containerSvc.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
				Env:         env,
				EnvFile:     sbox.EnvFile,
				User:        c.Username,
				UID:         c.Uid,
			},
		}, sbox.ContainerID, c.Shell, cmdEnv, os.Stdin, os.Stdout, os.Stderr, args...)
	if err != nil {
		slog.ErrorContext(ctx, "shell: containerService.ExecStream", "sandbox", sbox.ID, "error", err)
		return fmt.Errorf("failed to execute shell command for sandbox %s: %w", sbox.ID, err)
	}
	if err := wait(); err != nil {
		slog.ErrorContext(ctx, "sbox.new", "error", err)
	}

	if c.Rm {
		slog.InfoContext(ctx, "sbox.new finished, cleaning up...")
		if err := mc.RemoveSandbox(ctx, sbox.ID); err != nil {
			slog.ErrorContext(ctx, "RemoveSandbox", "error", err)
		}
		slog.InfoContext(ctx, "Cleanup complete. Exiting.")
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
