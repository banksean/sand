//go:build darwin

package main

import (
	"path/filepath"
	"testing"
)

func TestEffectiveAppBaseDirUsesConfiguredValue(t *testing.T) {
	got, err := effectiveAppBaseDir("/tmp/sand-alt")
	if err != nil {
		t.Fatalf("effectiveAppBaseDir returned error: %v", err)
	}
	if got != "/tmp/sand-alt" {
		t.Fatalf("effectiveAppBaseDir = %q, want /tmp/sand-alt", got)
	}
}

func TestEffectiveAppBaseDirFallsBackToAppHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := effectiveAppBaseDir("")
	if err != nil {
		t.Fatalf("effectiveAppBaseDir returned error: %v", err)
	}
	want := filepath.Join(home, "Library", "Application Support", "Sand")
	if got != want {
		t.Fatalf("effectiveAppBaseDir = %q, want %q", got, want)
	}
}
