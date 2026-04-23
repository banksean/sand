package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxFanoutHandlerWritesPerSandboxLogs(t *testing.T) {
	t.Parallel()

	logFile := filepath.Join(t.TempDir(), "daemon.log")
	dir := SandboxLogsDir(logFile)
	base := slog.NewTextHandler(&bytes.Buffer{}, nil)
	handler, err := NewSandboxFanoutHandler(base, dir, &slog.HandlerOptions{Level: slog.LevelDebug})
	if err != nil {
		t.Fatalf("NewSandboxFanoutHandler() error = %v", err)
	}

	logger := slog.New(handler)
	logger.InfoContext(context.Background(), "created sandbox", sandboxIDAttrKey, "sand-1")
	logger.InfoContext(context.Background(), "stopped sandbox", "sandbox", "sand-2")

	var sand1 bytes.Buffer
	if err := copySandboxLog(logFile, "sand-1", &sand1); err != nil {
		t.Fatalf("copySandboxLog(sand-1) error = %v", err)
	}
	if !strings.Contains(sand1.String(), "created sandbox") {
		t.Fatalf("sand-1 log missing message: %q", sand1.String())
	}

	var sand2 bytes.Buffer
	if err := copySandboxLog(logFile, "sand-2", &sand2); err != nil {
		t.Fatalf("copySandboxLog(sand-2) error = %v", err)
	}
	if !strings.Contains(sand2.String(), "stopped sandbox") {
		t.Fatalf("sand-2 log missing message: %q", sand2.String())
	}
}

func TestCopySandboxLogMissingFile(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := copySandboxLog(filepath.Join(t.TempDir(), "daemon.log"), "missing-sandbox", &buf)
	if err == nil {
		t.Fatal("copySandboxLog() error = nil, want missing log error")
	}
}
