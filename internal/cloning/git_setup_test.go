package cloning

import (
	"context"
	"testing"

	"github.com/banksean/sand/internal/hostops"
)

func TestSetupGitRemotesUsesSandboxName(t *testing.T) {
	var addedRemote string
	gitOps := &hostops.MockGitOps{
		TopLevelFunc: func(ctx context.Context, dir string) string {
			return "/repo"
		},
		AddRemoteFunc: func(ctx context.Context, dir, name, url string) error {
			addedRemote = name
			return nil
		},
	}

	setup := NewGitSetup(gitOps)
	if err := setup.SetupGitRemotes(context.Background(), "id-123", "friendly", "/repo", "/clone", ""); err != nil {
		t.Fatalf("SetupGitRemotes() error = %v", err)
	}

	if addedRemote != "sand/friendly" {
		t.Fatalf("added remote = %q, want sand/friendly", addedRemote)
	}
}

func TestSetupGitRemotesReplacesExistingSandboxNameRemote(t *testing.T) {
	var removedRemote, addedRemote string
	gitOps := &hostops.MockGitOps{
		TopLevelFunc: func(ctx context.Context, dir string) string {
			return "/repo"
		},
		RemoteURLFunc: func(ctx context.Context, dir, name string) string {
			if name == "sand/friendly" {
				return "/old-clone"
			}
			return ""
		},
		RemoveRemoteFunc: func(ctx context.Context, dir, name string) error {
			removedRemote = name
			return nil
		},
		AddRemoteFunc: func(ctx context.Context, dir, name, url string) error {
			addedRemote = name
			return nil
		},
	}

	setup := NewGitSetup(gitOps)
	if err := setup.SetupGitRemotes(context.Background(), "id-123", "friendly", "/repo", "/clone", ""); err != nil {
		t.Fatalf("SetupGitRemotes() error = %v", err)
	}

	if removedRemote != "sand/friendly" {
		t.Fatalf("removed remote = %q, want sand/friendly", removedRemote)
	}
	if addedRemote != "sand/friendly" {
		t.Fatalf("added remote = %q, want sand/friendly", addedRemote)
	}
}

func TestSetupGitRemotesSetsCloneBranchUpstreamToOrigin(t *testing.T) {
	var gotDir, gotBranch, gotRemote string
	gitOps := &hostops.MockGitOps{
		TopLevelFunc: func(ctx context.Context, dir string) string {
			return "/repo"
		},
		BranchFunc: func(ctx context.Context, dir string) string {
			if dir == "/clone" {
				return "main"
			}
			return ""
		},
		SetBranchUpstreamFunc: func(ctx context.Context, dir, branch, remote string) error {
			gotDir = dir
			gotBranch = branch
			gotRemote = remote
			return nil
		},
	}

	setup := NewGitSetup(gitOps)
	if err := setup.SetupGitRemotes(context.Background(), "id-123", "friendly", "/repo", "/clone", ""); err != nil {
		t.Fatalf("SetupGitRemotes() error = %v", err)
	}

	if gotDir != "/clone" || gotBranch != "main" || gotRemote != "origin" {
		t.Fatalf("SetBranchUpstream args = (%q, %q, %q), want (/clone, main, origin)", gotDir, gotBranch, gotRemote)
	}
}

func TestSetupGitRemotesPointsCloneOriginAtMirrorBeforeFetch(t *testing.T) {
	var gotSetRemoteURLDir, gotSetRemoteURLName, gotSetRemoteURL string
	var fetches []string
	gitOps := &hostops.MockGitOps{
		TopLevelFunc: func(ctx context.Context, dir string) string {
			return "/repo"
		},
		SetRemoteURLFunc: func(ctx context.Context, dir, name, url string) error {
			gotSetRemoteURLDir = dir
			gotSetRemoteURLName = name
			gotSetRemoteURL = url
			return nil
		},
		FetchFunc: func(ctx context.Context, dir, remote string) error {
			fetches = append(fetches, dir+":"+remote)
			return nil
		},
	}

	setup := NewGitSetup(gitOps)
	if err := setup.SetupGitRemotes(context.Background(), "id-123", "friendly", "/repo", "/clone", "/mirror/repo.git"); err != nil {
		t.Fatalf("SetupGitRemotes() error = %v", err)
	}

	if gotSetRemoteURLDir != "/clone" || gotSetRemoteURLName != "origin" || gotSetRemoteURL != "/mirror/repo.git" {
		t.Fatalf("SetRemoteURL args = (%q, %q, %q), want (/clone, origin, /mirror/repo.git)", gotSetRemoteURLDir, gotSetRemoteURLName, gotSetRemoteURL)
	}
	if len(fetches) == 0 || fetches[0] != "/clone:origin" {
		t.Fatalf("first fetch = %v, want /clone:origin", fetches)
	}
}
