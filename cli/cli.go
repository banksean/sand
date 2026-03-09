// package cli contains reusable implemeations of cli subcommands.
// The struct types contain field tags that github.com/alecthomas/kong reads and interprets
// to provide automatic documentation, default flag values and so on.
//
// In general, code in this package should not depend on any sand/mux details besides the
// sand/mux/MuxClient type. That type handles the transport details for communicating with
// the sandd daemon, whether by unix domain socket (when running on the host OS) or by TCP
// socket (when running inside a container).
package cli

import (
	"context"

	"github.com/banksean/sand/mux"
)

type CLIContext struct {
	AppBaseDir string
	LogFile    string
	LogLevel   string
	CloneRoot  string
	Context    context.Context
	MuxClient  *mux.MuxClient
}

// ShellFlags are shared by commands that exec a shell inside a container.
type ShellFlags struct {
	Shell string `short:"s" default:"/bin/zsh" placeholder:"<shell-command>" help:"shell command to exec in the container"`
}

// SandboxCreationFlags are shared by commands that create a sandbox.
type SandboxCreationFlags struct {
	ImageName string `short:"i" default:"ghcr.io/banksean/sand/default:latest" placeholder:"<container-image-name>" help:"name of container image to use"`
	Rm        bool   `help:"remove the sandbox after the command terminates"`
}

// SandboxSelectionFlags are shared by commands that operate on a sandbox by ID or on all sandboxes.
type SandboxSelectionFlags struct {
	ID  string `arg:"" completion-predictor:"sandbox-name" optional:"" help:"ID of the sandbox"`
	All bool   `short:"a" help:"all sandboxes"`
}
