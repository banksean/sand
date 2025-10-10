package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nxadm/tail"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <log file path>\n", os.Args[0])
		os.Exit(1)
	}
	inputPath := os.Args[1]

	h := NewHandler(nil, os.Stdout)

	t, err := tail.TailFile(inputPath, tail.Config{
		ReOpen:        true,
		Follow:        true,
		CompleteLines: true,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx := context.Background()

	for line := range t.Lines {
		decoder := json.NewDecoder(strings.NewReader(line.Text))
		var slogLine map[string]any
		if err := decoder.Decode(&slogLine); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		h.Handle(ctx, slogLine)
	}
	err = t.Wait()
	if err != nil {
		fmt.Println(err)
	}
}

const (
	timeFormat = "[15:04:05.000]"

	reset = "\033[0m"

	black        = 30
	red          = 31
	green        = 32
	yellow       = 33
	blue         = 34
	magenta      = 35
	cyan         = 36
	lightGray    = 37
	darkGray     = 90
	lightRed     = 91
	lightGreen   = 92
	lightYellow  = 93
	lightBlue    = 94
	lightMagenta = 95
	lightCyan    = 96
	white        = 97
)

func colorizer(colorCode int, v string) string {
	return fmt.Sprintf("\033[%sm%s%s", strconv.Itoa(colorCode), v, reset)
}

type Handler struct {
	r                func([]string, slog.Attr) slog.Attr
	b                *bytes.Buffer
	m                *sync.Mutex
	writer           io.Writer
	colorize         bool
	outputEmptyAttrs bool
}

func (h *Handler) computeAttrs(
	r map[string]any,
) map[string]any {
	ret := r
	delete(ret, slog.LevelKey)
	delete(ret, slog.TimeKey)
	delete(ret, slog.MessageKey)

	return ret
}

func (h *Handler) Handle(ctx context.Context, r map[string]any) error {
	colorize := func(code int, value string) string {
		return value
	}
	if h.colorize {
		colorize = colorizer
	}

	levelName, ok := r[slog.LevelKey].(string)
	if !ok {
		return fmt.Errorf("level is not a string")
	}

	levelAttr := slog.Attr{
		Key:   slog.LevelKey,
		Value: slog.AnyValue(levelName),
	}
	if h.r != nil {
		levelAttr = h.r([]string{}, levelAttr)
	}

	var level slog.Level
	switch strings.ToUpper(levelName) {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		return fmt.Errorf("unknown level name %q", levelName)
	}

	if !levelAttr.Equal(slog.Attr{}) {
		levelName = levelAttr.Value.String() + ":"

		if level <= slog.LevelDebug {
			levelName = colorize(lightGray, levelName)
		} else if level <= slog.LevelInfo {
			levelName = colorize(cyan, levelName)
		} else if level < slog.LevelWarn {
			levelName = colorize(lightBlue, levelName)
		} else if level < slog.LevelError {
			levelName = colorize(lightYellow, levelName)
		} else if level <= slog.LevelError+1 {
			levelName = colorize(lightRed, levelName)
		} else if level > slog.LevelError+1 {
			levelName = colorize(lightMagenta, levelName)
		}
	}

	var timestamp string
	timestamp, ok = r[slog.TimeKey].(string)
	if ok {
		ts, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing timestamp %q: %v\n", timestamp, err)
			timestamp = colorize(lightGray, timestamp)
		} else {
			timestamp = ts.Local().Format(time.DateTime)
		}
	}

	var msg string
	msg, ok = r[slog.MessageKey].(string)
	if !ok {
		msg = colorize(white, msg)
	}

	delete(r, slog.LevelKey)
	delete(r, slog.TimeKey)
	delete(r, slog.MessageKey)

	var attrsAsBytes []byte
	var err error
	if h.outputEmptyAttrs || len(r) > 0 {
		attrsAsBytes, err = json.MarshalIndent(r, "", "  ")
		if err != nil {
			return fmt.Errorf("error when marshaling attrs: %w", err)
		}
	}

	out := strings.Builder{}
	if len(timestamp) > 0 {
		out.WriteString(timestamp)
		out.WriteString(" ")
	}
	if len(levelName) > 0 {
		out.WriteString(levelName)
		out.WriteString(" ")
	}
	if len(msg) > 0 {
		out.WriteString(msg)
		out.WriteString(" ")
	}
	if len(attrsAsBytes) > 0 {
		out.WriteString(colorize(darkGray, string(attrsAsBytes)))
	}

	_, err = io.WriteString(h.writer, out.String()+"\n")
	if err != nil {
		return err
	}

	return nil
}

func NewHandler(handlerOptions *slog.HandlerOptions, writer io.Writer) *Handler {
	if handlerOptions == nil {
		handlerOptions = &slog.HandlerOptions{}
	}

	buf := &bytes.Buffer{}
	handler := &Handler{
		b:                buf,
		r:                handlerOptions.ReplaceAttr,
		m:                &sync.Mutex{},
		outputEmptyAttrs: true,
		colorize:         true,
		writer:           writer,
	}

	return handler
}
