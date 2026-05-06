package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/banksean/sand/internal/sandtypes"
)

type LsCmd struct{}

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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SANDBOX NAME\tSANDBOX ID\tSTATUS\tFROM DIR\tFROM GIT\tCURRENT GIT\tIMAGE NAME")
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
		hostOriginDir := sbox.HostOriginDir
		userHomeDir, err := os.UserHomeDir()
		if err == nil {
			hostOriginDir = strings.Replace(hostOriginDir, userHomeDir, "~", 1)
		}
		imgName := strings.TrimPrefix(sbox.ImageName, "ghcr.io/banksean/sand/")

		originalBranch := gitSummary(sbox.OriginalGitDetails)
		currentBranch := gitSummary(sbox.CurrentGitDetails)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", sbox.Name, sbox.ID, strings.Join(status, ", "), hostOriginDir, originalBranch, currentBranch, imgName)
	}
	w.Flush()
	return nil
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
