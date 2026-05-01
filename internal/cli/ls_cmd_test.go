package cli

import (
	"testing"

	"github.com/banksean/sand/internal/sandtypes"
)

func TestGitSummaryIncludesRelativeCounts(t *testing.T) {
	got := gitSummary(&sandtypes.GitDetails{
		Branch:      "feature",
		Commit:      "1234567890abcdef",
		IsDirty:     true,
		HasRelative: true,
		Ahead:       2,
		Behind:      1,
	})
	want := "*12345678 feature (2 ahead, 1 behind)"
	if got != want {
		t.Fatalf("gitSummary() = %q, want %q", got, want)
	}
}
