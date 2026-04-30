package daemon

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type resolvedAgentCapabilities struct {
	AuthRequired  bool
	AuthAvailable bool
}

func (d *Daemon) resolveCreateSandboxCapabilities(opts CreateSandboxOpts) (resolvedAgentCapabilities, error) {
	if d.boxer == nil || d.boxer.AgentRegistry == nil {
		return resolvedAgentCapabilities{}, fmt.Errorf("agent registry is not initialized")
	}

	if opts.Agent == "" {
		return resolvedAgentCapabilities{}, nil
	}

	agentConfig, ok := d.boxer.AgentRegistry.Lookup(opts.Agent)
	if !ok || !agentConfig.Selectable {
		return resolvedAgentCapabilities{}, fmt.Errorf("unknown agent %q (supported: %s)", opts.Agent, strings.Join(d.boxer.AgentRegistry.SelectableNames(), ", "))
	}

	fileEnv, err := loadEnvFileValues(opts.EnvFile)
	if err != nil {
		return resolvedAgentCapabilities{}, err
	}

	resolved := resolvedAgentCapabilities{}
	authSpec := agentConfig.Capabilities.Auth
	if authSpec == nil || len(authSpec.EnvAnyOf) == 0 {
		return resolved, nil
	}

	resolved.AuthRequired = true
	if hasAnyEnvGroup(authSpec.EnvAnyOf, fileEnv) {
		resolved.AuthAvailable = true
		return resolved, nil
	}

	return resolved, fmt.Errorf(
		"agent %q requires authentication env vars before sandbox creation; set one of [%s] in the sandd environment or %s",
		opts.Agent,
		formatEnvGroups(authSpec.EnvAnyOf),
		formatEnvFileForError(opts.EnvFile),
	)
}

func hasAnyEnvGroup(groups [][]string, fileEnv map[string]string) bool {
	for _, group := range groups {
		if hasAllEnvVars(group, fileEnv) {
			return true
		}
	}
	return false
}

func hasAllEnvVars(names []string, fileEnv map[string]string) bool {
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok && strings.TrimSpace(value) != "" {
			continue
		}
		if value := strings.TrimSpace(fileEnv[name]); value != "" {
			continue
		}
		return false
	}
	return true
}

func loadEnvFileValues(path string) (map[string]string, error) {
	values := make(map[string]string)
	if path == "" {
		return values, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, fmt.Errorf("reading env file %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}

		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading env file %q: %w", path, err)
	}
	return values, nil
}

func formatEnvGroups(groups [][]string) string {
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		parts = append(parts, strings.Join(group, " + "))
	}
	return strings.Join(parts, "; ")
}

func formatEnvFileForError(path string) string {
	if path == "" {
		return "the configured env file"
	}
	return fmt.Sprintf("env file %q", path)
}
