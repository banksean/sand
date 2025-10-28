package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/banksean/sand/applecontainer"
	"github.com/banksean/sand/applecontainer/types"
)

const (
	appleContainerVersion = "0.5.0"
	minimumMacOSVersion   = 26
)

type diagnosticCheck struct {
	ID          string
	Description string
	Run         func(context.Context) error
	// TODO: Severity, AffectedFeatures, Remedy
}

var (
	diagnosticChecks = []diagnosticCheck{
		{
			ID:          "macos",
			Description: "Running on MacOS",
			Run: func(ctx context.Context) error {
				if runtime.GOOS != "darwin" {
					return fmt.Errorf("this program requires macOS %d or greater, but detected OS: %s", minimumMacOSVersion, runtime.GOOS)
				}
				return nil
			},
		},
		{
			ID:          "macos-version",
			Description: fmt.Sprintf("Running MacOS version %d or greater", minimumMacOSVersion),
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
			ID:          "container-runtime",
			Description: "Have https://github.com/apple/container runtime installed at the right version",
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
			ID:          "container-dns-name",
			Description: "Container system has at least one dns name configured",
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
			ID:          "container-dns-domain-set",
			Description: "Container system has dns.domain property set",
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
		{
			ID:          "git-dir",
			Description: "should be invoked from a git directory",
			Run: func(ctx context.Context) error {
				gitCmd := exec.Command("git", "rev-parse", "--show-toplevel")
				out, err := gitCmd.Output()
				if err != nil {
					return fmt.Errorf("%s: %s", err.Error(), string(out))
				}
				return nil
			},
		},
		{
			ID:          "git-ssh-checkout",
			Description: "git checkout should be authenticated to origin with ssh",
			Run: func(ctx context.Context) error {
				gitCmd := exec.Command("git", "remote", "get-url", "origin")
				out, err := gitCmd.Output()
				if err != nil {
					return err
				}
				origin := strings.TrimSpace(string(out))
				isSSH := strings.HasPrefix(origin, "git@") || strings.HasPrefix(origin, "ssh://")

				if !isSSH {
					return fmt.Errorf("git origin %q does not appear to be authenticated with ssh", origin)
				}
				return nil
			},
		},
	}
	diagnosticCheckMap = map[string]diagnosticCheck{}
)

func init() {
	for _, check := range diagnosticChecks {
		diagnosticCheckMap[check.ID] = check
	}
}

func verifyPrerequisites(ctx context.Context, checkIDs ...string) error {
	failures := map[string]string{}
	for _, checkID := range checkIDs {
		check, ok := diagnosticCheckMap[checkID]
		if !ok {
			failures[checkID] = "unrecognized prerequisite check ID"
			continue
		}
		if err := check.Run(ctx); err != nil {
			failures[check.ID] = check.Description
			slog.ErrorContext(ctx, "diagnosticCheck failed", "name", check.Description, "error", err)
		} else {
			slog.InfoContext(ctx, "diagnosticCheck passed", "name", check.Description)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	errs := []error{}
	slog.ErrorContext(ctx, "prerequisite check(s) failed", "failures", failures)
	for id, description := range failures {
		errs = append(errs, fmt.Errorf("check failed %q: %s", id, description))
	}
	return errors.Join(errs...)
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
