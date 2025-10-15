package sand

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// filterClaudeJSON parses ~/.claude.json and filters the "projects" field
// down to just the entry with the key that equals cwd, replacing that key
// with the hard-coded value of "/app". It returns the re-marshaled JSON.
func filterClaudeJSON(ctx context.Context, cwd string) ([]byte, error) {
	slog.InfoContext(ctx, "filterClaudeJSON", "cwd", cwd)
	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	slog.InfoContext(ctx, "filterClaudeJSON", "cwd", cwd, "homeDir", homeDir)

	// Read ~/.claude.json
	claudeJSONPath := filepath.Join(homeDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", claudeJSONPath, err)
	}

	// Parse JSON into a map
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Get the projects field
	projects, ok := config["projects"]
	if !ok {
		return nil, fmt.Errorf("no 'projects' field found in %s", claudeJSONPath)
	}

	// Ensure projects is a map
	projectsMap, ok := projects.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("'projects' field is not a map")
	}

	// Create new filtered projects map with /app as the key
	filteredProjects := map[string]interface{}{}
	// Find the entry with key equal to cwd
	value, ok := projectsMap[cwd]
	if ok {
		filteredProjects["/app"] = value
	}

	// Replace the projects field
	config["projects"] = filteredProjects

	slog.InfoContext(ctx, "filterClaudeJSON sucess")
	// Re-marshal the JSON
	result, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filtered JSON: %w", err)
	}

	return result, nil
}
