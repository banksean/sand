package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	applecontainer "github.com/banksean/apple-container"
)

const appleContainerVersion = "0.5.0"
const minimumMacOSVersion = 26

func verifyPrerequisites(ctx context.Context) error {
	// Check macOS version
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("this program requires macOS %d or greater, but detected OS: %s", minimumMacOSVersion, runtime.GOOS)
	}

	majorVersion, err := getMacOSMajorVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get macOS version: %w", err)
	}
	if majorVersion < minimumMacOSVersion {
		return fmt.Errorf("macOS version %d detected, but version %d or greater is required", majorVersion, minimumMacOSVersion)
	}

	version, err := applecontainer.System.Version(ctx)
	if err != nil {
		return fmt.Errorf("could not locate Apple's `container` command from the releases published at https://github.com/apple/container/releases/tag/%s", appleContainerVersion)
	}
	slog.InfoContext(ctx, "verifyPrerequisites", "version", version)
	if !strings.Contains("container CLI version "+version, appleContainerVersion) {
		return fmt.Errorf("expected container command version %q, but got %q", appleContainerVersion, version)
	}
	return nil
}

func getMacOSMajorVersion(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "sw_vers", "-productVersion")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	version := strings.TrimSpace(string(output))
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid version format: %s", version)
	}

	majorVersion, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("failed to parse major version: %w", err)
	}

	return majorVersion, nil
}
