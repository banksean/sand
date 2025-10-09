package version

import "testing"

func TestEqual(t *testing.T) {
	tests := []struct {
		name     string
		v1       Info
		v2       Info
		expected bool
	}{
		{
			name:     "both empty",
			v1:       Info{},
			v2:       Info{},
			expected: true,
		},
		{
			name:     "same commit",
			v1:       Info{GitCommit: "abc123"},
			v2:       Info{GitCommit: "abc123"},
			expected: true,
		},
		{
			name:     "different commits",
			v1:       Info{GitCommit: "abc123"},
			v2:       Info{GitCommit: "def456"},
			expected: false,
		},
		{
			name:     "one empty one set",
			v1:       Info{GitCommit: "abc123"},
			v2:       Info{},
			expected: false,
		},
		{
			name:     "same commit different build time",
			v1:       Info{GitCommit: "abc123", BuildTime: "2024-01-01"},
			v2:       Info{GitCommit: "abc123", BuildTime: "2024-01-02"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.v1.Equal(tt.v2)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
