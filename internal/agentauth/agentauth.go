package agentauth

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/banksean/sand/internal/cli/agentlaunch"
)

func ValidateSelection(agent, envFile string) error {
	if agent == "" {
		return nil
	}

	if !agentlaunch.HasAgent(agent) {
		return fmt.Errorf("unknown agent %q (supported: %s)", agent, strings.Join(agentlaunch.SupportedAgents(), ", "))
	}

	requiredGroups := agentlaunch.AuthEnvAnyOf(agent)
	if len(requiredGroups) == 0 {
		return nil
	}

	fileEnv, err := loadEnvFileValues(envFile)
	if err != nil {
		return err
	}

	for _, group := range requiredGroups {
		if hasAllEnvVars(group, fileEnv) {
			return nil
		}
	}

	return fmt.Errorf("agent %q requires authentication env vars before sandbox creation; set one of [%s] in the current environment or %s",
		agent,
		formatEnvGroups(requiredGroups),
		formatEnvFileForError(envFile),
	)
}

func hasAllEnvVars(names []string, fileEnv map[string]string) bool {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
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
