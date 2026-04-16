package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	"gopkg.in/yaml.v3"
)

type ConfigCmd struct {
	Ls ConfigLsCmd `cmd:"" help:"show effective configuration with sources"`
	// TODOL: get, set, unset subcommands
}

type ConfigLsCmd struct{}

func (c *ConfigLsCmd) Run(k *kong.Kong, cctx *CLIContext) error {
	projCfg := map[string]any{}
	err := decodeYAML("./.sand.yaml", &projCfg)
	if !os.IsNotExist(err) && err != io.EOF && err != nil {
		return err
	}

	userCfg := map[string]any{}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	userCfgFilename := filepath.Join(home, ".sand.yaml")
	err = decodeYAML(userCfgFilename, &userCfg)
	if !os.IsNotExist(err) && err != io.EOF && err != nil {
		return err
	}
	defaultsCfg := encodeNodeDetail(k.Model.Node)
	walkMerge(nil, projCfg, userCfg, defaultsCfg, func(path []string, name string, projVal, userVal, defaultVal any) {
		var val any
		source := ""
		if projVal != nil {
			val = projVal
			source = " # ./.sand.yaml"
		} else if userVal != nil {
			val = userVal
			source = " # " + userCfgFilename
		} else if defaultVal != nil {
			val = defaultVal
		}
		prefix := strings.Repeat("  ", len(path)-1)
		if val != nil {
			fmt.Printf(prefix+"%s: %v%s\n", name, val, source)
		} else {
			fmt.Printf(prefix+"%s:\n", name)
		}
	})
	return nil
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
		fmt.Printf("err: %v\n", err)
		return err
	}
	defer f.Close()
	return yaml.NewDecoder(f).Decode(value)
}

func encodeNodeDetail(node *kong.Node) map[string]any {
	ret := map[string]any{}
	for _, flag := range node.Flags {
		if flag.Value.Set {
			ret[flag.Name] = flag.Value.Target.Interface()
		} else if flag.Default != "" {
			ret[flag.Name] = flag.Default
		}
	}
	for _, child := range node.Children {
		if child.Type == kong.CommandNode {
			if detail := encodeNodeDetail(child); len(detail) > 0 {
				ret[child.Name] = detail
			}
		}
	}
	return ret
}
