package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	"gopkg.in/yaml.v3"

	profilecfg "github.com/banksean/sand/internal/profiles"
)

type ConfigCmd struct {
	Ls ConfigLsCmd `cmd:"" help:"show effective configuration with sources"`
	// TODOL: get, set, unset subcommands
}

type ConfigLsCmd struct{}

func (c *ConfigLsCmd) Run(k *kong.Kong, cctx *CLIContext) error {
	projCfg, userCfg, defaultsCfg, projCfgPath, userCfgPath, err := loadEffectiveConfigMaps(k)
	if err != nil {
		return err
	}
	var renderErr error
	walkMerge(nil, projCfg, userCfg, defaultsCfg, func(path []string, name string, projVal, userVal, defaultVal any) {
		if renderErr != nil {
			return
		}
		var val any
		source := ""
		if projVal != nil {
			val = projVal
			source = " # " + projCfgPath
		} else if userVal != nil {
			val = userVal
			source = " # " + userCfgPath
		} else if defaultVal != nil {
			val = defaultVal
		}
		renderErr = writeConfigEntry(os.Stdout, path, name, val, source)
	})
	return renderErr
}

func writeConfigEntry(w io.Writer, path []string, name string, val any, source string) error {
	prefix := strings.Repeat("  ", len(path)-1)
	if val == nil {
		_, err := fmt.Fprintf(w, "%s%s:\n", prefix, name)
		return err
	}
	if isYAMLBlockValue(val) {
		encoded, err := yaml.Marshal(val)
		if err != nil {
			return fmt.Errorf("marshal config value %s: %w", strings.Join(path, "."), err)
		}
		if _, err := fmt.Fprintf(w, "%s%s:%s\n", prefix, name, source); err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimRight(string(encoded), "\n"), "\n") {
			if _, err := fmt.Fprintf(w, "%s  %s\n", prefix, line); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := fmt.Fprintf(w, "%s%s: %v%s\n", prefix, name, val, source)
	return err
}

func isYAMLBlockValue(value any) bool {
	value = derefValue(value)
	if value == nil {
		return false
	}

	switch reflect.ValueOf(value).Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.Struct:
		return true
	default:
		return false
	}
}

// FindProjectConfig searches cwd and its ancestors for a .sand.yaml file,
// returning the first path found or "" if none exists.
func FindProjectConfig() string {
	return profilecfg.FindProjectConfig("")
}

// loadEffectiveConfigMaps loads the project-level and user-level (~/.sand.yaml)
// config files and returns their contents along with kong-derived defaults.
// projCfgPath and userCfgPath are the resolved paths, useful for source annotations.
// Any of the returned maps may be empty if the corresponding file does not exist.
func loadEffectiveConfigMaps(k *kong.Kong) (projCfg, userCfg, defaultsCfg map[string]any, projCfgPath, userCfgPath string, err error) {
	projCfg = map[string]any{}
	projCfgPath = FindProjectConfig()
	if projCfgPath != "" {
		if e := decodeYAML(projCfgPath, &projCfg); e != nil && !os.IsNotExist(e) && e != io.EOF {
			return nil, nil, nil, "", "", fmt.Errorf("loadEffectiveConfigMaps: %w", e)
		}
	}

	userCfg = map[string]any{}
	home, herr := os.UserHomeDir()
	if herr != nil {
		return nil, nil, nil, "", "", herr
	}
	userCfgPath = filepath.Join(home, ".sand.yaml")
	if e := decodeYAML(userCfgPath, &userCfg); e != nil && !os.IsNotExist(e) && e != io.EOF {
		return nil, nil, nil, "", "", e
	}

	defaultsCfg = normalizeConfigMap(encodeNodeDefaults(k.Model.Node))
	return projCfg, userCfg, defaultsCfg, projCfgPath, userCfgPath, nil
}

func LoadEffectiveProfileConfig() (profilecfg.Config, error) {
	return profilecfg.LoadConfigForDir("")
}

func effectiveConfigPaths() ([]string, error) {
	return profilecfg.ConfigPaths("")
}

func walkMerge(path []string, a, b, c map[string]any, f func(path []string, name string, aVal any, bVal any, cVal any)) {
	keys := []string{}
	for aKey := range a {
		keys = append(keys, aKey)
	}
	for bKey := range b {
		if _, ok := a[bKey]; ok {
			continue
		}
		keys = append(keys, bKey)
	}
	for cKey := range c {
		if _, ok := a[cKey]; ok {
			continue
		} else if _, ok := b[cKey]; ok {
			continue
		}
		keys = append(keys, cKey)
	}
	sort.Strings(keys)
	// Would use defer, but it gets called in the reverse order of what we want.
	q := []func(){}
	for _, key := range keys {
		av := a[key]
		bv := b[key]
		cv := c[key]
		newPath := append(path, key)
		am, amOK := av.(map[string]any)
		bm, bmOK := bv.(map[string]any)
		cm, cmOK := cv.(map[string]any)
		if !amOK && !bmOK && !cmOK {
			f(newPath, key, av, bv, cv)
		} else {
			newPath, am, bm, cm := newPath, am, bm, cm
			q = append(q, func() {
				f(newPath, key, nil, nil, nil)
				walkMerge(newPath, am, bm, cm, f)
			})
		}
	}
	for _, qf := range q {
		qf()
	}
}

func decodeYAML(filename string, value any) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return yaml.NewDecoder(f).Decode(value)
}

func encodeNodeDefaults(node *kong.Node) map[string]any {
	ret := map[string]any{}
	for _, flag := range node.Flags {
		if flag.Default != "" {
			ret[flag.Name] = flag.Default
		}
	}
	for _, child := range node.Children {
		if child.Type == kong.CommandNode {
			if detail := encodeNodeDefaults(child); len(detail) > 0 {
				ret[child.Name] = detail
			}
		}
	}
	return ret
}

func normalizeConfigMap(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}

	normalized := map[string]any{}
	for key, value := range cfg {
		switch {
		case key == "caches":
			if nested, ok := value.(map[string]any); ok {
				normalized[key] = normalizeConfigMap(nested)
			} else {
				normalized[key] = derefValue(value)
			}
		case strings.HasPrefix(key, "caches-"):
			cacheCfg, _ := normalized["caches"].(map[string]any)
			if cacheCfg == nil {
				cacheCfg = map[string]any{}
				normalized["caches"] = cacheCfg
			}
			cacheCfg[strings.TrimPrefix(key, "caches-")] = derefValue(value)
		default:
			if nested, ok := value.(map[string]any); ok {
				normalized[key] = normalizeConfigMap(nested)
			} else {
				normalized[key] = derefValue(value)
			}
		}
	}

	return normalized
}

func derefValue(value any) any {
	if value == nil {
		return nil
	}

	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	return v.Interface()
}
