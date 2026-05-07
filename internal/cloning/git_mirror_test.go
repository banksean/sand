package cloning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/banksean/sand/internal/hostops"
)

func TestStableRepoIDUsesCanonicalPathAndFileIdentity(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}

	fileOps := &hostops.MockFileOps{StatFunc: os.Stat}
	first, err := StableRepoID(fileOps, repo)
	if err != nil {
		t.Fatalf("StableRepoID(repo) error = %v", err)
	}
	second, err := StableRepoID(fileOps, link)
	if err != nil {
		t.Fatalf("StableRepoID(link) error = %v", err)
	}
	if first != second {
		t.Fatalf("repo IDs differ for canonical same path: %q vs %q", first, second)
	}
	if len(first) != 32 {
		t.Fatalf("repo ID length = %d, want 32", len(first))
	}
}

func TestGitMirrorEnsureUpdatedCreatesMissingMirror(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	root := t.TempDir()
	var clonedSource, clonedMirror string

	mirror := NewGitMirror(root, &hostops.MockGitOps{
		CloneMirrorFunc: func(ctx context.Context, sourceDir, mirrorDir string) error {
			clonedSource = sourceDir
			clonedMirror = mirrorDir
			return os.Mkdir(mirrorDir, 0o750)
		},
	}, &hostops.MockFileOps{
		StatFunc:     os.Stat,
		MkdirAllFunc: os.MkdirAll,
	})

	mirrorDir, err := mirror.EnsureUpdated(ctx, repo)
	if err != nil {
		t.Fatalf("EnsureUpdated() error = %v", err)
	}
	if clonedSource != repo {
		t.Fatalf("clone source = %q, want %q", clonedSource, repo)
	}
	if clonedMirror != mirrorDir {
		t.Fatalf("clone mirror = %q, want returned mirror %q", clonedMirror, mirrorDir)
	}
	if !strings.HasPrefix(mirrorDir, root) {
		t.Fatalf("mirror dir %q does not live under %q", mirrorDir, root)
	}
}

func TestGitMirrorEnsureUpdatedUpdatesExistingMirror(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	root := t.TempDir()
	probe := NewGitMirror(root, &hostops.MockGitOps{}, &hostops.MockFileOps{StatFunc: os.Stat})
	mirrorDir, err := probe.MirrorDir(repo)
	if err != nil {
		t.Fatalf("MirrorDir() error = %v", err)
	}
	if err := os.MkdirAll(mirrorDir, 0o750); err != nil {
		t.Fatal(err)
	}

	var updatedMirror string
	mirror := NewGitMirror(root, &hostops.MockGitOps{
		UpdateMirrorFunc: func(ctx context.Context, mirrorDir string) error {
			updatedMirror = mirrorDir
			return nil
		},
	}, &hostops.MockFileOps{
		StatFunc:     os.Stat,
		MkdirAllFunc: os.MkdirAll,
	})

	got, err := mirror.EnsureUpdated(ctx, repo)
	if err != nil {
		t.Fatalf("EnsureUpdated() error = %v", err)
	}
	if got != mirrorDir {
		t.Fatalf("mirror dir = %q, want %q", got, mirrorDir)
	}
	if updatedMirror != mirrorDir {
		t.Fatalf("updated mirror = %q, want %q", updatedMirror, mirrorDir)
	}
}

func TestGitMirrorEnsureUpdatedSerializesConcurrentCreation(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	root := t.TempDir()
	var cloneCount atomic.Int32

	mirror := NewGitMirror(root, &hostops.MockGitOps{
		CloneMirrorFunc: func(ctx context.Context, sourceDir, mirrorDir string) error {
			cloneCount.Add(1)
			return os.MkdirAll(mirrorDir, 0o750)
		},
		UpdateMirrorFunc: func(ctx context.Context, mirrorDir string) error {
			return nil
		},
	}, &hostops.MockFileOps{
		StatFunc:     os.Stat,
		MkdirAllFunc: os.MkdirAll,
	})

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := mirror.EnsureUpdated(ctx, repo); err != nil {
				t.Errorf("EnsureUpdated() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if got := cloneCount.Load(); got != 1 {
		t.Fatalf("CloneMirror called %d times, want 1", got)
	}
}

func TestGitMirrorWriteSnapshotRef(t *testing.T) {
	var gotDir, gotRef, gotValue string
	mirror := NewGitMirror("/mirrors", &hostops.MockGitOps{
		UpdateRefFunc: func(ctx context.Context, dir, ref, value string) error {
			gotDir = dir
			gotRef = ref
			gotValue = value
			return nil
		},
	}, &hostops.MockFileOps{})

	if err := mirror.WriteSnapshotRef(context.Background(), "/mirror/repo.git", "sandbox-1", "abc123"); err != nil {
		t.Fatalf("WriteSnapshotRef() error = %v", err)
	}
	if gotDir != "/mirror/repo.git" || gotRef != "refs/sand/snapshots/sandbox-1" || gotValue != "abc123" {
		t.Fatalf("UpdateRef args = (%q, %q, %q)", gotDir, gotRef, gotValue)
	}
}
