package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	applecontainer "github.com/banksean/apple-container"
)

const expectedContainerVersion = "container CLI version 0.5.0"

func verifyPrerequisites(ctx context.Context) error {
	version, err := applecontainer.System.Version(ctx)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "verifyPrerequisites", "version", version)
	if !strings.Contains(version, expectedContainerVersion) {
		return fmt.Errorf("expected container command version %q, but got %q", expectedContainerVersion, version)
	}
	return nil
}
