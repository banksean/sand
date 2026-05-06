package runtimepaths

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerSocketPathsUseFixedLengthNames(t *testing.T) {
	shortID := "sandbox"
	longID := "12703e28-ec0d-4490-a5c8-fc914577a73b-extra-long-suffix"

	shortBase := filepath.Base(ContainerHTTPSocketPath(shortID))
	longBase := filepath.Base(ContainerHTTPSocketPath(longID))
	if len(shortBase) != len(longBase) {
		t.Fatalf("socket basename lengths differ: %q (%d), %q (%d)", shortBase, len(shortBase), longBase, len(longBase))
	}
	if !strings.HasSuffix(shortBase, ".sock") || !strings.HasSuffix(longBase, ".sock") {
		t.Fatalf("socket basenames should end with .sock: %q, %q", shortBase, longBase)
	}
}

func TestContainerSocketPathsUseShortRuntimeRoot(t *testing.T) {
	path := ContainerGRPCSocketPath("sandbox")
	if !strings.HasPrefix(path, "/tmp/sand-") {
		t.Fatalf("socket path = %q, want /tmp/sand-<uid>/...", path)
	}
	if strings.Contains(path, "Library/Application Support/Sand") {
		t.Fatalf("socket path should not use app base dir: %q", path)
	}
}
