package runtimedeps

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/options"
)

type fakeContainerSystem struct {
	versionFunc     func(context.Context) (string, error)
	dnsListFunc     func(context.Context) ([]string, error)
	propertyGetFunc func(context.Context, string) (string, error)
	propertySetFunc func(context.Context, string, string) error
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

func (f *fakeContainerSystem) PropertyGet(ctx context.Context, id string) (string, error) {
	if f.propertyGetFunc != nil {
		return f.propertyGetFunc(ctx, id)
	}
	return "dev.local", nil
}

func (f *fakeContainerSystem) PropertySet(ctx context.Context, id, value string) error {
	if f.propertySetFunc != nil {
		return f.propertySetFunc(ctx, id, value)
	}
	return nil
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
		propertyGetFunc: func(context.Context, string) (string, error) {
			t.Fatal("PropertyGet should not be called after container command failure")
			return "", nil
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

func TestDNSDomainPromptSetsDefaultDomain(t *testing.T) {
	var gotID, gotValue string
	replaceSystemOps(t, &fakeContainerSystem{
		propertyGetFunc: func(context.Context, string) (string, error) {
			return "", nil
		},
		propertySetFunc: func(_ context.Context, id, value string) error {
			gotID = id
			gotValue = value
			return nil
		},
	})

	var stdout bytes.Buffer
	err := VerifyWithOptions(context.Background(), "", VerifyOptions{
		Stdin:            strings.NewReader("\n"),
		Stdout:           &stdout,
		PromptRemedies:   true,
		DefaultDNSDomain: DefaultDNSDomain,
	}, ContainerSystemDNSDomain)
	if err != nil {
		t.Fatalf("VerifyWithOptions() error = %v", err)
	}
	if gotID != "dns.domain" || gotValue != DefaultDNSDomain {
		t.Fatalf("PropertySet called with id=%q value=%q, want dns.domain %q", gotID, gotValue, DefaultDNSDomain)
	}
	if got := stdout.String(); got != "Set dns.domain to dev.local [Y/n]? " {
		t.Fatalf("prompt output = %q", got)
	}
}

func TestDNSDomainPromptDeclineReturnsManualCommand(t *testing.T) {
	replaceSystemOps(t, &fakeContainerSystem{
		propertyGetFunc: func(context.Context, string) (string, error) {
			return "", nil
		},
		propertySetFunc: func(context.Context, string, string) error {
			t.Fatal("PropertySet should not be called after decline")
			return nil
		},
	})

	err := VerifyWithOptions(context.Background(), "", VerifyOptions{
		Stdin:            strings.NewReader("n\n"),
		Stdout:           ioDiscard{},
		PromptRemedies:   true,
		DefaultDNSDomain: DefaultDNSDomain,
	}, ContainerSystemDNSDomain)
	if err == nil {
		t.Fatal("VerifyWithOptions() error = nil, want error")
	}
	if want := "container system property set dns.domain dev.local"; !strings.Contains(err.Error(), want) {
		t.Fatalf("VerifyWithOptions() error = %q, want command %q", err, want)
	}
}

func TestDNSDomainNonInteractiveReturnsManualCommand(t *testing.T) {
	replaceSystemOps(t, &fakeContainerSystem{
		propertyGetFunc: func(context.Context, string) (string, error) {
			return "", nil
		},
	})

	err := VerifyWithOptions(context.Background(), "", VerifyOptions{}, ContainerSystemDNSDomain)
	if err == nil {
		t.Fatal("VerifyWithOptions() error = nil, want error")
	}
	if want := "container system property set dns.domain dev.local"; !strings.Contains(err.Error(), want) {
		t.Fatalf("VerifyWithOptions() error = %q, want command %q", err, want)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
