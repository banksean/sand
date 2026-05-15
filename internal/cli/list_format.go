package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/banksean/sand/internal/applecontainer/types"
)

type lsRow struct {
	Name       string
	ID         string
	Status     string
	FromDir    string
	FromGit    string
	CurrentGit string
	ImageName  string
	Stats      *types.ContainerStats
}

func renderLsTable(w io.Writer, currentRows, otherRows []lsRow, long bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	headings := []string{
		"NAME",
		"ID",
		"STATUS",
		"FROM DIR",
		"FROM GIT",
		"CURRENT GIT",
		"IMAGE",
	}
	if long {
		headings = append(headings, "CPU", "PROCS", "MEM", "BLOCK R/W", "NET TX/RX")
	}
	if _, err := fmt.Fprintln(tw, strings.Join(headings, "\t")); err != nil {
		return err
	}
	if err := renderLsRows(tw, currentRows, long); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(tw, "--- other sandboxes ---"); err != nil {
		return err
	}
	if err := renderLsRows(tw, otherRows, long); err != nil {
		return err
	}
	return tw.Flush()
}

func renderLsRows(w io.Writer, rows []lsRow, long bool) error {
	for _, row := range rows {
		values := []string{
			row.Name,
			shortSandboxID(row.ID),
			row.Status,
			row.FromDir,
			row.FromGit,
			row.CurrentGit,
			row.ImageName,
		}
		if long {
			values = append(values, formatStatsColumns(row.Stats)...)
		}
		if _, err := fmt.Fprintln(w, strings.Join(values, "\t")); err != nil {
			return err
		}
	}
	return nil
}

func formatStatsColumns(stats *types.ContainerStats) []string {
	if stats == nil {
		return []string{"-", "-", "-", "-", "-"}
	}
	return []string{
		formatCPUUsec(stats.CPUUsageUsec),
		strconv.Itoa(stats.NumProcesses),
		formatBytePair(stats.MemoryUsageBytes, stats.MemoryLimitBytes),
		formatBytePair(stats.BlockReadBytes, stats.BlockWriteBytes),
		formatBytePair(stats.NetworkTxBytes, stats.NetworkRxBytes),
	}
}

func formatBytePair(first, second int) string {
	return fmt.Sprintf("%s/%s", formatBytes(first), formatBytes(second))
}

func formatBytes(n int) string {
	if n < 0 {
		return "-"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f%s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1fEiB", value/unit)
}

func formatCPUUsec(usec int) string {
	if usec < 0 {
		return "-"
	}
	if usec < 1000 {
		return fmt.Sprintf("%dus", usec)
	}
	if usec < 1000*1000 {
		return fmt.Sprintf("%.1fms", float64(usec)/1000)
	}
	return fmt.Sprintf("%.1fs", float64(usec)/(1000*1000))
}

func shortSandboxID(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) == 5 &&
		len(parts[0]) == 8 &&
		len(parts[1]) == 4 &&
		len(parts[2]) == 4 &&
		len(parts[3]) == 4 &&
		len(parts[4]) == 12 &&
		allHex(parts) {
		return parts[4]
	}
	return id
}

func allHex(parts []string) bool {
	for _, part := range parts {
		for _, r := range part {
			if !('0' <= r && r <= '9') && !('a' <= r && r <= 'f') && !('A' <= r && r <= 'F') {
				return false
			}
		}
	}
	return true
}

func displayPath(path string, homeDir string) string {
	if homeDir == "" {
		return path
	}
	rel, err := filepath.Rel(homeDir, path)
	if err != nil {
		return path
	}
	if rel == "." {
		return "~"
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join("~", rel)
	}
	return path
}
