package cloning

import (
	"testing"
)

func TestGeneratedFileTargetRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"", "/abs/path", "..", "../secret"} {
		if _, err := generatedFileTarget("/sandbox/dotfiles", path); err == nil {
			t.Fatalf("generatedFileTarget(%q) error = nil, want error", path)
		}
	}
}

func TestGeneratedFileTargetAllowsNestedRelativePath(t *testing.T) {
	got, err := generatedFileTarget("/sandbox/dotfiles", ".config/opencode/opencode.json")
	if err != nil {
		t.Fatalf("generatedFileTarget() error = %v", err)
	}
	if got != "/sandbox/dotfiles/.config/opencode/opencode.json" {
		t.Fatalf("target = %q", got)
	}
}
