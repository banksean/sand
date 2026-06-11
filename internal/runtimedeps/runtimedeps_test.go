package runtimedeps

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer"
	"github.com/banksean/sand/internal/applecontainer/options"
)

type fakeContainerSystem struct {
	versionFunc   func(context.Context) (string, error)
	dnsListFunc   func(context.Context) ([]string, error)
	getConfigFunc func(context.Context) (*applecontainer.ContainerSystemConfig, error)
}

// Start implements [containerSystem].
func (f *fakeContainerSystem) Start(ctx context.Context, opts *options.SystemStart) (string, error) {
	panic("unimplemented")
}

// Status implements [containerSystem].
func (f *fakeContainerSystem) Status(ctx context.Context, opts *options.SystemStatus) (string, error) {
	panic("unimplemented")
}

func (f *fakeContainerSystem) Version(ctx context.Context) (string, error) {
	if f.versionFunc != nil {
		return f.versionFunc(ctx)
	}
	return "container CLI version " + AppleContainerVersion, nil
}

func (f *fakeContainerSystem) DNSList(ctx context.Context) ([]string, error) {
	if f.dnsListFunc != nil {
		return f.dnsListFunc(ctx)
	}
	return []string{"dev.local"}, nil
}

func (f *fakeContainerSystem) GetConfig(ctx context.Context) (*applecontainer.ContainerSystemConfig, error) {
	if f.getConfigFunc != nil {
		return f.getConfigFunc(ctx)
	}
	return nil, nil
}

func replaceSystemOps(t *testing.T, fake containerSystem) {
	t.Helper()
	prev := systemOps
	systemOps = fake
	t.Cleanup(func() { systemOps = prev })
}

func TestVerifyWithOptionsFailsFast(t *testing.T) {
	replaceSystemOps(t, &fakeContainerSystem{
		versionFunc: func(context.Context) (string, error) {
			return "", errors.New("not found")
		},
		getConfigFunc: func(context.Context) (*applecontainer.ContainerSystemConfig, error) {
			t.Fatal("PropertyGet should not be called after container command failure")
			return nil, nil
		},
	})

	err := VerifyWithOptions(context.Background(), "", VerifyOptions{}, ContainerCommand, ContainerSystemDNSDomain)
	if err == nil {
		t.Fatal("VerifyWithOptions() error = nil, want error")
	}
	if got := err.Error(); strings.Contains(got, string(ContainerSystemDNSDomain)) {
		t.Fatalf("VerifyWithOptions() error includes later DNS check: %v", err)
	}
}

func TestGitDirFailureIncludesDiagnosticDescription(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Chdir(t.TempDir())
	t.Setenv("GIT_DIR", "")
	t.Setenv("GIT_WORK_TREE", "")

	err := VerifyWithOptions(context.Background(), "", VerifyOptions{}, GitDir)
	if err == nil {
		t.Fatal("VerifyWithOptions() error = nil, want error")
	}
	for _, want := range []string{
		"should be invoked from a git directory",
		"exit status 128",
		"not a git repository",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("VerifyWithOptions() error = %q, want substring %q", err, want)
		}
	}
}

func TestContainerCommandMissingUsesVersionConstantInInstallerURL(t *testing.T) {
	replaceSystemOps(t, &fakeContainerSystem{
		versionFunc: func(context.Context) (string, error) {
			return "", errors.New("not found")
		},
	})

	err := VerifyWithOptions(context.Background(), "", VerifyOptions{}, ContainerCommand)
	if err == nil {
		t.Fatal("VerifyWithOptions() error = nil, want error")
	}
	got := err.Error()
	for _, want := range []string{
		"apple/container " + AppleContainerVersion + " is not installed",
		"/download/" + AppleContainerVersion + "/",
		"container-" + AppleContainerVersion + "-installer-signed.pkg",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("VerifyWithOptions() error = %q, want substring %q", got, want)
		}
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
