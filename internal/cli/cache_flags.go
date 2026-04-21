package cli

import "github.com/banksean/sand/internal/sandtypes"

// CacheFlags defines global shared-cache configuration that can be loaded by Kong
// from ~/.sand.yaml and project .sand.yaml without introducing a "caches" subcommand.
type CacheFlags struct {
	Mise *bool `name:"mise" help:"enable mise"`
}

func (c CacheFlags) SharedCacheConfig() sandtypes.SharedCacheConfig {
	var cfg sandtypes.SharedCacheConfig

	if c.Mise != nil {
		cfg.Mise = *c.Mise
	}

	return cfg
}
