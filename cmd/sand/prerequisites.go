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
	"github.com/banksean/apple-container/types"
)

const (
	appleContainerVersion = "0.5.0"
	minimumMacOSVersion   = 26
)

type diagnosticCheck struct {
	Name string
	Run  func(context.Context) error
}

var diagnosticChecks = []diagnosticCheck{
	{
		Name: "Running on MacOS",
		Run: func(ctx context.Context) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("this program requires macOS %d or greater, but detected OS: %s", minimumMacOSVersion, runtime.GOOS)
			}
			return nil
		},
	},
	{
		Name: fmt.Sprintf("Running MacOS version %d or greater", minimumMacOSVersion),
		Run: func(ctx context.Context) error {
			majorVersion, err := getMacOSMajorVersion(ctx)
			if err != nil {
				return fmt.Errorf("failed to get macOS version: %w", err)
			}
			if majorVersion < minimumMacOSVersion {
				return fmt.Errorf("MacOS version %d detected, but version %d or greater is required", majorVersion, minimumMacOSVersion)
			}
			return nil
		},
	},
	{
		Name: "Have https://github.com/apple/container runtime installed at the right version",
		Run: func(ctx context.Context) error {
			version, err := applecontainer.System.Version(ctx)
			if err != nil {
				return fmt.Errorf("could not locate Apple's `container` command from the releases published at https://github.com/apple/container/releases/tag/%s", appleContainerVersion)
			}
			slog.InfoContext(ctx, "verifyPrerequisites", "version", version)
			if !strings.Contains("container CLI version "+version, appleContainerVersion) {
				return fmt.Errorf("expected container command version %q, but got %q", appleContainerVersion, version)
			}
			return nil
		},
	},
	{
		Name: "Container system has at least one dns name configured",
		Run: func(ctx context.Context) error {
			domains, err := applecontainer.System.DNSList(ctx)
			if err != nil {
				return fmt.Errorf("could not get container system dns domain list: %w", err)
			}
			if len(domains) == 0 {
				return fmt.Errorf("no container system dns domains exist. vsc and ssh will not work without at least one dns domain")
			}
			slog.InfoContext(ctx, "configured DNS domains", "domains", domains)
			return nil
		},
	},
	{
		Name: "Container system has dns.domain property set",
		Run: func(ctx context.Context) error {
			systemProps, err := applecontainer.System.PropertyList(ctx)
			if err != nil {
				return fmt.Errorf("could not get container system properties: %w", err)
			}
			if len(systemProps) == 0 {
				return fmt.Errorf("no container system properties")
			}

			propMap := map[string]types.SystemProperty{}
			for _, p := range systemProps {
				propMap[p.ID] = p
			}

			if p, ok := propMap["dns.domain"]; !ok || p.Value == nil {
				return fmt.Errorf("missing system property 'dns.domain'")
			}
			return nil
		},
	},
}

func verifyPrerequisites(ctx context.Context) map[string]string {
	failures := map[string]string{}
	for _, check := range diagnosticChecks {
		if err := check.Run(ctx); err != nil {
			failures[check.Name] = err.Error()
			slog.ErrorContext(ctx, "diagnosticCheck failed", "name", check.Name, "error", err)
		} else {
			slog.InfoContext(ctx, "diagnosticCheck passed", "name", check.Name)
		}
	}

	return failures
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
