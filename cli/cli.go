// package cli contains reusable implemeations of cli subcommands.
// The struct types contain field tags that github.com/alecthomas/kong reads and interprets
// to provide automatic documentation, default flag values and so on.
//
// In general, code in this package should not depend on any sand/mux details besides the
// sand/daemon#Client type. That type handles the transport details for communicating with
// the sandd daemon, whether by unix domain socket (when running on the host OS) or by TCP
// socket (when running inside a container).
package cli

import (
	"context"

	"github.com/banksean/sand/daemon"
)

type CLIContext struct {
	AppBaseDir string
	LogFile    string
	LogLevel   string
	CloneRoot  string
	Context    context.Context
	Daemon     daemon.Client
}

const (
	DefaultImageName = "ghcr.io/banksean/sand/default:latest"
)

// ShellFlags are shared by commands that exec a shell inside a container.
type ShellFlags struct {
	Shell string `short:"s" default:"/bin/zsh" placeholder:"<shell-command>" help:"shell command to exec in the container"`
}

// SandboxCreationFlags are shared by commands that create a sandbox.
type SandboxCreationFlags struct {
	ImageName          string `short:"i" placeholder:"<container-image-name>" help:"name of container image to use"`
	CloneFromDir       string `short:"d" placeholder:"<project-dir>" help:"directory to clone into the sandbox. Defaults to current working directory, if unset."`
	EnvFile            string `short:"e" default:".env" placholder:"<file-path>" help:"path to env file to use when creating a new shell"`
	Rm                 bool   `help:"remove the sandbox after the command terminates"`
	AllowedDomainsFile string `placeholder:"<file-path>" help:"path to allowed-domains.txt file for DNS egress filtering (overrides the init image default)"`
}

// SandboxNameFlag is shared by commands that require a single sandbox name argument.
type SandboxNameFlag struct {
	SandboxName string `arg:"" completion-predictor:"sandbox-name" help:"name of the sandbox"`
}

// MultiSandboxNameFlags are shared by commands that operate on a sandbox by name or on all sandboxes.
type MultiSandboxNameFlags struct {
	SandboxName string `arg:"" completion-predictor:"sandbox-name" optional:"" help:"name of the sandbox"`
	All         bool   `short:"a" help:"all sandboxes"`
}
