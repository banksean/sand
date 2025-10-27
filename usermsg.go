package sand

import (
	"context"
	"fmt"
	"io"
	"log/slog"
)

type UserMessenger interface {
	Message(ctx context.Context, msg string)
}

type terminalMessenger struct {
	writer io.Writer
}

func NewTerminalMessenger(writer io.Writer) UserMessenger {
	return &terminalMessenger{writer: writer}
}

func (tm *terminalMessenger) Message(ctx context.Context, msg string) {
	if tm.writer == nil {
		slog.DebugContext(ctx, "userMsg (no writer)", "msg", msg)
		return
	}
	fmt.Fprintln(tm.writer, "\033[90m"+msg+"\033[0m")
}

type nullMessenger struct{}

func NewNullMessenger() UserMessenger {
	return &nullMessenger{}
}

func (nm *nullMessenger) Message(ctx context.Context, msg string) {
	slog.DebugContext(ctx, "userMsg (null messenger)", "msg", msg)
}
