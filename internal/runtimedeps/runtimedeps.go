package runtimedeps

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/banksean/sand/internal/applecontainer"
	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

const (
	AppleContainerVersion      = "0.12.0"
	MinimumMacOSVersion        = 26
	CustomKernelReleaseVersion = "v0.0.1"
	CustomKernelHash           = "fce4baecf9f814d0dc17e55c185f25b49bd462b61c81fb8520e306990b0c65c1"
	CustomInitImage            = "ghcr.io/banksean/sand/custom-init:latest"
)

type diagnosticCheck struct {
	ID          PrerequID
	Description string
	Run         func(context.Context, string, VerifyOptions) error
	// TODO: Severity, AffectedFeatures, Remedy
}

type PrerequID string

const (
	GitRemoteIsSSH           PrerequID = "git-ssh-checkout"
	GitDir                   PrerequID = "git-dir"
	ContainerSystemDNSDomain PrerequID = "container-dns-domain-set"
	ContainerSystemDNSName   PrerequID = "container-dns-name"
	ContainerCommand         PrerequID = "container-runtime"
	ContainerSystemRunning   PrerequID = "container-system-running"
	MacOSVersion             PrerequID = "macos-version"
	MacOS                    PrerequID = "macos"
	CustomInitImagePulled    PrerequID = "custom-init-image-pulled"
	CustomKernelInstalled    PrerequID = "custom-kernel-installed"
)

const DefaultDNSDomain = "dev.local"

var ErrContainerSystemNotRunning = errors.New("container system service is not running")

type containerSystem interface {
	Version(context.Context) (string, error)
	DNSList(context.Context) ([]string, error)
	PropertyGet(context.Context, string) (string, error)
	PropertySet(context.Context, string, string) error
	Status(ctx context.Context, opts *options.SystemStatus) (string, error)
	Start(ctx context.Context, opts *options.SystemStart) (string, error)
}

type VerifyOptions struct {
	Stdin            io.Reader
	Stdout           io.Writer
	PromptRemedies   bool
	DefaultDNSDomain string
}

var (
	systemOps = containerSystem(&applecontainer.System)

	diagnosticChecks = []diagnosticCheck{
		{
			ID:          MacOS,
			Description: "Running on MacOS",
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
				if runtime.GOOS != "darwin" {
					return fmt.Errorf("this program requires macOS %d or greater, but detected OS: %s", MinimumMacOSVersion, runtime.GOOS)
				}
				return nil
			},
		},
		{
			ID:          MacOSVersion,
			Description: fmt.Sprintf("Running MacOS version %d or greater", MinimumMacOSVersion),
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
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
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
				version, err := systemOps.Version(ctx)
				if err != nil {
					return fmt.Errorf("apple/container %s is not installed. Install it from %s", AppleContainerVersion, AppleContainerInstallerURL())
				}
				slog.InfoContext(ctx, "verifyPrerequisites", "version", version)
				if !strings.Contains("container CLI version "+version, AppleContainerVersion) {
					return fmt.Errorf("apple/container %s is required, but found %q. Install it from %s", AppleContainerVersion, version, AppleContainerInstallerURL())
				}
				return nil
			},
		},
		{
			ID:          ContainerSystemDNSName,
			Description: "Container system has at least one dns name configured",
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
				domains, err := systemOps.DNSList(ctx)
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
			ID:          ContainerSystemRunning,
			Description: "Container system service is running",
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
				status, err := systemOps.Status(ctx, nil)
				if err == nil {
					return nil
				}
				if status == "apiserver is not running and not registered with launchd" {
					if !opts.PromptRemedies {
						return fmt.Errorf("container system service is not running")
					}
					ok, err := PromptYesDefault(opts.Stdin, opts.Stdout, fmt.Sprintf("Start container system [Y/n]? "))
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("start container service by running `container system start`")
					}

					if msg, err := systemOps.Start(ctx, nil); err != nil {
						return fmt.Errorf("starting container service: %q %w", msg, err)
					}
				}
				return nil
			},
		},
		{
			ID:          ContainerSystemDNSDomain,
			Description: "Container system has dns.domain property set",
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
				domain, err := systemOps.PropertyGet(ctx, "dns.domain")
				if err != nil {
					return fmt.Errorf("could not get container system properties: %w", err)
				}
				if domain == "" {
					domain := opts.DefaultDNSDomain
					if domain == "" {
						domain = DefaultDNSDomain
					}
					command := fmt.Sprintf("container system property set dns.domain %s", domain)
					if !opts.PromptRemedies {
						return fmt.Errorf("container system property dns.domain is not set. Run: %s", command)
					}
					ok, err := PromptYesDefault(opts.Stdin, opts.Stdout, fmt.Sprintf("Set dns.domain to %s [Y/n]? ", domain))
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("container system property dns.domain is not set. Run: %s", command)
					}
					if err := systemOps.PropertySet(ctx, "dns.domain", domain); err != nil {
						return fmt.Errorf("setting container system property dns.domain: %w", err)
					}
				}

				return nil
			},
		},
		{
			ID:          GitDir,
			Description: "should be invoked from a git directory",
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
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
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
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
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
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
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
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
	return VerifyWithOptions(ctx, appBaseDir, VerifyOptions{}, checkIDs...)
}

func VerifyWithOptions(ctx context.Context, appBaseDir string, opts VerifyOptions, checkIDs ...PrerequID) error {
	for _, checkID := range checkIDs {
		check, ok := diagnosticCheckMap[checkID]
		if !ok {
			return fmt.Errorf("unrecognized prerequisite check ID %q", checkID)
		}
		if err := check.Run(ctx, appBaseDir, opts); err != nil {
			slog.ErrorContext(ctx, "diagnosticCheck failed", "name", check.Description, "error", err)
			return err
		} else {
			slog.InfoContext(ctx, "diagnosticCheck passed", "name", check.Description)
		}
	}
	return nil
}

func AppleContainerInstallerURL() string {
	return fmt.Sprintf(
		"https://github.com/apple/container/releases/download/%s/container-%s-installer-signed.pkg",
		AppleContainerVersion,
		AppleContainerVersion,
	)
}

func ContainerSystemStartCommand() string {
	return "container system start"
}

func ContainerSystemNotRunningError(err error) error {
	if err == nil {
		return fmt.Errorf("%w. Run: %s", ErrContainerSystemNotRunning, ContainerSystemStartCommand())
	}
	return fmt.Errorf("%w. Run: %s: %v", ErrContainerSystemNotRunning, ContainerSystemStartCommand(), err)
}

func IsContainerSystemNotRunningError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrContainerSystemNotRunning) || strings.Contains(err.Error(), ErrContainerSystemNotRunning.Error()) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "Ensure container system service has been started") ||
		strings.Contains(msg, "container system is not running") ||
		strings.Contains(msg, "container system service is not running") ||
		strings.Contains(msg, "container system service isn't running") ||
		strings.Contains(msg, "system service is not running") ||
		strings.Contains(msg, "system service isn't running")
}

func PromptYesDefault(stdin io.Reader, stdout io.Writer, prompt string) (bool, error) {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = io.Discard
	}
	fmt.Fprint(stdout, prompt)

	text, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("couldn't read from stdin: %w", err)
	}
	switch strings.TrimSpace(strings.ToLower(text)) {
	case "", "y", "yes":
		return true, nil
	default:
		return false, nil
	}
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

func CheckImageExistsLocally(ctx context.Context, imageName string) bool {
	imgs, err := applecontainer.Images.Inspect(ctx, imageName)
	if err != nil || len(imgs) == 0 {
		return false
	}
	return true
}

func CheckImageIsLatest(ctx context.Context, imageName string) (bool, error) {
	remoteDigest, err := crane.Digest(imageName, crane.WithAuth(authn.Anonymous))
	if err != nil {
		return false, fmt.Errorf("failed to get digest for %s from remote registry: %w", imageName, err)
	}
	imgs, err := applecontainer.Images.Inspect(ctx, imageName)
	if err != nil {
		return false, fmt.Errorf("failed to inspect local registry for %s: %w", imageName, err)
	}
	if len(imgs) == 0 {
		return false, fmt.Errorf("not found in local registry: %s", imageName)
	}
	img := imgs[0]
	slog.InfoContext(ctx, "checkLocalContainerRegistry", "localDigest", img.Index.Digest, "remoteDigest", remoteDigest)
	return remoteDigest == img.Index.Digest, nil
}
