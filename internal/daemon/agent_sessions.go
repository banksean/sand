package daemon

import (
	"context"
	"fmt"
	"io"

	"github.com/banksean/sand/internal/sandtypes"
)

func (d *Daemon) SyncAgentSessions(ctx context.Context, sandbox string) error {
	if d.sessions == nil {
		return fmt.Errorf("agent session archive is unavailable")
	}
	box, err := d.boxer.Get(ctx, sandbox)
	if err != nil {
		return err
	}
	if box == nil {
		return fmt.Errorf("sandbox not found: %s", sandbox)
	}
	return d.sessions.Sync(ctx, box)
}

func (d *Daemon) BeginAgentSessionLaunch(ctx context.Context, sandbox string) (string, error) {
	box, err := d.boxer.Get(ctx, sandbox)
	if err != nil {
		return "", err
	}
	if box == nil {
		return "", fmt.Errorf("sandbox not found: %s", sandbox)
	}
	return d.sessions.BeginLaunch(ctx, box)
}

func (d *Daemon) EndAgentSessionLaunch(ctx context.Context, launchID string) error {
	return d.sessions.EndLaunch(ctx, launchID)
}

func (d *Daemon) ListAgentSessions(ctx context.Context, agent, sandbox string) ([]sandtypes.AgentSession, error) {
	if err := d.syncAllAgentSessions(ctx); err != nil {
		return nil, err
	}
	return d.sessions.List(ctx, agent, sandbox)
}

func (d *Daemon) syncAllAgentSessions(ctx context.Context) error {
	boxes, err := d.boxer.List(ctx)
	if err != nil {
		return err
	}
	for i := range boxes {
		if !boxes[i].SessionArchiveEnabled {
			continue
		}
		if err := d.sessions.Sync(ctx, &boxes[i]); err != nil {
			return fmt.Errorf("sync sessions for sandbox %s: %w", boxes[i].Name, err)
		}
	}
	return nil
}

func (d *Daemon) GetAgentSession(ctx context.Context, id string) (sandtypes.AgentSession, error) {
	return d.sessions.Resolve(ctx, id)
}

func (d *Daemon) ReadAgentSession(ctx context.Context, id, format string, w io.Writer) error {
	return d.sessions.Read(ctx, id, format, w)
}

func (d *Daemon) ExportAgentSession(ctx context.Context, id, format, destination string) error {
	return d.sessions.Export(ctx, id, format, destination)
}

func (d *Daemon) DeleteAgentSession(ctx context.Context, id string) error {
	return d.sessions.Delete(ctx, id)
}
