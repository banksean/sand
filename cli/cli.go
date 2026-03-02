package cli

import (
	"context"

	"github.com/banksean/sand/box"
)

type Context struct {
	AppBaseDir string
	LogFile    string
	LogLevel   string
	CloneRoot  string
	Context    context.Context
	Boxer      *box.Boxer
}
