//go:build darwin

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestResolveSanddPathPrefersSiblingBinary(t *testing.T) {
	dir := t.TempDir()
	sandPath := filepath.Join(dir, "sand")
	sanddPath := filepath.Join(dir, "sandd")
	writeExecutable(t, sandPath)
	writeExecutable(t, sanddPath)

	restore := stubPathResolvers(t)
	defer restore()
	executablePath = func() (string, error) { return sandPath, nil }
	lookPath = func(string) (string, error) {
		return filepath.Join(t.TempDir(), "sandd"), nil
	}

	got, err := resolveSanddPath()
	if err != nil {
		t.Fatalf("resolveSanddPath returned error: %v", err)
	}
	if got != sanddPath {
		t.Fatalf("resolveSanddPath = %q, want %q", got, sanddPath)
	}
}

func TestResolveSanddPathFallsBackToPath(t *testing.T) {
	dir := t.TempDir()
	sandPath := filepath.Join(dir, "sand")
	writeExecutable(t, sandPath)
	pathSandd := filepath.Join(t.TempDir(), "sandd")
	writeExecutable(t, pathSandd)

	restore := stubPathResolvers(t)
	defer restore()
	executablePath = func() (string, error) { return sandPath, nil }
	lookPath = func(string) (string, error) { return pathSandd, nil }

	got, err := resolveSanddPath()
	if err != nil {
		t.Fatalf("resolveSanddPath returned error: %v", err)
	}
	if got != pathSandd {
		t.Fatalf("resolveSanddPath = %q, want %q", got, pathSandd)
	}
}

func TestResolveSanddPathReportsMissingBinary(t *testing.T) {
	dir := t.TempDir()
	sandPath := filepath.Join(dir, "sand")
	writeExecutable(t, sandPath)

	restore := stubPathResolvers(t)
	defer restore()
	executablePath = func() (string, error) { return sandPath, nil }
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	_, err := resolveSanddPath()
	if err == nil {
		t.Fatal("resolveSanddPath returned nil error, want missing binary error")
	}
	if !strings.Contains(err.Error(), "install sandd") {
		t.Fatalf("resolveSanddPath error = %q, want install guidance", err.Error())
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing executable %s: %v", path, err)
	}
}

func stubPathResolvers(t *testing.T) func() {
	t.Helper()
	origExecutablePath := executablePath
	origLookPath := lookPath
	origStatPath := statPath
	statPath = os.Stat
	return func() {
		executablePath = origExecutablePath
		lookPath = origLookPath
		statPath = origStatPath
	}
}
