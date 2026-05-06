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
	if err := setup.SetupGitRemotes(context.Background(), "id-123", "friendly", "/repo", "/clone"); err != nil {
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
	if err := setup.SetupGitRemotes(context.Background(), "id-123", "friendly", "/repo", "/clone"); err != nil {
		t.Fatalf("SetupGitRemotes() error = %v", err)
	}

	if removedRemote != "sand/friendly" {
		t.Fatalf("removed remote = %q, want sand/friendly", removedRemote)
	}
	if addedRemote != "sand/friendly" {
		t.Fatalf("added remote = %q, want sand/friendly", addedRemote)
	}
}
