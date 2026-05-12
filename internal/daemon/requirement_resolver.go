package daemon

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/sandtypes"
)

type resolvedAgentRequirements struct {
	AuthRequired  bool
	AuthAvailable bool
	AuthEnv       map[string]string
}

func (d *Daemon) validateSelectableAgent(agent string) error {
	if agent == "" {
		return nil
	}
	_, err := d.lookupSelectableAgent(agent)
	return err
}

func (d *Daemon) lookupSelectableAgent(agent string) (*cloning.AgentConfig, error) {
	if d.boxer == nil || d.boxer.AgentRegistry == nil {
		return nil, fmt.Errorf("agent registry is not initialized")
	}

	agentConfig, ok := d.boxer.AgentRegistry.Lookup(agent)
	if !ok || !agentConfig.Selectable {
		return nil, fmt.Errorf("unknown agent %q (supported: %s)", agent, strings.Join(d.boxer.AgentRegistry.SelectableNames(), ", "))
	}
	return agentConfig, nil
}

func (d *Daemon) resolveCreateSandboxRequirements(opts CreateSandboxOpts) (resolvedAgentRequirements, error) {
	if opts.Agent == "" {
		return resolvedAgentRequirements{}, nil
	}

	agentConfig, err := d.lookupSelectableAgent(opts.Agent)
	if err != nil {
		return resolvedAgentRequirements{}, err
	}

	fileEnv, processEnvNames, restrictProcessEnv, err := resolveAuthSources(opts)
	if err != nil {
		return resolvedAgentRequirements{}, err
	}

	resolved := resolvedAgentRequirements{}
	authSpec := agentConfig.Requirements.Auth
	if authSpec == nil || len(authSpec.EnvAnyOf) == 0 {
		return resolved, nil
	}

	resolved.AuthRequired = true
	if authEnv, ok := resolveAnyEnvGroup(authSpec.EnvAnyOf, fileEnv, processEnvNames, restrictProcessEnv); ok {
		resolved.AuthAvailable = true
		resolved.AuthEnv = authEnv
		return resolved, nil
	}

	return resolved, fmt.Errorf(
		"agent %q requires authentication env vars before launch; set one of [%s] in the sandd environment or %s",
		opts.Agent,
		formatEnvGroups(authSpec.EnvAnyOf),
		formatEnvFileForError(opts.EnvFile),
	)
}

func resolveAuthSources(opts CreateSandboxOpts) (map[string]string, map[string]struct{}, bool, error) {
	if !opts.ProfileEnvConfigured && !envPolicyConfigured(opts.ProfileEnv) {
		fileEnv, err := loadEnvFileValues(opts.EnvFile)
		return fileEnv, nil, false, err
	}

	fileEnv, err := loadAuthEnvFileValues(opts.ProfileEnv.Files)
	if err != nil {
		return nil, nil, true, err
	}
	return fileEnv, authEnvVarNames(opts.ProfileEnv.Vars), true, nil
}

func envPolicyConfigured(policy sandtypes.EnvPolicy) bool {
	return len(policy.Files) > 0 || len(policy.Vars) > 0
}

func loadAuthEnvFileValues(files []sandtypes.EnvFileRef) (map[string]string, error) {
	values := make(map[string]string)
	for _, file := range files {
		if !envScopeAllowsAuth(file.Scope) {
			continue
		}
		fileValues, err := loadEnvFileValues(file.Path)
		if err != nil {
			return nil, err
		}
		for key, value := range fileValues {
			values[key] = value
		}
	}
	return values, nil
}

func authEnvVarNames(vars []sandtypes.EnvVarRule) map[string]struct{} {
	names := make(map[string]struct{}, len(vars))
	for _, variable := range vars {
		if !envScopeAllowsAuth(variable.Scope) || strings.TrimSpace(variable.Name) == "" {
			continue
		}
		names[variable.Name] = struct{}{}
	}
	return names
}

func envScopeAllowsAuth(scope sandtypes.EnvScope) bool {
	return scope == sandtypes.EnvScopeAuth || scope == sandtypes.EnvScopeAll
}

func resolveAnyEnvGroup(groups [][]string, fileEnv map[string]string, processEnvNames map[string]struct{}, restrictProcessEnv bool) (map[string]string, bool) {
	for _, group := range groups {
		if values, ok := resolveAllEnvVars(group, fileEnv, processEnvNames, restrictProcessEnv); ok {
			return values, true
		}
	}
	return nil, false
}

func resolveAllEnvVars(names []string, fileEnv map[string]string, processEnvNames map[string]struct{}, restrictProcessEnv bool) (map[string]string, bool) {
	values := make(map[string]string, len(names))
	for _, name := range names {
		value, ok := resolveEnvValue(name, fileEnv, processEnvNames, restrictProcessEnv)
		if !ok {
			return nil, false
		}
		values[name] = value
	}
	return values, true
}

func resolveEnvValue(name string, fileEnv map[string]string, processEnvNames map[string]struct{}, restrictProcessEnv bool) (string, bool) {
	if !restrictProcessEnv || processEnvAllowed(name, processEnvNames) {
		if value, ok := os.LookupEnv(name); ok && strings.TrimSpace(value) != "" {
			return value, true
		}
	}
	if value := strings.TrimSpace(fileEnv[name]); value != "" {
		return value, true
	}
	return "", false
}

func processEnvAllowed(name string, processEnvNames map[string]struct{}) bool {
	_, ok := processEnvNames[name]
	return ok
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
