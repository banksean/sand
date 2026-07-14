package cli

import "github.com/banksean/sand/internal/sandtypes"

// CacheFlags defines global shared-cache configuration that can be loaded by Kong
// from ~/.sand.yaml and project .sand.yaml without introducing a "caches" subcommand.
type CacheFlags struct {
	Mise      *bool `name:"mise" default:"true" help:"enable mise cache"`
	APK       *bool `name:"apk" default:"true" help:"enable apk cache"`
	Agents    *bool `name:"agents" default:"true" help:"enable agent installer cache"`
	Bazel     *bool `name:"bazel" default:"false" help:"enable Bazel remote build cache configuration"`
	HTTPProxy *bool `name:"http-proxy" default:"false" help:"enable shared HTTP proxy cache configuration"`
}

func (c CacheFlags) SharedCacheConfig() sandtypes.SharedCacheConfig {
	var cfg sandtypes.SharedCacheConfig

	if c.Mise != nil {
		cfg.Mise = *c.Mise
	}

	if c.APK != nil {
		cfg.APK = *c.APK
	}

	if c.Agents != nil {
		cfg.Agents = *c.Agents
	}

	if c.Bazel != nil {
		cfg.Bazel = *c.Bazel
	}

	if c.HTTPProxy != nil {
		cfg.HTTPProxy = *c.HTTPProxy
	}

	return cfg
}
