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
