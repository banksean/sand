package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/banksean/sand/ansi"
	"github.com/nxadm/tail"
	"github.com/walles/moor/v2/pkg/moor"
)

var flagPager = flag.Bool("pager", false, "paginate output")

type writeFlusher interface {
	io.Writer
	Flush() error
}

func main() {
	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s <log file path>\n", os.Args[0])
		os.Exit(1)
	}
	inputPath := flag.Args()[0]

	ctx := context.Background()
	var writer writeFlusher
	var reader io.Reader

	pipeReader, pipeWriter := io.Pipe()
	buf := bufio.NewReadWriter(bufio.NewReader(pipeReader), bufio.NewWriter(pipeWriter))
	reader, writer = buf.Reader, buf.Writer

	h := NewHandler(nil, writer)

	t, err := tail.TailFile(inputPath, tail.Config{
		ReOpen:        true,
		Follow:        true,
		CompleteLines: true,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer t.Cleanup()

	go func() {
		for line := range t.Lines {
			decoder := json.NewDecoder(strings.NewReader(line.Text))
			var slogLine map[string]any
			if err := decoder.Decode(&slogLine); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
			}
			h.Handle(ctx, slogLine)
			writer.Flush()
		}
	}()
	if *flagPager {
		if err := moor.PageFromStream(reader, moor.Options{
			NoAutoFormat:  false,
			WrapLongLines: false,
			Title:         inputPath,
		}); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	} else {
		_, err := io.Copy(os.Stdout, reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err.Error())
		}
	}
}

const (
	timeFormat = "[15:04:05.000]"
)

type Handler struct {
	r                func([]string, slog.Attr) slog.Attr
	b                *bytes.Buffer
	m                *sync.Mutex
	writer           io.Writer
	colorize         bool
	outputEmptyAttrs bool
}

func (h *Handler) Handle(ctx context.Context, r map[string]any) error {
	colorize := func(code int, value string) string {
		return value
	}
	if h.colorize {
		colorize = ansi.Colorize
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
			levelName = colorize(ansi.LightGray, levelName)
		} else if level <= slog.LevelInfo {
			levelName = colorize(ansi.Cyan, levelName)
		} else if level < slog.LevelWarn {
			levelName = colorize(ansi.LightBlue, levelName)
		} else if level < slog.LevelError {
			levelName = colorize(ansi.LightYellow, levelName)
		} else if level <= slog.LevelError+1 {
			levelName = colorize(ansi.LightRed, levelName)
		} else if level > slog.LevelError+1 {
			levelName = colorize(ansi.LightMagenta, levelName)
		}
	}

	var timestamp string
	timestamp, ok = r[slog.TimeKey].(string)
	if ok {
		ts, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing timestamp %q: %v\n", timestamp, err)
		} else {
			timestamp = ts.Local().Format(timeFormat)
		}
		timestamp = colorize(ansi.LightGray, timestamp)
	}

	var msg string
	msg, ok = r[slog.MessageKey].(string)
	if !ok {
		msg = colorize(ansi.White, msg)
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
		out.WriteString(colorize(ansi.DarkGray, string(attrsAsBytes)))
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
