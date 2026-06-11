package applecontainer

import "github.com/pelletier/go-toml/v2"

// ContainerSystemConfig is the go equivalent of
// https://github.com/apple/container/blob/main/Sources/ContainerPersistence/ContainerSystemConfig.swift
type ContainerSystemConfig struct {
	BuildConfig     BuildConfig     `toml:"build"`
	ContainerConfig ContainerConfig `toml:"container"`
	DNSConfig       DNSConfig       `toml:"dns"`
	KernelConfig    KernelConfig    `toml:"kernel"`
	MachineConfig   MachineConfig   `toml:"machine"`
	NetworkConfig   NetworkConfig   `toml:"network"`
	RegistryConfig  RegistryConfig  `toml:"registry"`
	VminitConfig    VminitConfig    `toml:"vminit"`
}

type BuildConfig struct {
	Rosetta bool   `toml:"rosetta"`
	CPUs    int    `toml:"cpus"`
	Memory  string `toml:"memory"`
	Image   string `toml:"image"`
}

type ContainerConfig struct {
	CPUs   int    `toml:"cpus"`
	Memory string `toml:"memory"`
}

type DNSConfig struct {
	Domain string `toml:"domain"`
}

type KernelConfig struct {
	BinaryPath string `toml:"binaryPath"`
	URL        string `toml:"url"`
}

type MachineConfig struct {
	CPUs      int    `toml:"cpus"`
	Memory    string `toml:"memory"`
	HomeMount string `toml:"homeMount"`
}

type NetworkConfig struct {
	Subnet   string `toml:"subnet"`
	SubnetV6 string `toml:"subnetv6"`
}

type RegistryConfig struct {
	Domain string `toml:"domain"`
}

type VminitConfig struct {
	Image string `toml:"image"`
}

func ParseContainerSystemConfig(byts []byte) (*ContainerSystemConfig, error) {
	var cfg ContainerSystemConfig
	err := toml.Unmarshal(byts, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
