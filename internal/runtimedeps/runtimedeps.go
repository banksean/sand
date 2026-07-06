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
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/hostops"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

const (
	AppleContainerVersion      = "1.0.0"
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
	GitRemoteIsSSH                 PrerequID = "git-ssh-checkout"
	GitDir                         PrerequID = "git-dir"
	ContainerSystemDNSDomain       PrerequID = "container-dns-domain-set"
	ContainerSystemDNSRegistration PrerequID = "container-dns-domain-registered"
	ContainerSystemDNSName         PrerequID = "container-dns-name"
	ContainerCommand               PrerequID = "container-runtime"
	ContainerSystemRunning         PrerequID = "container-system-running"
	MacOSVersion                   PrerequID = "macos-version"
	MacOS                          PrerequID = "macos"
	CustomInitImagePulled          PrerequID = "custom-init-image-pulled"
	CustomKernelInstalled          PrerequID = "custom-kernel-installed"
)

const DefaultDNSDomain = "dev.local"

var ErrContainerSystemNotRunning = errors.New("container system service is not running")

type containerSystem interface {
	Version(context.Context) (string, error)
	EnsureRunning(context.Context) error
	DNSList(context.Context) ([]string, error)
	GetConfig(ctx context.Context) (*applecontainer.ContainerSystemConfig, error)
}

type imageInspector interface {
	Inspect(ctx context.Context, name string) ([]*types.ImageManifest, error)
}

type VerifyOptions struct {
	Stdin            io.Reader
	Stdout           io.Writer
	PromptRemedies   bool
	DefaultDNSDomain string
}

var (
	systemOps = containerSystem(xpcContainerSystem{})
	imageOps  imageInspector

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
					return fmt.Errorf("could not get apple/container API server version: %w", err)
				}
				slog.InfoContext(ctx, "verifyPrerequisites", "version", version)
				if !strings.Contains(version, AppleContainerVersion) {
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
				if err := systemOps.EnsureRunning(ctx); err == nil {
					return nil
				} else {
					if !opts.PromptRemedies {
						return ContainerSystemNotRunningError(err)
					}
					return ContainerSystemNotRunningError(err)
				}
			},
		},
		{
			ID:          ContainerSystemDNSDomain,
			Description: "Container system has dns.domain property set",
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
				_, err := ensureDNSDomain(ctx, opts)
				return err
			},
		},
		{
			ID:          ContainerSystemDNSRegistration,
			Description: "Container system dns list includes dns.domain",
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
				_, err := EffectiveDNSDomain(ctx, opts)
				return err
			},
		},
		{
			ID:          GitDir,
			Description: "should be invoked from a git directory",
			Run: func(ctx context.Context, appBaseDir string, opts VerifyOptions) error {
				gitCmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
				out, err := gitCmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("%s: %s", err.Error(), strings.TrimSpace(string(out)))
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
				imgs, err := inspectLocalImage(ctx, CustomInitImage)
				if err != nil {
					return err
				}
				if len(imgs) == 0 {
					return fmt.Errorf("not found in local registry: %s", CustomInitImage)
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

// EffectiveDNSDomain returns the dns.domain value after validating that the
// container DNS subsystem has registered the same domain.
func EffectiveDNSDomain(ctx context.Context, opts VerifyOptions) (string, error) {
	domain, err := ensureDNSDomain(ctx, opts)
	if err != nil {
		return "", err
	}
	if err := ensureDNSDomainRegistered(ctx, domain); err != nil {
		return "", err
	}
	return domain, nil
}

func ensureDNSDomain(ctx context.Context, opts VerifyOptions) (string, error) {
	cfg, err := systemOps.GetConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("could not get container system properties: %w", err)
	}
	if cfg.DNSConfig.Domain != "" {
		return cfg.DNSConfig.Domain, nil
	}
	// TODO: offer to set it for them, with
	// PromptYesDefault(opts.Stdin, opts.Stdout, fmt.Sprintf("Set dns.domain to %s [Y/n]? ", domain))
	return "", fmt.Errorf("dns.domain not set in config - edit your ~/.config/container/config.toml to set it")
}

func ensureDNSDomainRegistered(ctx context.Context, domain string) error {
	domains, err := systemOps.DNSList(ctx)
	if err != nil {
		return fmt.Errorf("could not get container system dns domain list: %w", err)
	}
	for _, registered := range domains {
		if registered == domain {
			slog.InfoContext(ctx, "container dns domain is registered", "domain", domain)
			return nil
		}
	}
	return fmt.Errorf("container system dns list does not include %q. Run: sudo container system dns create %s", domain, domain)
}

func VerifyWithOptions(ctx context.Context, appBaseDir string, opts VerifyOptions, checkIDs ...PrerequID) error {
	for _, checkID := range checkIDs {
		check, ok := diagnosticCheckMap[checkID]
		if !ok {
			return fmt.Errorf("unrecognized prerequisite check ID %q", checkID)
		}
		if err := check.Run(ctx, appBaseDir, opts); err != nil {
			slog.ErrorContext(ctx, "diagnosticCheck failed", "name", check.Description, "error", err)
			return fmt.Errorf("%s: %w", check.Description, err)
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
	imgs, err := inspectLocalImage(ctx, imageName)
	if err != nil || len(imgs) == 0 {
		return false
	}
	return true
}

func CheckImageIsLatest(ctx context.Context, imageName string) (bool, error) {
	imgs, err := inspectLocalImage(ctx, imageName)
	if err != nil {
		return false, fmt.Errorf("failed to inspect local registry for %s: %w", imageName, err)
	}
	if len(imgs) == 0 {
		return false, fmt.Errorf("not found in local registry: %s", imageName)
	}
	return CheckImageDigestIsLatest(ctx, imageName, imgs[0].Index.Digest)
}

func inspectLocalImage(ctx context.Context, imageName string) ([]*types.ImageManifest, error) {
	ops, err := localImageOps()
	if err != nil {
		return nil, err
	}
	return ops.Inspect(ctx, imageName)
}

func localImageOps() (imageInspector, error) {
	if imageOps != nil {
		return imageOps, nil
	}
	return hostops.NewAppleImageOps()
}

func CheckImageDigestIsLatest(ctx context.Context, imageName, localDigest string) (bool, error) {
	remoteDigest, err := crane.Digest(imageName, crane.WithAuth(authn.Anonymous))
	if err != nil {
		return false, fmt.Errorf("failed to get digest for %s from remote registry: %w", imageName, err)
	}
	slog.InfoContext(ctx, "checkLocalContainerRegistry", "localDigest", localDigest, "remoteDigest", remoteDigest)
	return remoteDigest == localDigest, nil
}
