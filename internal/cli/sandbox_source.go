package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/banksean/sand/internal/hostops"
)

func checkSandboxSourceGit(ctx context.Context, gitOps hostops.GitOps, sourceDir string, stderr io.Writer) bool {
	if gitOps.TopLevel(ctx, sourceDir) != "" {
		return true
	}

	fmt.Fprintf(stderr, "warning: %q is not inside a git repository; sand's git features (sand git, git remotes, and sandbox branches) will not be available\n", sourceDir)
	return false
}
