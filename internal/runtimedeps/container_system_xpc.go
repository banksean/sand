package runtimedeps

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/banksean/sand/internal/applecontainer"
	"github.com/banksean/sand/internal/applecontainer/xpc"
)

const (
	containerAppRootEnv     = "CONTAINER_APP_ROOT"
	containerInstallRootEnv = "CONTAINER_INSTALL_ROOT"
)

type xpcContainerSystem struct{}

func (xpcContainerSystem) Version(ctx context.Context) (string, error) {
	health, err := pingContainerSystem(ctx)
	if err != nil {
		return "", err
	}
	return health.APIServerVersion, nil
}

func (xpcContainerSystem) EnsureRunning(ctx context.Context) error {
	_, err := pingContainerSystem(ctx)
	return err
}

func (xpcContainerSystem) DNSList(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir("/etc/resolver")
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var domains []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "containerization.") {
			continue
		}
		domain, err := resolverDomain(filepath.Join("/etc/resolver", entry.Name()))
		if err != nil {
			return nil, err
		}
		if domain != "" {
			domains = append(domains, domain)
		}
	}
	sort.Strings(domains)
	return domains, nil
}

func (xpcContainerSystem) GetConfig(ctx context.Context) (*applecontainer.ContainerSystemConfig, error) {
	cfg := &applecontainer.ContainerSystemConfig{}
	var readAny bool
	var errs []error
	for _, path := range containerConfigPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
			continue
		}
		readAny = true
		parsed, err := applecontainer.ParseContainerSystemConfig(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		mergeContainerSystemConfig(cfg, parsed)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("read container system config: %w", errs[0])
	}
	if !readAny {
		return cfg, nil
	}
	return cfg, nil
}

func pingContainerSystem(ctx context.Context) (xpc.SystemHealth, error) {
	client, err := xpc.NewClient()
	if err != nil {
		return xpc.SystemHealth{}, err
	}
	defer client.Close()
	return client.Ping(ctx)
}

func resolverDomain(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "domain" {
			return strings.TrimSuffix(fields[1], "."), nil
		}
	}
	return "", scanner.Err()
}

func containerConfigPaths() []string {
	var paths []string
	if appRoot := os.Getenv(containerAppRootEnv); appRoot != "" {
		paths = append(paths, filepath.Join(appRoot, "config", "config.toml"))
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, "Library", "Application Support", "com.apple.container", "config", "config.toml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "container", "config.toml"))
	}
	if installRoot := os.Getenv(containerInstallRootEnv); installRoot != "" {
		paths = append(paths, filepath.Join(installRoot, "etc", "container", "config.toml"))
	} else {
		paths = append(paths, "/usr/local/etc/container/config.toml")
	}
	return paths
}

func mergeContainerSystemConfig(dst, src *applecontainer.ContainerSystemConfig) {
	if dst.DNSConfig.Domain == "" {
		dst.DNSConfig.Domain = src.DNSConfig.Domain
	}
	if dst.BuildConfig == (applecontainer.BuildConfig{}) {
		dst.BuildConfig = src.BuildConfig
	}
	if dst.ContainerConfig == (applecontainer.ContainerConfig{}) {
		dst.ContainerConfig = src.ContainerConfig
	}
	if dst.KernelConfig == (applecontainer.KernelConfig{}) {
		dst.KernelConfig = src.KernelConfig
	}
	if dst.MachineConfig == (applecontainer.MachineConfig{}) {
		dst.MachineConfig = src.MachineConfig
	}
	if dst.NetworkConfig == (applecontainer.NetworkConfig{}) {
		dst.NetworkConfig = src.NetworkConfig
	}
	if dst.RegistryConfig == (applecontainer.RegistryConfig{}) {
		dst.RegistryConfig = src.RegistryConfig
	}
	if dst.VminitConfig == (applecontainer.VminitConfig{}) {
		dst.VminitConfig = src.VminitConfig
	}
}
