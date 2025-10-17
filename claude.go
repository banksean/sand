package sand

import (
	"context"
	"encoding/json"
	"errors"
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
		if errors.Is(err, os.ErrNotExist) {
			slog.InfoContext(ctx, "filterClaudeJSON missing file, using default", "path", claudeJSONPath)
			return json.Marshal(map[string]any{"projects": map[string]any{}})
		}
		return nil, fmt.Errorf("failed to read %s: %w", claudeJSONPath, err)
	}

	// Parse JSON into a map
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Extract the projects map if present
	projectsMap := map[string]interface{}{}
	if projects, ok := config["projects"]; ok {
		if cast, ok := projects.(map[string]interface{}); ok {
			projectsMap = cast
		} else {
			slog.InfoContext(ctx, "filterClaudeJSON non-map projects field, resetting", "type", fmt.Sprintf("%T", projects))
		}
	}

	// Create new filtered projects map with /app as the key when we have a match
	filteredProjects := map[string]interface{}{}
	if value, ok := projectsMap[cwd]; ok {
		filteredProjects["/app"] = value
	}

	// Replace the projects field
	config["projects"] = filteredProjects

	slog.InfoContext(ctx, "filterClaudeJSON success")
	// Re-marshal the JSON
	result, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filtered JSON: %w", err)
	}

	return result, nil
}
