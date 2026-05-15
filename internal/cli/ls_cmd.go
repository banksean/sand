package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
)

type LsCmd struct {
	Long bool `short:"l" help:"show resource usage columns"`
}

func (c *LsCmd) Run(cctx *CLIContext) error {
	ctx := cctx.Context
	mc := cctx.Daemon

	list, err := mc.ListSandboxes(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "ListSandboxes", "error", err)
		return err
	}

	if len(list) == 0 {
		return nil
	}

	currentWorkspace := currentWorkspaceDir(ctx)
	var statsByContainerID map[string]*types.ContainerStats
	if c.Long {
		statsByContainerID = lsStatsByContainerID(ctx, mc, list)
	}
	userHomeDir, _ := os.UserHomeDir()
	currentRows := make([]lsRow, 0, len(list))
	otherRows := make([]lsRow, 0, len(list))
	for _, sbox := range list {
		ctr := sbox.Container
		status := []string{"dormant"}
		if ctr != nil {
			status[0] = ctr.Status
		}
		if sbox.SandboxContainerError != "" {
			status = append(status, sbox.SandboxContainerError)
		}
		if sbox.SandboxWorkDirError != "" {
			status = append(status, sbox.SandboxWorkDirError)
		}
		imgName := strings.TrimPrefix(sbox.ImageName, "ghcr.io/banksean/sand/")

		originalBranch := gitSummary(sbox.OriginalGitDetails)
		currentBranch := gitSummary(sbox.CurrentGitDetails)

		row := lsRow{
			Name:       sbox.Name,
			ID:         sbox.ID,
			Status:     strings.Join(status, ", "),
			FromDir:    displayPath(sbox.HostOriginDir, userHomeDir),
			FromGit:    originalBranch,
			CurrentGit: currentBranch,
			ImageName:  imgName,
			Stats:      statsByContainerID[sbox.ContainerID],
		}
		if samePath(currentWorkspace, sbox.HostOriginDir) {
			currentRows = append(currentRows, row)
		} else {
			otherRows = append(otherRows, row)
		}
	}
	return renderLsTable(os.Stdout, currentRows, otherRows, c.Long)
}

func currentWorkspaceDir(ctx context.Context) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	gitTopLevel := hostops.NewDefaultGitOps().TopLevel(ctx, cwd)
	if gitTopLevel != "" {
		return canonicalPath(gitTopLevel)
	}
	return canonicalPath(cwd)
}

func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return canonicalPath(a) == canonicalPath(b)
}

func lsStatsByContainerID(ctx context.Context, mc daemon.Client, list []sandtypes.Box) map[string]*types.ContainerStats {
	names := make([]string, 0, len(list))
	for _, sbox := range list {
		if sbox.Name == "" || sbox.ContainerID == "" || !isRunningContainer(sbox.Container) {
			continue
		}
		names = append(names, sbox.Name)
	}
	if len(names) == 0 {
		return nil
	}
	stats, err := mc.Stats(ctx, names...)
	if err != nil {
		slog.WarnContext(ctx, "Stats for sand ls", "error", err)
		return nil
	}
	byID := make(map[string]*types.ContainerStats, len(stats))
	for i := range stats {
		byID[stats[i].ID] = &stats[i]
	}
	return byID
}

func isRunningContainer(ctr *types.Container) bool {
	return ctr != nil && strings.EqualFold(ctr.Status, "running")
}

func gitSummary(details *sandtypes.GitDetails) string {
	if details == nil {
		return ""
	}
	commit := "        "
	if len(details.Commit) > 8 {
		commit = details.Commit[:8] + " "
	}
	ret := commit + details.Branch
	if details.IsDirty {
		ret = "*" + ret
	} else {
		ret = " " + ret
	}
	if details.HasRelative {
		ret += fmt.Sprintf(" (%d ahead, %d behind)", details.Ahead, details.Behind)
	}
	return ret
}
