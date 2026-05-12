package profiles

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/banksean/sand/internal/sandtypes"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Profiles map[string]sandtypes.Profile `yaml:"profiles"`
}

func LoadConfig(paths ...string) (Config, error) {
	var merged Config
	for _, path := range paths {
		if path == "" {
			continue
		}
		cfg, err := loadConfigFile(path)
		if err != nil {
			return Config{}, err
		}
		merged = MergeConfigs(merged, cfg)
	}
	return merged, nil
}

func MergeConfigs(base, override Config) Config {
	merged := Config{Profiles: map[string]sandtypes.Profile{}}
	for name, profile := range base.Profiles {
		merged.Profiles[name] = namedProfile(name, profile)
	}
	for name, profile := range override.Profiles {
		merged.Profiles[name] = namedProfile(name, profile)
	}
	if len(merged.Profiles) == 0 {
		merged.Profiles = nil
	}
	return merged
}

func loadConfigFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("loading profile config %q: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("loading profile config %q: %w", path, err)
	}
	for name, profile := range cfg.Profiles {
		cfg.Profiles[name] = namedProfile(name, profile)
	}
	return cfg, nil
}

func namedProfile(name string, profile sandtypes.Profile) sandtypes.Profile {
	profile.Name = name
	return profile
}
