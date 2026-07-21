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

func TestFormatStatsColumns(t *testing.T) {
	stats := &sandtypes.ContainerStats{
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

func TestRenderLsTableSplitsCurrentAndOtherRows(t *testing.T) {
	var buf bytes.Buffer
	currentRows := []lsRow{{Name: "current", ID: "3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b", Status: "running"}}
	otherRows := []lsRow{{Name: "other", ID: "sandbox-dev-1", Status: "dormant"}}
	if err := renderLsTable(&buf, currentRows, otherRows, nil, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "HERE") || strings.Contains(out, "CPU") {
		t.Fatalf("default renderLsTable output has unexpected columns:\n%s", out)
	}
	currentIndex := strings.Index(out, "current")
	delimiterIndex := strings.Index(out, "--- other sandboxes ---")
	otherIndex := strings.Index(out, "other")
	if currentIndex < 0 || delimiterIndex < 0 || otherIndex < 0 {
		t.Fatalf("renderLsTable output missing expected rows or delimiter:\n%s", out)
	}
	if !(currentIndex < delimiterIndex && delimiterIndex < otherIndex) {
		t.Fatalf("renderLsTable did not split rows as expected:\n%s", out)
	}
}

func TestRenderLsTableLongUsesShortIDAndStats(t *testing.T) {
	var buf bytes.Buffer
	rows := []lsRow{{
		Name:      "box",
		ID:        "3a9a0df8-3ad2-4b79-9a4f-0d7e41f1df1b",
		Status:    "running",
		FromDir:   "~/project",
		ImageName: "base:latest",
		Stats: &sandtypes.ContainerStats{
			CPUUsageUsec:     1500,
			NumProcesses:     3,
			MemoryUsageBytes: 1024,
			MemoryLimitBytes: 1024 * 1024,
		},
	}}
	if err := renderLsTable(&buf, rows, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"CPU", "0d7e41f1df1b", "1.5ms", "1.0KiB/1.0MiB"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderLsTable output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "HERE") {
		t.Fatalf("renderLsTable output includes removed HERE column:\n%s", out)
	}
}

func TestRenderLsTableIncludesDeletedSectionAfterActiveRows(t *testing.T) {
	var buf bytes.Buffer
	currentRows := []lsRow{{Name: "current", ID: "current-id", Status: "running"}}
	otherRows := []lsRow{{Name: "other", ID: "other-id", Status: "dormant"}}
	deletedRows := []lsRow{{Name: "deleted", ID: "deleted-id", Status: "deleted"}}
	if err := renderLsTable(&buf, currentRows, otherRows, deletedRows, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	currentIndex := strings.Index(out, "current")
	otherDelimiterIndex := strings.Index(out, "--- other sandboxes ---")
	otherIndex := strings.Index(out, "other")
	deletedDelimiterIndex := strings.Index(out, deletedSanboxesHeader)
	deletedIndex := strings.Index(out, "deleted-id")
	if currentIndex < 0 || otherDelimiterIndex < 0 || otherIndex < 0 || deletedDelimiterIndex < 0 || deletedIndex < 0 {
		t.Fatalf("renderLsTable output missing expected rows or delimiters:\n%s", out)
	}
	if !(currentIndex < otherDelimiterIndex && otherDelimiterIndex < otherIndex && otherIndex < deletedDelimiterIndex && deletedDelimiterIndex < deletedIndex) {
		t.Fatalf("renderLsTable did not order deleted section after active rows:\n%s", out)
	}
}

func TestRowFromSandboxUsesDeletedStatus(t *testing.T) {
	row := rowFromSandbox(sandtypes.Box{
		ID:            "deleted-id",
		Name:          "deleted-name",
		State:         "deleted",
		HostOriginDir: "/home/user/project",
		ImageName:     "ghcr.io/banksean/sand/base:latest",
	}, "/home/user", nil)
	if row.Status != "deleted" {
		t.Fatalf("row status = %q, want deleted", row.Status)
	}
	if row.FromDir != "~/project" {
		t.Fatalf("row FromDir = %q, want ~/project", row.FromDir)
	}
	if row.ImageName != "base:latest" {
		t.Fatalf("row ImageName = %q, want base:latest", row.ImageName)
	}
}
