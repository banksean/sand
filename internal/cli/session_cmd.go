package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/sessionarchive"
)

type SessionCmd struct {
	Ls     SessionLsCmd     `cmd:"" help:"list archived agent sessions"`
	Show   SessionShowCmd   `cmd:"" help:"show an archived transcript"`
	Export SessionExportCmd `cmd:"" help:"export an archived transcript"`
	Rm     SessionRmCmd     `cmd:"" help:"permanently delete an archived transcript"`
}

type SessionLsCmd struct {
	Agent   string `help:"filter by agent type"`
	Sandbox string `help:"filter by sandbox name or ID"`
	JSON    bool   `help:"write machine-readable JSON"`
}

func (c *SessionLsCmd) Run(cctx *CLIContext) error {
	sessions, err := cctx.Daemon.ListAgentSessions(cctx.Context, daemon.ListAgentSessionsOpts{Agent: c.Agent, Sandbox: c.Sandbox})
	if err != nil {
		return err
	}
	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(sessions)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tAGENT\tUPDATED\tSIZE\tSTATUS\tSANDBOX\tTITLE"); err != nil {
		return err
	}
	for _, session := range sessions {
		title := strings.ReplaceAll(session.Title, "\n", " ")
		fmt.Fprintf(w, "%.12s\t%s\t%s\t%s\t%s\t%s\t%s\n", session.ID, session.AgentType,
			session.UpdatedAt.Local().Format("2006-01-02 15:04"), sessionarchive.FormatSize(session.SizeBytes),
			session.Status, strings.Join(session.SandboxNames, ","), title)
	}
	return w.Flush()
}

type SessionShowCmd struct {
	ID     string `arg:"" help:"session ID or unambiguous prefix"`
	Format string `default:"text" enum:"text,json" help:"output format"`
}

func (c *SessionShowCmd) Run(cctx *CLIContext) error {
	return cctx.Daemon.ReadAgentSession(cctx.Context, c.ID, c.Format, os.Stdout)
}

type SessionExportCmd struct {
	ID         string `arg:"" help:"session ID or unambiguous prefix"`
	Format     string `default:"raw" enum:"raw,jsonl,markdown" help:"export format"`
	OutputPath string `short:"o" required:"" placeholder:"<host FS path>" help:"destination path"`
}

func (c *SessionExportCmd) Run(cctx *CLIContext) error {
	path := c.OutputPath
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		path = filepath.Join(cwd, path)
	}
	return cctx.Daemon.ExportAgentSession(cctx.Context, c.ID, c.Format, path)
}

var (
	sessionRmStdin  io.Reader = os.Stdin
	sessionRmStdout io.Writer = os.Stdout
)

type SessionRmCmd struct {
	ID    string `arg:"" help:"session ID or unambiguous prefix"`
	Force bool   `short:"f" help:"delete without confirmation"`
}

func (c *SessionRmCmd) Run(cctx *CLIContext) error {
	session, err := cctx.Daemon.GetAgentSession(cctx.Context, c.ID)
	if err != nil {
		return err
	}
	if !c.Force {
		fmt.Fprintf(sessionRmStdout, "delete archived %s session %.12s (%s) [y/N]? ", session.AgentType, session.ID, session.Title)
		text, err := readPromptLine(cctx.Context, bufio.NewReader(sessionRmStdin))
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintln(sessionRmStdout)
			return err
		}
		if !isYes(text) {
			return nil
		}
	}
	if err := cctx.Daemon.DeleteAgentSession(cctx.Context, session.ID); err != nil {
		return err
	}
	_, err = fmt.Fprintln(sessionRmStdout, session.ID)
	return err
}
