package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/hostops"
)

func TestCheckSandboxSourceGit(t *testing.T) {
	t.Run("git source", func(t *testing.T) {
		var gotDir string
		gitOps := &hostops.MockGitOps{
			TopLevelFunc: func(_ context.Context, dir string) string {
				gotDir = dir
				return "/project"
			},
		}
		var stderr bytes.Buffer

		if !checkSandboxSourceGit(context.Background(), gitOps, "/project/subdir", &stderr) {
			t.Fatal("checkSandboxSourceGit() = false, want true")
		}
		if gotDir != "/project/subdir" {
			t.Fatalf("TopLevel() dir = %q, want %q", gotDir, "/project/subdir")
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("non-git source", func(t *testing.T) {
		gitOps := &hostops.MockGitOps{
			TopLevelFunc: func(_ context.Context, dir string) string {
				if dir != "/workspace" {
					t.Fatalf("TopLevel() dir = %q, want %q", dir, "/workspace")
				}
				return ""
			},
		}
		var stderr bytes.Buffer

		if checkSandboxSourceGit(context.Background(), gitOps, "/workspace", &stderr) {
			t.Fatal("checkSandboxSourceGit() = true, want false")
		}
		for _, want := range []string{"warning:", `"/workspace"`, "sand git", "git remotes", "sandbox branches"} {
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
			}
		}
	})
}
