package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
)

func boolPtr(v bool) *bool {
	return &v
}

func TestCacheFlagsSharedCacheConfig(t *testing.T) {
	tests := []struct {
		name  string
		flags CacheFlags
		want  struct {
			enabled bool
			module  bool
			build   bool
		}
	}{
		{
			name: "enabled turns on both caches",
			flags: CacheFlags{
				Go: GoCacheFlags{Enabled: boolPtr(true)},
			},
			want: struct {
				enabled bool
				module  bool
				build   bool
			}{enabled: true, module: true, build: true},
		},
		{
			name: "explicit module false overrides inherited enable",
			flags: CacheFlags{
				Go: GoCacheFlags{Enabled: boolPtr(true), ModuleCache: boolPtr(false)},
			},
			want: struct {
				enabled bool
				module  bool
				build   bool
			}{enabled: true, module: false, build: true},
		},
		{
			name: "explicit disable clears both caches",
			flags: CacheFlags{
				Go: GoCacheFlags{Enabled: boolPtr(false)},
			},
			want: struct {
				enabled bool
				module  bool
				build   bool
			}{enabled: false, module: false, build: false},
		},
		{
			name: "single cache enables overall config",
			flags: CacheFlags{
				Go: GoCacheFlags{BuildCache: boolPtr(true)},
			},
			want: struct {
				enabled bool
				module  bool
				build   bool
			}{enabled: true, module: false, build: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.flags.SharedCacheConfig()
			if got.Go.Enabled != tt.want.enabled || got.Go.ModuleCache != tt.want.module || got.Go.BuildCache != tt.want.build {
				t.Fatalf("got %+v, want enabled=%v module=%v build=%v", got.Go, tt.want.enabled, tt.want.module, tt.want.build)
			}
		})
	}
}

func TestCacheFlagsLoadedByKongYAML(t *testing.T) {
	type cli struct {
		Caches CacheFlags `embed:"" prefix:"caches-"`
	}

	projDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := os.WriteFile(filepath.Join(homeDir, ".sand.yaml"), []byte("caches:\n  go:\n    enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, ".sand.yaml"), []byte("caches:\n  go:\n    module-cache: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var parsed cli
	parser := kong.Must(&parsed,
		kong.Configuration(kongyaml.Loader, filepath.Join(homeDir, ".sand.yaml"), filepath.Join(projDir, ".sand.yaml")),
	)
	_, err := parser.Parse([]string{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := parsed.Caches.SharedCacheConfig()
	if !got.Go.Enabled || got.Go.ModuleCache || !got.Go.BuildCache {
		t.Fatalf("got %+v, want enabled=true module=false build=true", got.Go)
	}
}
