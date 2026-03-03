package cli

import (
	"context"

	"github.com/banksean/sand/box"
	"github.com/banksean/sand/mux"
)

type Context struct {
	AppBaseDir string
	LogFile    string
	LogLevel   string
	CloneRoot  string
	Context    context.Context
	Boxer      *box.Boxer
	MuxClient  *mux.MuxClient
}
