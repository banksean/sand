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
			mise bool
		}
	}{
		{
			name: "mise can be enabled directly",
			flags: CacheFlags{
				Mise: boolPtr(true),
			},
			want: struct {
				mise bool
			}{mise: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.flags.SharedCacheConfig()
			if got.Mise != tt.want.mise {
				t.Fatalf("got mise=%v, want mise=%v", got.Mise, tt.want.mise)
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

	if err := os.WriteFile(filepath.Join(homeDir, ".sand.yaml"), []byte("caches:\n  mise: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, ".sand.yaml"), []byte("caches:\n mise: true\n"), 0o644); err != nil {
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
	if !got.Mise {
		t.Fatalf("got mise=%v, want mise=true", got.Mise)
	}
}
