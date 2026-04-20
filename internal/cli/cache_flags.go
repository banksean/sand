package cli

import "github.com/banksean/sand/internal/sandtypes"

// CacheFlags defines global shared-cache configuration that can be loaded by Kong
// from ~/.sand.yaml and project .sand.yaml without introducing a "caches" subcommand.
type CacheFlags struct {
	Go   GoCacheFlags `embed:"" prefix:"go-"`
	Mise *bool        `name:"mise" help:"enable mise"`
}

type GoCacheFlags struct {
	Enabled     *bool `name:"enabled" hidden:"" help:"enable shared Go caches"`
	ModuleCache *bool `name:"module-cache" hidden:"" help:"enable the shared Go module cache"`
	BuildCache  *bool `name:"build-cache" hidden:"" help:"enable the shared Go build cache"`
}

func (c CacheFlags) SharedCacheConfig() sandtypes.SharedCacheConfig {
	var cfg sandtypes.SharedCacheConfig

	if c.Go.Enabled != nil {
		cfg.Go.Enabled = *c.Go.Enabled
		if *c.Go.Enabled {
			cfg.Go.ModuleCache = true
			cfg.Go.BuildCache = true
		}
	}
	if c.Go.ModuleCache != nil {
		cfg.Go.ModuleCache = *c.Go.ModuleCache
		if *c.Go.ModuleCache {
			cfg.Go.Enabled = true
		}
	}
	if c.Go.BuildCache != nil {
		cfg.Go.BuildCache = *c.Go.BuildCache
		if *c.Go.BuildCache {
			cfg.Go.Enabled = true
		}
	}
	if !cfg.Go.ModuleCache && !cfg.Go.BuildCache {
		cfg.Go.Enabled = false
	}

	if c.Mise != nil {
		cfg.Mise = *c.Mise
	}

	return cfg
}
