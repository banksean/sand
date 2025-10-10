package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestStatusBarWrite(t *testing.T) {
	buf := &bytes.Buffer{}
	sb := NewStatusBar(buf, "test.log")

	n, err := sb.Write([]byte("test\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 5 {
		t.Errorf("Expected to write 5 bytes, got %d", n)
	}
	if !strings.Contains(buf.String(), "test\n") {
		t.Errorf("Expected output to contain 'test\\n', got %q", buf.String())
	}
}

func TestStatusBarCleanupSafe(t *testing.T) {
	buf := &bytes.Buffer{}
	sb := NewStatusBar(buf, "test.log")

	sb.Cleanup()
}

func TestStatusBarUpdate(t *testing.T) {
	buf := &bytes.Buffer{}
	sb := NewStatusBar(buf, "test.log")

	sb.Update(100)
}
