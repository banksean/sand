package profiles

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

func LoadConfigForDir(projectDir string) (Config, error) {
	paths, err := ConfigPaths(projectDir)
	if err != nil {
		return Config{}, err
	}
	return LoadConfig(paths...)
}

func ConfigPaths(projectDir string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	paths := []string{filepath.Join(home, ".sand.yaml")}
	if projectPath := FindProjectConfig(projectDir); projectPath != "" {
		paths = append(paths, projectPath)
	}
	return paths, nil
}

func FindProjectConfig(startDir string) string {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, ".sand.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
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
