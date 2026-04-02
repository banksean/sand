package runtimedeps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/banksean/sand/applecontainer"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

const (
	AppleContainerVersion      = "0.11.0"
	MinimumMacOSVersion        = 26
	CustomKernelReleaseVersion = "v0.0.1"
	CustomKernelHash           = "fce4baecf9f814d0dc17e55c185f25b49bd462b61c81fb8520e306990b0c65c1"
	CustomInitImage            = "ghcr.io/banksean/sand/custom-init:latest"
)

type diagnosticCheck struct {
	ID          PrerequID
	Description string
	Run         func(context.Context, string) error
	// TODO: Severity, AffectedFeatures, Remedy
}

type PrerequID string

const (
	GitRemoteIsSSH           PrerequID = "git-ssh-checkout"
	GitDir                   PrerequID = "git-dir"
	ContainerSystemDNSDomain PrerequID = "container-dns-domain-set"
	ContainerSystemDNSName   PrerequID = "container-dns-name"
	ContainerCommand         PrerequID = "container-runtime"
	MacOSVersion             PrerequID = "macos-version"
	MacOS                    PrerequID = "macos"
	CustomInitImagePulled    PrerequID = "custom-init-image-pulled"
	CustomKernelInstalled    PrerequID = "custom-kernel-installed"
)

var (
	diagnosticChecks = []diagnosticCheck{
		{
			ID:          MacOS,
			Description: "Running on MacOS",
			Run: func(ctx context.Context, appBaseDir string) error {
				if runtime.GOOS != "darwin" {
					return fmt.Errorf("this program requires macOS %d or greater, but detected OS: %s", MinimumMacOSVersion, runtime.GOOS)
				}
				return nil
			},
		},
		{
			ID:          MacOSVersion,
			Description: fmt.Sprintf("Running MacOS version %d or greater", MinimumMacOSVersion),
			Run: func(ctx context.Context, appBaseDir string) error {
				majorVersion, err := getMacOSMajorVersion(ctx)
				if err != nil {
					return fmt.Errorf("failed to get macOS version: %w", err)
				}
				if majorVersion < MinimumMacOSVersion {
					return fmt.Errorf("MacOS version %d detected, but version %d or greater is required", majorVersion, MinimumMacOSVersion)
				}
				return nil
			},
		},
		{
			ID:          ContainerCommand,
			Description: fmt.Sprintf("Have https://github.com/apple/container runtime installed at version %s", AppleContainerVersion),
			Run: func(ctx context.Context, appBaseDir string) error {
				version, err := applecontainer.System.Version(ctx)
				if err != nil {
					return fmt.Errorf("could not locate Apple's `container` command from the releases published at https://github.com/apple/container/releases/tag/%s", AppleContainerVersion)
				}
				slog.InfoContext(ctx, "verifyPrerequisites", "version", version)
				if !strings.Contains("container CLI version "+version, AppleContainerVersion) {
					return fmt.Errorf("expected container command version %q, but got %q", AppleContainerVersion, version)
				}
				return nil
			},
		},
		{
			ID:          ContainerSystemDNSName,
			Description: "Container system has at least one dns name configured",
			Run: func(ctx context.Context, appBaseDir string) error {
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
			ID:          ContainerSystemDNSDomain,
			Description: "Container system has dns.domain property set",
			Run: func(ctx context.Context, appBaseDir string) error {
				domain, err := applecontainer.System.PropertyGet(ctx, "dns.domain")
				if err != nil {
					return fmt.Errorf("could not get container system properties: %w", err)
				}
				if domain == "" {
					return fmt.Errorf("container system property dns.domain is not set")
				}

				return nil
			},
		},
		{
			ID:          GitDir,
			Description: "should be invoked from a git directory",
			Run: func(ctx context.Context, appBaseDir string) error {
				gitCmd := exec.Command("git", "rev-parse", "--show-toplevel")
				out, err := gitCmd.Output()
				if err != nil {
					return fmt.Errorf("%s: %s", err.Error(), string(out))
				}
				return nil
			},
		},
		{
			ID:          GitRemoteIsSSH,
			Description: "git checkout should be authenticated to origin with ssh",
			Run: func(ctx context.Context, appBaseDir string) error {
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
		{
			ID:          CustomInitImagePulled,
			Description: "custom init image must be pulled and available in local registry. run `sand install-ebpf-support` to install it",
			Run: func(ctx context.Context, appBaseDir string) error {
				inspectCmd := exec.Command("container", "image", "inspect", CustomInitImage)
				_, err := inspectCmd.Output()
				if err != nil {
					return err
				}
				return nil
			},
		},
		{
			ID:          CustomKernelInstalled,
			Description: "custom kernel binary must be installed. run `sand install-ebpf-support` to install it",
			Run: func(ctx context.Context, appBaseDir string) error {
				kernelFile := filepath.Join(appBaseDir, "kernel", CustomKernelReleaseVersion, "vmlinux")
				_, err := os.Stat(kernelFile)
				if err != nil {
					return err
				}
				return nil
			},
		},
	}
	diagnosticCheckMap = map[PrerequID]diagnosticCheck{}
)

func init() {
	for _, check := range diagnosticChecks {
		diagnosticCheckMap[check.ID] = check
	}
}

func Verify(ctx context.Context, appBaseDir string, checkIDs ...PrerequID) error {
	failures := map[PrerequID]string{}
	for _, checkID := range checkIDs {
		check, ok := diagnosticCheckMap[checkID]
		if !ok {
			failures[checkID] = "unrecognized prerequisite check ID"
			continue
		}
		if err := check.Run(ctx, appBaseDir); err != nil {
			failures[check.ID] = fmt.Sprintf("%s: %s", check.Description, err.Error())
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

func CheckImageExistsLocally(ctx context.Context, imageName string) (bool, error) {
	imgs, err := applecontainer.Images.Inspect(ctx, imageName)
	if err != nil {
		return false, err
	}
	if len(imgs) == 0 {
		return false, fmt.Errorf("not found in local registry: %s", imageName)
	}
	return true, nil
}

func CheckImageIsLatest(ctx context.Context, imageName string) (bool, error) {
	remoteDigest, err := crane.Digest(imageName, crane.WithAuth(authn.Anonymous))
	if err != nil {
		return false, err
	}
	imgs, err := applecontainer.Images.Inspect(ctx, imageName)
	if err != nil {
		return false, err
	}
	if len(imgs) == 0 {
		return false, fmt.Errorf("not found in local registry: %s", imageName)
	}
	img := imgs[0]
	slog.InfoContext(ctx, "checkLocalContainerRegistry", "localDigest", img.Index.Digest, "remoteDigest", remoteDigest)
	return remoteDigest == img.Index.Digest, nil
}
