package sessionarchive

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/banksean/sand/internal/db"
	"github.com/banksean/sand/internal/sandtypes"
)

func TestServiceSyncReadAndDeleteClaudeSession(t *testing.T) {
	root := t.TempDir()
	database, err := db.Connect(root)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := New(root, database)
	box := &sandtypes.Box{ID: "box-1", Name: "demo", AgentType: "claude", SandboxWorkDir: filepath.Join(root, "clones", "box-1"), SessionArchiveEnabled: true}
	nativeDir := NativeDir(root, box.ID, box.AgentType)
	if err := os.MkdirAll(filepath.Join(nativeDir, "-app"), 0o700); err != nil {
		t.Fatal(err)
	}
	nativePath := filepath.Join(nativeDir, "-app", "11111111-2222-3333-4444-555555555555.jsonl")
	data := strings.Join([]string{
		`{"type":"user","timestamp":"2026-07-25T12:00:00Z","message":{"role":"user","content":"fix the tests"}}`,
		`{"type":"assistant","timestamp":"2026-07-25T12:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"I'll inspect them."},{"type":"tool_use","id":"call-1","name":"shell","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","timestamp":"2026-07-25T12:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"ok"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(nativePath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := service.Sync(context.Background(), box); err != nil {
		t.Fatal(err)
	}
	sessions, err := service.List(context.Background(), "claude", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if sessions[0].Status != "complete" {
		t.Fatalf("status = %q", sessions[0].Status)
	}
	if sessions[0].Title != "fix the tests" {
		t.Fatalf("title = %q", sessions[0].Title)
	}

	var rendered bytes.Buffer
	if err := service.Read(context.Background(), sessions[0].ID[:12], "text", &rendered); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## User", "fix the tests", "Tool call: shell", "go test ./...", "Tool result: call-1"} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("rendered transcript missing %q:\n%s", want, rendered.String())
		}
	}

	var rawPath string
	if err := database.QueryRow("SELECT raw_path FROM agent_session_sources WHERE session_id=?", sessions[0].ID).Scan(&rawPath); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(rawPath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("raw mode = %o", info.Mode().Perm())
	}
	if err := service.Delete(context.Background(), sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rawPath); !os.IsNotExist(err) {
		t.Fatalf("raw snapshot still exists: %v", err)
	}
	if _, err := os.Stat(nativePath); !os.IsNotExist(err) {
		t.Fatalf("native transcript still exists: %v", err)
	}
}

func TestDiscoverFiltersAgentState(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"rollout-2026-07-25-id.jsonl", "session_index.jsonl", "history.jsonl"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := discover(root, "codex-jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "rollout-2026-07-25-id.jsonl" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestStableIDSeparatesAgentTypes(t *testing.T) {
	if stableID("claude", "same") == stableID("codex", "same") {
		t.Fatal("stable IDs collided across agent types")
	}
}

func TestDeleteRefusesActiveLaunch(t *testing.T) {
	root := t.TempDir()
	database, err := db.Connect(root)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := New(root, database)
	box := &sandtypes.Box{ID: "box-active", Name: "active", AgentType: "claude", SandboxWorkDir: filepath.Join(root, "clones", "box-active"), SessionArchiveEnabled: true}
	nativeDir := filepath.Join(NativeDir(root, box.ID, box.AgentType), "-app")
	if err := os.MkdirAll(nativeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "active.jsonl"), []byte(`{"type":"user","message":{"role":"user","content":"hello"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Sync(context.Background(), box); err != nil {
		t.Fatal(err)
	}
	sessions, err := service.List(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	launchID, err := service.BeginLaunch(context.Background(), box)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), sessions[0].ID); err == nil || !strings.Contains(err.Error(), "active agent launch") {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := service.EndLaunch(context.Background(), launchID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), sessions[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodeDatabaseCreatesLogicalSessions(t *testing.T) {
	root := t.TempDir()
	metadata, err := db.Connect(root)
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	service := New(root, metadata)
	box := &sandtypes.Box{ID: "box-open", Name: "open", AgentType: "opencode", SandboxWorkDir: filepath.Join(root, "clones", "box-open"), SessionArchiveEnabled: true}
	nativeDir := NativeDir(root, box.ID, box.AgentType)
	if err := os.MkdirAll(nativeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	nativePath := filepath.Join(nativeDir, "opencode.db")
	odb, err := sql.Open("sqlite", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`,
		`INSERT INTO session VALUES ('ses_one','First',1000,4000), ('ses_two','Second',2000,3000)`,
		`INSERT INTO message VALUES ('msg_one','ses_one',1000,'{"role":"user"}')`,
		`INSERT INTO part VALUES ('part_text','msg_one','ses_one',1100,'{"type":"text","text":"hello opencode"}')`,
		`INSERT INTO part VALUES ('part_tool','msg_one','ses_one',1200,'{"type":"tool","callID":"call_one","tool":"bash","state":{"status":"completed","input":{"command":"pwd"},"output":"/app"}}')`,
	} {
		if _, err := odb.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := odb.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.Sync(context.Background(), box); err != nil {
		t.Fatal(err)
	}
	sessions, err := service.List(context.Background(), "opencode", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	var first sandtypes.AgentSession
	for _, session := range sessions {
		if session.NativeSessionID == "ses_one" {
			first = session
		}
	}
	if first.ID == "" {
		t.Fatal("ses_one not found")
	}
	var rendered bytes.Buffer
	if err := service.Read(context.Background(), first.ID, "text", &rendered); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hello opencode", "Tool call: bash", "Tool result: call_one"} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("transcript missing %q:\n%s", want, rendered.String())
		}
	}
}
