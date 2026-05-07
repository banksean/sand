package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
)

// ValidateConfigFiles reports keys in Kong YAML config files that cannot map to
// any flag in the application model. It intentionally mirrors kong-yaml's
// hyphen-based path lookup, so both "caches.mise" and "caches-mise" are valid
// spellings for the same flag.
func ValidateConfigFiles(k *kong.Kong, paths ...string) error {
	validKeys := validConfigKeys(k.Model.Node)
	var unknowns []configKeyError

	for _, path := range paths {
		expanded := kong.ExpandPath(path)
		cfg := map[string]any{}
		if err := decodeYAML(expanded, &cfg); err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) || errors.Is(err, io.EOF) {
				continue
			}
			return fmt.Errorf("%s: %w", expanded, err)
		}

		for _, key := range unknownConfigKeys(cfg, validKeys) {
			unknowns = append(unknowns, configKeyError{path: expanded, key: key})
		}
	}

	if len(unknowns) == 0 {
		return nil
	}

	sort.Slice(unknowns, func(i, j int) bool {
		if unknowns[i].path == unknowns[j].path {
			return unknowns[i].key < unknowns[j].key
		}
		return unknowns[i].path < unknowns[j].path
	})

	lines := []string{"invalid .sand.yaml configuration: unrecognized key(s)"}
	for _, unknown := range unknowns {
		lines = append(lines, fmt.Sprintf("  %s: %s", unknown.path, unknown.key))
	}
	return errors.New(strings.Join(lines, "\n"))
}

type configKeyError struct {
	path string
	key  string
}

func validConfigKeys(node *kong.Node) map[string]struct{} {
	keys := map[string]struct{}{}
	collectValidConfigKeys(node, nil, keys)
	return keys
}

func collectValidConfigKeys(node *kong.Node, commandPath []string, keys map[string]struct{}) {
	for _, flag := range node.Flags {
		if flag.Hidden {
			continue
		}
		key := strings.Join(append(commandPath, flag.Name), "-")
		keys[key] = struct{}{}
	}

	for _, child := range node.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}
		collectValidConfigKeys(child, append(commandPath, child.Name), keys)
	}
}

func unknownConfigKeys(cfg map[string]any, validKeys map[string]struct{}) []string {
	var unknown []string
	walkConfigLeaves(nil, cfg, func(path []string) {
		key := strings.Join(path, "-")
		if _, ok := validKeys[key]; !ok {
			unknown = append(unknown, strings.Join(path, "."))
		}
	})
	sort.Strings(unknown)
	return unknown
}

func walkConfigLeaves(path []string, value any, visit func(path []string)) {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 && len(path) > 0 {
			visit(path)
			return
		}
		for key, child := range typed {
			walkConfigLeaves(append(path, key), child, visit)
		}
	default:
		if len(path) > 0 {
			visit(path)
		}
	}
}
