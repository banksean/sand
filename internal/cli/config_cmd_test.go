package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
)

// --- walkMerge ---

func TestWalkMerge_AllSourcesDelivered(t *testing.T) {
	// Verify that all three values are passed to the callback correctly so the
	// caller can apply its own priority logic.
	proj := map[string]any{"a": "proj"}
	user := map[string]any{"b": "user"}
	defs := map[string]any{"c": "default"}

	type entry struct{ a, b, c any }
	got := map[string]entry{}
	walkMerge(nil, proj, user, defs, func(path []string, name string, aVal, bVal, cVal any) {
		got[name] = entry{aVal, bVal, cVal}
	})

	if got["a"].a != "proj" || got["a"].b != nil || got["a"].c != nil {
		t.Errorf("key 'a': expected proj='proj', user=nil, default=nil; got %+v", got["a"])
	}
	if got["b"].a != nil || got["b"].b != "user" || got["b"].c != nil {
		t.Errorf("key 'b': expected proj=nil, user='user', default=nil; got %+v", got["b"])
	}
	if got["c"].a != nil || got["c"].b != nil || got["c"].c != "default" {
		t.Errorf("key 'c': expected proj=nil, user=nil, default='default'; got %+v", got["c"])
	}
}

func TestWalkMerge_OverlappingKeys(t *testing.T) {
	// When the same key exists in multiple maps, all three values arrive together.
	proj := map[string]any{"x": "proj-x"}
	user := map[string]any{"x": "user-x"}
	defs := map[string]any{"x": "def-x"}

	type entry struct{ a, b, c any }
	var got entry
	walkMerge(nil, proj, user, defs, func(path []string, name string, aVal, bVal, cVal any) {
		if name == "x" {
			got = entry{aVal, bVal, cVal}
		}
	})

	if got.a != "proj-x" || got.b != "user-x" || got.c != "def-x" {
		t.Errorf("expected all three values for 'x', got %+v", got)
	}
}

func TestWalkMerge_SortedOrder(t *testing.T) {
	proj := map[string]any{"z": 1, "a": 2, "m": 3}
	var names []string
	walkMerge(nil, proj, nil, nil, func(path []string, name string, aVal, bVal, cVal any) {
		names = append(names, name)
	})

	want := []string{"a", "m", "z"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("position %d: want %q, got %q", i, want[i], n)
		}
	}
}

func TestWalkMerge_NestedMaps(t *testing.T) {
	// Parent map nodes should be visited with nil values; leaf nodes carry values.
	proj := map[string]any{
		"parent": map[string]any{"child": "leaf"},
	}

	type visit struct {
		path    []string
		name    string
		allNil  bool
		leafVal any
	}
	var visits []visit
	walkMerge(nil, proj, nil, nil, func(path []string, name string, aVal, bVal, cVal any) {
		visits = append(visits, visit{
			path:    append([]string{}, path...),
			name:    name,
			allNil:  aVal == nil && bVal == nil && cVal == nil,
			leafVal: aVal,
		})
	})

	// Expect two visits: parent (nil vals) then child (leaf val)
	if len(visits) != 2 {
		t.Fatalf("expected 2 visits, got %d: %+v", len(visits), visits)
	}
	if visits[0].name != "parent" || !visits[0].allNil {
		t.Errorf("first visit should be parent with nil vals, got %+v", visits[0])
	}
	if visits[1].name != "child" || visits[1].leafVal != "leaf" {
		t.Errorf("second visit should be child='leaf', got %+v", visits[1])
	}
	if len(visits[1].path) != 2 || visits[1].path[0] != "parent" {
		t.Errorf("child path should be [parent child], got %v", visits[1].path)
	}
}

func TestWalkMerge_EmptyMaps(t *testing.T) {
	count := 0
	walkMerge(nil, nil, nil, nil, func(_ []string, _ string, _, _, _ any) {
		count++
	})
	if count != 0 {
		t.Errorf("expected no visits for empty maps, got %d", count)
	}
}

// --- loadEffectiveConfigMaps ---

// newKong returns a minimal *kong.Kong for testing loadEffectiveConfigMaps.
func newKong(t *testing.T) *kong.Kong {
	t.Helper()
	var target struct {
		Image string `default:"default-image"`
	}
	return kong.Must(&target)
}

func TestLoadEffectiveConfigMaps_NoFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Chdir(tmp)

	k := newKong(t)
	proj, user, defs, userCfgPath, err := loadEffectiveConfigMaps(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proj) != 0 {
		t.Errorf("expected empty projCfg, got %v", proj)
	}
	if len(user) != 0 {
		t.Errorf("expected empty userCfg, got %v", user)
	}
	if len(defs) == 0 {
		t.Error("expected non-empty defaultsCfg from kong")
	}
	wantPath := filepath.Join(tmp, ".sand.yaml")
	if userCfgPath != wantPath {
		t.Errorf("userCfgPath: want %q, got %q", wantPath, userCfgPath)
	}
}

func TestLoadEffectiveConfigMaps_ProjFile(t *testing.T) {
	projDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Chdir(projDir)

	if err := os.WriteFile(filepath.Join(projDir, ".sand.yaml"), []byte("image: proj-image\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	k := newKong(t)
	proj, user, _, _, err := loadEffectiveConfigMaps(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proj["image"] != "proj-image" {
		t.Errorf("projCfg[image]: want 'proj-image', got %v", proj["image"])
	}
	if len(user) != 0 {
		t.Errorf("expected empty userCfg, got %v", user)
	}
}

func TestLoadEffectiveConfigMaps_UserFile(t *testing.T) {
	projDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Chdir(projDir)

	if err := os.WriteFile(filepath.Join(homeDir, ".sand.yaml"), []byte("image: user-image\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	k := newKong(t)
	proj, user, _, _, err := loadEffectiveConfigMaps(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proj) != 0 {
		t.Errorf("expected empty projCfg, got %v", proj)
	}
	if user["image"] != "user-image" {
		t.Errorf("userCfg[image]: want 'user-image', got %v", user["image"])
	}
}

func TestLoadEffectiveConfigMaps_BothFiles(t *testing.T) {
	tmp := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Chdir(tmp)

	if err := os.WriteFile(filepath.Join(tmp, ".sand.yaml"), []byte("cpu: 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".sand.yaml"), []byte("memory: 4096\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	k := newKong(t)
	proj, user, _, _, err := loadEffectiveConfigMaps(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proj["cpu"] != 8 {
		t.Errorf("projCfg[cpu]: want 8, got %v", proj["cpu"])
	}
	if user["memory"] != 4096 {
		t.Errorf("userCfg[memory]: want 4096, got %v", user["memory"])
	}
}
