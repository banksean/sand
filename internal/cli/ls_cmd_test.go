package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/applecontainer/types"
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

func TestShortSandboxIDUsesLastUUIDSegment(t *testing.T) {
	got := shortSandboxID("3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b")
	want := "0d7e41f1df1b"
	if got != want {
		t.Fatalf("shortSandboxID() = %q, want %q", got, want)
	}
}

func TestShortSandboxIDLeavesNonUUIDAlone(t *testing.T) {
	got := shortSandboxID("sandbox-dev-1")
	if got != "sandbox-dev-1" {
		t.Fatalf("shortSandboxID() = %q, want original ID", got)
	}
}

func TestSamePathCanonicalizesSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	link := filepath.Join(dir, "repo-link")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if !samePath(target, link) {
		t.Fatalf("samePath(%q, %q) = false, want true", target, link)
	}
}

func TestCurrentWorkspaceDirUsesGitTopLevelFromNestedDir(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init").Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldCWD)
	})
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}

	got := currentWorkspaceDir(context.Background())
	want := canonicalPath(dir)
	if got != want {
		t.Fatalf("currentWorkspaceDir() = %q, want %q", got, want)
	}
}

func TestPrioritizeHereRowsKeepsRelativeOrder(t *testing.T) {
	rows := []lsRow{
		{Name: "old"},
		{Name: "here-1", Here: true},
		{Name: "other"},
		{Name: "here-2", Here: true},
	}
	gotRows := prioritizeHereRows(rows)
	got := []string{}
	for _, row := range gotRows {
		got = append(got, row.Name)
	}
	want := []string{"here-1", "here-2", "old", "other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prioritizeHereRows() = %v, want %v", got, want)
	}
}

func TestFormatStatsColumns(t *testing.T) {
	stats := &types.ContainerStats{
		CPUUsageUsec:     1500,
		NumProcesses:     3,
		MemoryUsageBytes: 1024,
		MemoryLimitBytes: 1024 * 1024,
		BlockReadBytes:   2048,
		BlockWriteBytes:  4096,
		NetworkTxBytes:   0,
		NetworkRxBytes:   512,
	}
	got := formatStatsColumns(stats)
	want := []string{"1.5ms", "3", "1.0KiB/1.0MiB", "2.0KiB/4.0KiB", "0B/512B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatStatsColumns() = %v, want %v", got, want)
	}
}

func TestRenderLsTableUsesShortIDAndStats(t *testing.T) {
	var buf bytes.Buffer
	rows := []lsRow{{
		Name:      "box",
		ID:        "3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b",
		Here:      true,
		Status:    "running",
		FromDir:   "~/project",
		ImageName: "default:latest",
		Stats: &types.ContainerStats{
			CPUUsageUsec:     1500,
			NumProcesses:     3,
			MemoryUsageBytes: 1024,
			MemoryLimitBytes: 1024 * 1024,
		},
	}}
	if err := renderLsTable(&buf, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"0d7e41f1df1b", "*", "1.5ms", "1.0KiB/1.0MiB"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderLsTable output missing %q:\n%s", want, out)
		}
	}
}
