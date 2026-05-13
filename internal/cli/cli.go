// package cli contains reusable implemeations of cli subcommands.
// The struct types contain field tags that github.com/alecthomas/kong reads and interprets
// to provide automatic documentation, default flag values and so on.
//
// In general, code in this package should not depend on any internal/daemon details besides the
// internal/daemon#Client type. That type handles the transport details for communicating with
// the sandd daemon, whether by unix domain socket (when running on the host OS) or by TCP
// socket (when running inside a container).
package cli

import (
	"context"

	"github.com/banksean/sand/internal/sandtypes"

	"github.com/banksean/sand/internal/daemon"
)

type CLIContext struct {
	AppBaseDir   string
	LogFile      string
	LogLevel     string
	CloneRoot    string
	Context      context.Context
	Daemon       daemon.Client
	SharedCaches sandtypes.SharedCacheConfig
}

const (
	DefaultImageName = "ghcr.io/banksean/sand/default:latest"
)

// ShellFlags are shared by commands that exec a shell inside a container.
type ShellFlags struct {
	Shell string `short:"s" default:"/bin/zsh" placeholder:"<shell-command>" help:"shell command to exec in the container"`
	Tmux  bool   `short:"t" help:"create or reconnect to a container-side tmux session"`
	Atch  bool   `help:"create or reconnect to a container-side atch session"`
}

type SSHAgentFlag struct {
	SSHAgent bool `help:"enable ssh-agent forwarding for the container"`
}

type ProjectEnvFlag struct {
	ProjectEnv bool `help:"pass project-scoped profile env to plain shell/exec/git commands"`
}

// SandboxCreationFlags are shared by commands that create a sandbox.
type SandboxCreationFlags struct {
	SSHAgentFlag
	ImageName          string   `name:"image" short:"i" placeholder:"<container-image-name>" help:"name of container image to use"`
	CloneFromDir       string   `short:"d" placeholder:"<project-dir>" help:"directory to clone into the sandbox. Defaults to current working directory, if unset."`
	ProfileName        string   `name:"profile" default:"default" placeholder:"<profile-name>" help:"profile policy from .sand.yaml to associate with the sandbox"`
	EnvFile            string   `short:"e" default:".env" placeholder:"<file-path>" help:"legacy env file path used when no default profile is configured"`
	Rm                 bool     `help:"remove the sandbox after the command terminates"`
	AllowedDomainsFile string   `placeholder:"<file-path>" help:"path to allowed-domains.txt file for DNS egress filtering (overrides the init image default)"`
	Volume             []string `short:"v" placeholder:"<host-path:container-path>" help:"bind mount a volume (can be specified multiple times)"`
	CPU                int      `help:"number of CPUs to allocate to the container" default:"2"`
	Memory             int      `help:"how much memory in MiB to allocate to the container" default:"1024"`
}

// SandboxNameFlag is shared by commands that require a single sandbox name argument.
type SandboxNameFlag struct {
	SandboxName string `arg:"" completion-predictor:"sandbox-name" help:"name of the sandbox"`
}

// MultiSandboxNameFlags are shared by commands that operate on a sandbox by name or on all sandboxes.
type MultiSandboxNameFlags struct {
	SandboxNames []string `arg:"" completion-predictor:"sandbox-name" optional:"" help:"names of the sandboxes"`
	All          bool     `short:"a" help:"all sandboxes"`
}
