package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nxadm/tail"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <log file path>\n", os.Args[0])
		os.Exit(1)
	}
	inputPath := os.Args[1]

	statusBar := NewStatusBar(os.Stdout, filepath.Base(inputPath))
	if err := statusBar.Enable(); err == nil {
		defer statusBar.Cleanup()
	}

	h := NewHandler(nil, statusBar)

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

	lineCount := 0
	for line := range t.Lines {
		decoder := json.NewDecoder(strings.NewReader(line.Text))
		var slogLine map[string]any
		if err := decoder.Decode(&slogLine); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		h.Handle(ctx, slogLine)
		lineCount++
		statusBar.Update(lineCount)
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

type StatusBar struct {
	writer      io.Writer
	enabled     bool
	width       int
	height      int
	fileName    string
	lineCount   int
	mu          sync.Mutex
	resizeChan  chan os.Signal
	lastUpdate  time.Time
	lineBuffer  []string
	maxBuffer   int
}

func NewStatusBar(writer io.Writer, fileName string) *StatusBar {
	return &StatusBar{
		writer:     writer,
		fileName:   fileName,
		resizeChan: make(chan os.Signal, 1),
		lineBuffer: make([]string, 0, 1000),
		maxBuffer:  1000,
	}
}

func (s *StatusBar) Enable() error {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return fmt.Errorf("not a terminal")
	}

	width, height, err := term.GetSize(fd)
	if err != nil {
		return err
	}

	if height < 3 {
		return fmt.Errorf("terminal too small")
	}

	s.mu.Lock()
	s.width = width
	s.height = height
	s.enabled = true
	s.mu.Unlock()

	fmt.Fprintf(s.writer, "\x1b[?1049h")
	fmt.Fprintf(s.writer, "\x1b[2J")
	fmt.Fprintf(s.writer, "\x1b[1;1H")
	s.setupScrollRegion()

	signal.Notify(s.resizeChan, syscall.SIGWINCH)
	go s.handleResize()

	return nil
}

func (s *StatusBar) handleResize() {
	for range s.resizeChan {
		fd := int(os.Stdout.Fd())
		width, height, err := term.GetSize(fd)
		if err != nil {
			continue
		}

		s.mu.Lock()
		s.width = width
		s.height = height
		
		s.resetScrollRegion()
		fmt.Fprintf(s.writer, "\x1b[2J")
		fmt.Fprintf(s.writer, "\x1b[1;1H")
		
		linesToShow := len(s.lineBuffer)
		if linesToShow > height-2 {
			linesToShow = height - 2
		}
		startIdx := len(s.lineBuffer) - linesToShow
		if startIdx < 0 {
			startIdx = 0
		}
		
		for i := startIdx; i < len(s.lineBuffer); i++ {
			fmt.Fprint(s.writer, s.lineBuffer[i])
		}
		
		s.mu.Unlock()
		
		s.setupScrollRegion()
		s.redraw()
	}
}

func (s *StatusBar) resetScrollRegion() {
	fmt.Fprintf(s.writer, "\x1b[r")
}

func (s *StatusBar) clearStatusLine(lineNumber int) {
	fmt.Fprintf(s.writer, "\x1b[s")
	fmt.Fprintf(s.writer, "\x1b[%d;1H", lineNumber)
	fmt.Fprintf(s.writer, "\x1b[K")
	fmt.Fprintf(s.writer, "\x1b[u")
}

func (s *StatusBar) setupScrollRegion() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return
	}

	fmt.Fprintf(s.writer, "\x1b[1;%dr", s.height-1)
}

func (s *StatusBar) Update(lineCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return
	}

	s.lineCount = lineCount

	now := time.Now()
	if now.Sub(s.lastUpdate) < 100*time.Millisecond {
		return
	}
	s.lastUpdate = now

	s.redrawLocked()
}

func (s *StatusBar) redraw() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redrawLocked()
}

func (s *StatusBar) redrawLocked() {
	if !s.enabled {
		return
	}

	status := fmt.Sprintf(" ⎗ %s │ %d lines ", s.fileName, s.lineCount)
	if len(status) > s.width {
		status = status[:s.width]
	}

	fmt.Fprintf(s.writer, "\x1b[s")
	fmt.Fprintf(s.writer, "\x1b[%d;1H", s.height)
	fmt.Fprintf(s.writer, "\x1b[7m")
	fmt.Fprintf(s.writer, "%-*s", s.width, status)
	fmt.Fprintf(s.writer, "\x1b[0m")
	fmt.Fprintf(s.writer, "\x1b[u")
}

func (s *StatusBar) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	if s.enabled {
		s.lineBuffer = append(s.lineBuffer, string(p))
		if len(s.lineBuffer) > s.maxBuffer {
			s.lineBuffer = s.lineBuffer[len(s.lineBuffer)-s.maxBuffer:]
		}
	}
	s.mu.Unlock()
	
	n, err = s.writer.Write(p)
	if err != nil {
		return n, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.enabled {
		s.redrawLocked()
	}

	return n, nil
}

func (s *StatusBar) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return
	}

	signal.Stop(s.resizeChan)
	close(s.resizeChan)

	s.resetScrollRegion()
	fmt.Fprintf(s.writer, "\x1b[?1049l")

	s.enabled = false
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
