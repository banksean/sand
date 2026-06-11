package applecontainer

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseContainerSystemConfig(t *testing.T) {
	cfgSrc := `
[build]
cpus = 2
image = "ghcr.io/apple/container-builder-shim/builder:0.12.0"
memory = "2048mb"
rosetta = true

[container]
cpus = 4
memory = "1gb"

[dns]
domain = "dev.local"

[kernel]
binaryPath = "opt/kata/share/kata-containers/vmlinux-6.18.15-186"
url = "https://github.com/kata-containers/kata-containers/releases/download/3.28.0/kata-static-3.28.0-arm64.tar.zst"

[machine]
cpus = 7
homeMount = "rw"
memory = "18gb"

[network]

[registry]
domain = "docker.io"

[vminit]
image = "ghcr.io/apple/containerization/vminit:0.33.3"`

	got, err := ParseContainerSystemConfig([]byte(cfgSrc))
	if err != nil {
		t.Errorf("failed to parse config toml: %v", err)
	}
	if got == nil {
		t.Errorf("cfg should not be nil")
	}
	want := &ContainerSystemConfig{
		BuildConfig: BuildConfig{
			CPUs:    2,
			Image:   "ghcr.io/apple/container-builder-shim/builder:0.12.0",
			Memory:  "2048mb",
			Rosetta: true,
		},
		ContainerConfig: ContainerConfig{
			CPUs:   4,
			Memory: "1gb",
		},
		DNSConfig: DNSConfig{
			Domain: "dev.local",
		},
		KernelConfig: KernelConfig{
			BinaryPath: "opt/kata/share/kata-containers/vmlinux-6.18.15-186",
			URL:        "https://github.com/kata-containers/kata-containers/releases/download/3.28.0/kata-static-3.28.0-arm64.tar.zst",
		},
		MachineConfig: MachineConfig{
			CPUs:      7,
			HomeMount: "rw",
			Memory:    "18gb",
		},
		NetworkConfig: NetworkConfig{},
		RegistryConfig: RegistryConfig{
			Domain: "docker.io",
		},
		VminitConfig: VminitConfig{
			Image: "ghcr.io/apple/containerization/vminit:0.33.3",
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("got != want, diff: \n%s\n", diff)
	}
}
