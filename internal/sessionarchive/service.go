package sessionarchive

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const adapterVersion = 1

type Service struct {
	root string
	db   *sql.DB
}

func New(appRoot string, db *sql.DB) *Service {
	service := &Service{root: filepath.Join(appRoot, "agent-sessions"), db: db}
	// A daemon restart terminates the RPC-side launch lease. Native stores remain
	// durable and will be reconciled on the next list/lifecycle operation.
	_, _ = db.Exec("UPDATE agent_session_launches SET ended_at=CURRENT_TIMESTAMP WHERE ended_at IS NULL")
	return service
}

func (s *Service) BeginLaunch(ctx context.Context, box *sandtypes.Box) (string, error) {
	if box == nil || !box.SessionArchiveEnabled {
		return "", fmt.Errorf("sandbox does not support agent session archival")
	}
	root := filepath.Join(s.root, "native", box.ID, box.AgentType)
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("agent session archive is unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("agent session archive is not a directory: %s", root)
	}
	probe, err := os.CreateTemp(root, ".capture-probe-*")
	if err != nil {
		return "", fmt.Errorf("agent session archive is not writable: %w", err)
	}
	probeName := probe.Name()
	if err := probe.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(probeName); err != nil {
		return "", err
	}
	id := uuid.NewString()
	_, err = s.db.ExecContext(ctx, "INSERT INTO agent_session_launches (id, sandbox_id, agent_type) VALUES (?, ?, ?)", id, box.ID, box.AgentType)
	return id, err
}

func (s *Service) EndLaunch(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE agent_session_launches SET ended_at=CURRENT_TIMESTAMP WHERE id=? AND ended_at IS NULL", id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return fmt.Errorf("active agent launch not found: %s", id)
	}
	return nil
}

func (s *Service) EndSandboxLaunches(ctx context.Context, sandboxID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE agent_session_launches SET ended_at=CURRENT_TIMESTAMP WHERE sandbox_id=? AND ended_at IS NULL", sandboxID)
	return err
}

func NativeDir(appRoot, sandboxID, agent string) string {
	return filepath.Join(appRoot, "agent-sessions", "native", sandboxID, agent)
}

type Event struct {
	Sequence   int             `json:"sequence"`
	Timestamp  string          `json:"timestamp,omitempty"`
	Kind       string          `json:"kind"`
	Role       string          `json:"role,omitempty"`
	Content    string          `json:"content,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Status     string          `json:"status,omitempty"`
	NativeType string          `json:"native_type,omitempty"`
}

type Manifest struct {
	SchemaVersion   int              `json:"schema_version"`
	ID              string           `json:"id"`
	AgentType       string           `json:"agent_type"`
	NativeSessionID string           `json:"native_session_id"`
	Title           string           `json:"title,omitempty"`
	Status          string           `json:"status"`
	AdapterVersion  int              `json:"adapter_version"`
	Sources         []ManifestSource `json:"sources"`
}

type ManifestSource struct {
	SandboxID   string `json:"sandbox_id"`
	SandboxName string `json:"sandbox_name"`
	RawFile     string `json:"raw_file"`
}

func (s *Service) Sync(ctx context.Context, box *sandtypes.Box) error {
	if box == nil || !box.SessionArchiveEnabled {
		return nil
	}
	definition, ok := agentdefs.Lookup(box.AgentType)
	if !ok || definition.SessionFormat == "" {
		return nil
	}
	root := NativeDir(filepath.Dir(filepath.Dir(box.SandboxWorkDir)), box.ID, box.AgentType)
	// The normal daemon layout is appRoot/clones/<id>; use the configured root for
	// custom clone roots and tests.
	wantRoot := filepath.Join(s.root, "native", box.ID, box.AgentType)
	if _, err := os.Stat(wantRoot); err == nil || errors.Is(err, os.ErrNotExist) {
		root = wantRoot
	}
	paths, err := discover(root, definition.SessionFormat)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if definition.SessionFormat == "opencode-sqlite" {
			if err := s.syncOpenCodeDB(ctx, box, path); err != nil {
				return err
			}
			continue
		}
		if err := s.syncFile(ctx, box, definition.SessionFormat, path); err != nil {
			return err
		}
	}
	return nil
}

// ScrubCredentials removes co-located authentication state after a sandbox is
// removed. Transcript snapshots never include these files.
func (s *Service) ScrubCredentials(box *sandtypes.Box) error {
	if box == nil || !box.SessionArchiveEnabled || box.AgentType != "opencode" {
		return nil
	}
	root := filepath.Join(s.root, "native", box.ID, box.AgentType)
	for _, name := range []string{"auth.json", "auth.json.tmp"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func discover(root, format string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		switch format {
		case "claude-jsonl":
			if strings.HasSuffix(name, ".jsonl") && name != "sessions-index.jsonl" {
				paths = append(paths, path)
			}
		case "codex-jsonl":
			if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") {
				paths = append(paths, path)
			}
		case "gemini-json":
			if strings.HasSuffix(name, ".json") && strings.Contains(filepath.ToSlash(path), "/chats/") {
				paths = append(paths, path)
			}
		case "opencode-sqlite":
			if name == "opencode.db" {
				paths = append(paths, path)
			}
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func nativeID(format, path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".jsonl"), ".json")
	if format == "codex-jsonl" {
		parts := strings.Split(name, "-")
		if len(parts) >= 6 {
			return strings.Join(parts[len(parts)-5:], "-")
		}
	}
	if format == "opencode-sqlite" {
		return "opencode-store-" + filepath.Base(filepath.Dir(path))
	}
	return name
}

func stableID(agent, native string) string {
	sum := sha256.Sum256([]byte(agent + "\x00" + native))
	return hex.EncodeToString(sum[:16])
}

func (s *Service) syncFile(ctx context.Context, box *sandtypes.Box, format, path string) error {
	sourceInfo, err := os.Stat(path)
	if err != nil {
		return err
	}
	native := nativeID(format, path)
	id := stableID(box.AgentType, native)
	rawPath := filepath.Join(s.root, "raw", id, box.ID+"-"+filepath.Base(path))
	if err := snapshotRaw(format, path, rawPath); err != nil {
		return fmt.Errorf("snapshot raw agent session: %w", err)
	}
	info, err := os.Stat(rawPath)
	if err != nil {
		return err
	}
	normalized := filepath.Join(s.root, "normalized", id+".jsonl")
	status := "raw-only"
	title := ""
	if format != "opencode-sqlite" {
		events, parseErr := normalizeFile(rawPath)
		if parseErr == nil {
			if err := writeEvents(normalized, events); err != nil {
				return err
			}
			status = "complete"
			title = firstUserMessage(events)
		} else {
			status = "normalization-failed"
		}
	}
	if len(title) > 160 {
		title = title[:160]
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO agent_sessions (id, agent_type, native_session_id, title, status, normalized_path, size_bytes, started_at, updated_at, adapter_version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(agent_type, native_session_id) DO UPDATE SET
	  title=CASE WHEN excluded.title='' THEN agent_sessions.title ELSE excluded.title END,
	  status=excluded.status, normalized_path=excluded.normalized_path,
	  started_at=min(agent_sessions.started_at, excluded.started_at),
	  updated_at=max(agent_sessions.updated_at, excluded.updated_at), adapter_version=excluded.adapter_version`,
		id, box.AgentType, native, title, status, normalized, info.Size(), sourceInfo.ModTime(), sourceInfo.ModTime(), adapterVersion)
	if err != nil {
		return fmt.Errorf("upsert agent session: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO agent_session_sources (session_id, sandbox_id, sandbox_name, native_path, raw_path, size_bytes, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(raw_path) DO UPDATE SET sandbox_name=excluded.sandbox_name, native_path=excluded.native_path, size_bytes=excluded.size_bytes, updated_at=excluded.updated_at`,
		id, box.ID, box.Name, path, rawPath, info.Size(), sourceInfo.ModTime())
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "UPDATE agent_sessions SET size_bytes=(SELECT COALESCE(sum(size_bytes), 0) FROM agent_session_sources WHERE session_id=?) WHERE id=?", id, id)
	if err != nil {
		return err
	}
	return s.writeManifest(ctx, id)
}

func (s *Service) syncOpenCodeDB(ctx context.Context, box *sandtypes.Box, path string) error {
	snapshot := filepath.Join(s.root, "snapshots", box.ID+"-opencode.db")
	if err := snapshotRaw("opencode-sqlite", path, snapshot); err != nil {
		return err
	}
	database, err := sql.Open("sqlite", snapshot)
	if err != nil {
		return err
	}
	defer database.Close()
	rows, err := database.QueryContext(ctx, "SELECT id, title, time_created, time_updated FROM session ORDER BY time_updated")
	if err != nil {
		return fmt.Errorf("read opencode sessions: %w", err)
	}
	type row struct {
		id, title        string
		created, updated int64
	}
	var sessions []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.title, &item.created, &item.updated); err != nil {
			rows.Close()
			return err
		}
		sessions = append(sessions, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, session := range sessions {
		id := stableID(box.AgentType, session.id)
		rawPath := filepath.Join(s.root, "raw", id, box.ID+"-opencode.jsonl")
		events, err := exportOpenCodeSession(ctx, database, session.id, rawPath)
		if err != nil {
			return err
		}
		normalized := filepath.Join(s.root, "normalized", id+".jsonl")
		if err := writeEvents(normalized, events); err != nil {
			return err
		}
		info, err := os.Stat(rawPath)
		if err != nil {
			return err
		}
		created := time.UnixMilli(session.created)
		updated := time.UnixMilli(session.updated)
		_, err = s.db.ExecContext(ctx, `INSERT INTO agent_sessions (id, agent_type, native_session_id, title, status, normalized_path, size_bytes, started_at, updated_at, adapter_version)
VALUES (?, ?, ?, ?, 'complete', ?, ?, ?, ?, ?)
ON CONFLICT(agent_type, native_session_id) DO UPDATE SET title=excluded.title, status='complete', normalized_path=excluded.normalized_path,
started_at=min(agent_sessions.started_at, excluded.started_at), updated_at=max(agent_sessions.updated_at, excluded.updated_at), adapter_version=excluded.adapter_version`,
			id, box.AgentType, session.id, session.title, normalized, info.Size(), created, updated, adapterVersion)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO agent_session_sources (session_id, sandbox_id, sandbox_name, native_path, raw_path, size_bytes, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(raw_path) DO UPDATE SET sandbox_name=excluded.sandbox_name, size_bytes=excluded.size_bytes, updated_at=excluded.updated_at`,
			id, box.ID, box.Name, path, rawPath, info.Size(), updated)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, "UPDATE agent_sessions SET size_bytes=(SELECT COALESCE(sum(size_bytes), 0) FROM agent_session_sources WHERE session_id=?) WHERE id=?", id, id)
		if err != nil {
			return err
		}
		if err := s.writeManifest(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func exportOpenCodeSession(ctx context.Context, database *sql.DB, sessionID, destination string) ([]Event, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".opencode-*.tmp")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return nil, err
	}
	enc := json.NewEncoder(tmp)
	var events []Event
	roles := map[string]string{}
	messageRows, err := database.QueryContext(ctx, "SELECT id, time_created, data FROM message WHERE session_id=? ORDER BY time_created, id", sessionID)
	if err != nil {
		tmp.Close()
		return nil, err
	}
	for messageRows.Next() {
		var id, data string
		var created int64
		if err := messageRows.Scan(&id, &created, &data); err != nil {
			messageRows.Close()
			tmp.Close()
			return nil, err
		}
		var value any
		if json.Unmarshal([]byte(data), &value) == nil {
			if object, ok := value.(map[string]any); ok {
				roles[id] = stringValue(object["role"])
			}
			events = append(events, extractEvents(value)...)
		}
		if err := enc.Encode(map[string]any{"record_type": "message", "id": id, "time_created": created, "data": json.RawMessage(data)}); err != nil {
			messageRows.Close()
			tmp.Close()
			return nil, err
		}
	}
	if err := messageRows.Close(); err != nil {
		tmp.Close()
		return nil, err
	}
	partRows, err := database.QueryContext(ctx, "SELECT id, message_id, time_created, data FROM part WHERE session_id=? ORDER BY time_created, id", sessionID)
	if err != nil {
		tmp.Close()
		return nil, err
	}
	for partRows.Next() {
		var id, messageID, data string
		var created int64
		if err := partRows.Scan(&id, &messageID, &created, &data); err != nil {
			partRows.Close()
			tmp.Close()
			return nil, err
		}
		var value any
		if json.Unmarshal([]byte(data), &value) == nil {
			extracted := extractEvents(value)
			for i := range extracted {
				if extracted[i].Role == "" && (extracted[i].Kind == "message" || extracted[i].Kind == "user_message" || extracted[i].Kind == "assistant_message") {
					extracted[i].Role = roles[messageID]
					extracted[i].Kind = roleKind(extracted[i].Role)
				}
			}
			events = append(events, extracted...)
		}
		if err := enc.Encode(map[string]any{"record_type": "part", "id": id, "message_id": messageID, "time_created": created, "data": json.RawMessage(data)}); err != nil {
			partRows.Close()
			tmp.Close()
			return nil, err
		}
	}
	if err := partRows.Close(); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(name, destination); err != nil {
		return nil, err
	}
	sequence(events)
	return events, nil
}

func (s *Service) manifestPath(id string) string {
	return filepath.Join(s.root, "manifests", id+".json")
}

func (s *Service) writeManifest(ctx context.Context, id string) error {
	var manifest Manifest
	manifest.SchemaVersion = 1
	if err := s.db.QueryRowContext(ctx, "SELECT id, agent_type, native_session_id, title, status, adapter_version FROM agent_sessions WHERE id=?", id).
		Scan(&manifest.ID, &manifest.AgentType, &manifest.NativeSessionID, &manifest.Title, &manifest.Status, &manifest.AdapterVersion); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT sandbox_id, sandbox_name, raw_path FROM agent_session_sources WHERE session_id=? ORDER BY sandbox_id, raw_path", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var source ManifestSource
		var rawPath string
		if err := rows.Scan(&source.SandboxID, &source.SandboxName, &rawPath); err != nil {
			return err
		}
		source.RawFile = filepath.Base(rawPath)
		manifest.Sources = append(manifest.Sources, source)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := s.manifestPath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func snapshotRaw(format, source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".raw-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Remove(tmpPath)
	defer os.Remove(tmpPath)
	if format == "opencode-sqlite" {
		db, err := sql.Open("sqlite", source)
		if err != nil {
			return err
		}
		defer db.Close()
		quoted := strings.ReplaceAll(tmpPath, "'", "''")
		if _, err := db.Exec("VACUUM INTO '" + quoted + "'"); err != nil {
			return err
		}
	} else {
		in, err := os.Open(source)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, destination)
}

func normalizeFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if strings.HasSuffix(path, ".jsonl") {
		var events []Event
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
		for scanner.Scan() {
			var value any
			if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
				// A final partial line is expected while an agent is still writing.
				continue
			}
			events = append(events, extractEvents(value)...)
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		sequence(events)
		return events, nil
	}
	var value any
	if err := json.NewDecoder(f).Decode(&value); err != nil {
		return nil, err
	}
	events := extractEvents(value)
	sequence(events)
	return events, nil
}

func sequence(events []Event) {
	for i := range events {
		events[i].Sequence = i + 1
	}
}

func extractEvents(value any) []Event {
	switch v := value.(type) {
	case []any:
		var out []Event
		for _, item := range v {
			out = append(out, extractEvents(item)...)
		}
		return out
	case map[string]any:
		return extractObject(v)
	default:
		return nil
	}
}

func extractObject(v map[string]any) []Event {
	timestamp, _ := v["timestamp"].(string)
	nativeType, _ := v["type"].(string)
	if payload, ok := v["payload"].(map[string]any); ok {
		if _, exists := payload["timestamp"]; !exists {
			payload["timestamp"] = timestamp
		}
		if _, exists := payload["_outer_type"]; !exists {
			payload["_outer_type"] = nativeType
		}
		return extractObject(payload)
	}
	if message, ok := v["message"].(map[string]any); ok {
		if _, exists := message["timestamp"]; !exists {
			message["timestamp"] = timestamp
		}
		return extractObject(message)
	}
	role, _ := v["role"].(string)
	typ, _ := v["type"].(string)
	if typ == "text" && stringValue(v["text"]) != "" {
		return []Event{{Timestamp: timestamp, Kind: roleKind(role), Role: role, Content: stringValue(v["text"]), NativeType: typ}}
	}
	if typ == "tool" {
		state, _ := v["state"].(map[string]any)
		call := Event{Timestamp: timestamp, Kind: "tool_call", ToolName: stringValue(v["tool"]), ToolCallID: firstString(v, "callID", "call_id", "id"), Input: rawValue(state["input"]), Status: stringValue(state["status"]), NativeType: typ}
		out := []Event{call}
		if output, ok := state["output"]; ok {
			out = append(out, Event{Timestamp: timestamp, Kind: "tool_result", ToolCallID: call.ToolCallID, Output: rawValue(output), Status: call.Status, NativeType: typ})
		}
		return out
	}
	if typ == "user" || typ == "assistant" {
		role = typ
	}
	if content, ok := v["content"].(string); ok && (role != "" || content != "") {
		return []Event{{Timestamp: timestamp, Kind: roleKind(role), Role: role, Content: content, NativeType: typ}}
	}
	if contents, ok := v["content"].([]any); ok {
		var out []Event
		for _, content := range contents {
			item, ok := content.(map[string]any)
			if !ok {
				continue
			}
			ct, _ := item["type"].(string)
			switch ct {
			case "text", "input_text", "output_text":
				text, _ := item["text"].(string)
				out = append(out, Event{Timestamp: timestamp, Kind: roleKind(role), Role: role, Content: text, NativeType: ct})
			case "tool_use", "function_call":
				out = append(out, Event{Timestamp: timestamp, Kind: "tool_call", ToolName: stringValue(item["name"]), ToolCallID: firstString(item, "id", "call_id"), Input: rawValue(firstValue(item, "input", "arguments")), NativeType: ct})
			case "tool_result", "function_call_output":
				out = append(out, Event{Timestamp: timestamp, Kind: "tool_result", ToolCallID: firstString(item, "tool_use_id", "call_id"), Output: rawValue(firstValue(item, "content", "output")), NativeType: ct})
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if typ == "function_call" || typ == "tool_use" {
		return []Event{{Timestamp: timestamp, Kind: "tool_call", ToolName: stringValue(v["name"]), ToolCallID: firstString(v, "call_id", "id"), Input: rawValue(firstValue(v, "arguments", "input")), NativeType: typ}}
	}
	if typ == "function_call_output" || typ == "tool_result" {
		return []Event{{Timestamp: timestamp, Kind: "tool_result", ToolCallID: firstString(v, "call_id", "tool_use_id"), Output: rawValue(firstValue(v, "output", "content")), NativeType: typ}}
	}
	for _, key := range []string{"messages", "history", "turns"} {
		if nested, ok := v[key]; ok {
			return extractEvents(nested)
		}
	}
	data, _ := json.Marshal(v)
	return []Event{{Timestamp: timestamp, Kind: "native_event", Content: string(data), NativeType: typ}}
}

func roleKind(role string) string {
	if role == "user" {
		return "user_message"
	}
	if role == "assistant" {
		return "assistant_message"
	}
	return "message"
}
func stringValue(v any) string { s, _ := v.(string); return s }
func firstString(v map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := stringValue(v[k]); s != "" {
			return s
		}
	}
	return ""
}

func firstValue(v map[string]any, keys ...string) any {
	for _, k := range keys {
		if value, ok := v[k]; ok {
			return value
		}
	}
	return nil
}

func rawValue(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	data, _ := json.Marshal(v)
	return data
}

func writeEvents(path string, events []Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".events-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	enc := json.NewEncoder(tmp)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func firstUserMessage(events []Event) string {
	for _, event := range events {
		if event.Kind == "user_message" && event.Content != "" {
			return strings.TrimSpace(event.Content)
		}
	}
	return ""
}

func (s *Service) List(ctx context.Context, agent, sandbox string) ([]sandtypes.AgentSession, error) {
	query := `SELECT s.id, s.agent_type, s.native_session_id, s.title, s.status, s.size_bytes, s.started_at, s.updated_at,
COALESCE(group_concat(DISTINCT src.sandbox_id), ''), COALESCE(group_concat(DISTINCT src.sandbox_name), '')
FROM agent_sessions s JOIN agent_session_sources src ON src.session_id=s.id WHERE 1=1`
	var args []any
	if agent != "" {
		query += " AND s.agent_type=?"
		args = append(args, agent)
	}
	if sandbox != "" {
		query += " AND (src.sandbox_id=? OR src.sandbox_name=?)"
		args = append(args, sandbox, sandbox)
	}
	query += " GROUP BY s.id ORDER BY s.updated_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []sandtypes.AgentSession
	for rows.Next() {
		var item sandtypes.AgentSession
		var ids, names string
		if err := rows.Scan(&item.ID, &item.AgentType, &item.NativeSessionID, &item.Title, &item.Status, &item.SizeBytes, &item.StartedAt, &item.UpdatedAt, &ids, &names); err != nil {
			return nil, err
		}
		if ids != "" {
			item.SandboxIDs = strings.Split(ids, ",")
		}
		if names != "" {
			item.SandboxNames = strings.Split(names, ",")
		}
		sessions = append(sessions, item)
	}
	return sessions, rows.Err()
}

func (s *Service) Resolve(ctx context.Context, id string) (sandtypes.AgentSession, error) {
	items, err := s.List(ctx, "", "")
	if err != nil {
		return sandtypes.AgentSession{}, err
	}
	var matches []sandtypes.AgentSession
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
		if strings.HasPrefix(item.ID, id) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return sandtypes.AgentSession{}, fmt.Errorf("agent session not found: %s", id)
	}
	if len(matches) > 1 {
		return sandtypes.AgentSession{}, fmt.Errorf("agent session ID prefix %q is ambiguous", id)
	}
	return matches[0], nil
}

func (s *Service) Read(ctx context.Context, id, format string, w io.Writer) error {
	item, err := s.Resolve(ctx, id)
	if err != nil {
		return err
	}
	var normalized string
	if err := s.db.QueryRowContext(ctx, "SELECT normalized_path FROM agent_sessions WHERE id=?", item.ID).Scan(&normalized); err != nil {
		return err
	}
	if format == "json" || format == "jsonl" {
		if normalized == "" {
			return fmt.Errorf("session %s has no normalized transcript", item.ID)
		}
		return copyFile(normalized, w)
	}
	if format != "" && format != "text" && format != "markdown" {
		return fmt.Errorf("unknown session format %q", format)
	}
	if normalized == "" {
		return fmt.Errorf("session %s is raw-only; export it with --format raw", item.ID)
	}
	f, err := os.Open(normalized)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return err
		}
		switch event.Kind {
		case "user_message":
			fmt.Fprintf(w, "## User\n\n%s\n\n", event.Content)
		case "assistant_message":
			fmt.Fprintf(w, "## Assistant\n\n%s\n\n", event.Content)
		case "tool_call":
			fmt.Fprintf(w, "### Tool call: %s\n\n```json\n%s\n```\n\n", event.ToolName, event.Input)
		case "tool_result":
			fmt.Fprintf(w, "### Tool result: %s\n\n```json\n%s\n```\n\n", event.ToolCallID, event.Output)
		case "native_event":
			fmt.Fprintf(w, "### Native event\n\n```json\n%s\n```\n\n", event.Content)
		}
	}
	return scanner.Err()
}

func copyFile(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func (s *Service) Export(ctx context.Context, id, format, destination string) error {
	item, err := s.Resolve(ctx, id)
	if err != nil {
		return err
	}
	if format == "" {
		format = "raw"
	}
	if format == "jsonl" || format == "markdown" {
		f, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		return s.Read(ctx, item.ID, map[string]string{"jsonl": "json", "markdown": "markdown"}[format], f)
	}
	if format != "raw" {
		return fmt.Errorf("unknown export format %q", format)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT raw_path FROM agent_session_sources WHERE session_id=? ORDER BY raw_path", item.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	if manifestData, err := os.ReadFile(s.manifestPath(item.ID)); err == nil {
		hdr := &tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifestData)), ModTime: time.Now()}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(manifestData); err != nil {
			return err
		}
	}
	index := 0
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		index++
		hdr, _ := tar.FileInfoHeader(info, "")
		hdr.Name = fmt.Sprintf("raw/%d-%s", index, filepath.Base(path))
		if err := tw.WriteHeader(hdr); err != nil {
			f.Close()
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return rows.Err()
}

func (s *Service) Delete(ctx context.Context, id string) error {
	item, err := s.Resolve(ctx, id)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT native_path, raw_path FROM agent_session_sources WHERE session_id=?", item.ID)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var nativePath, path string
		if err := rows.Scan(&nativePath, &path); err != nil {
			rows.Close()
			return err
		}
		paths = append(paths, nativePath, path)
	}
	rows.Close()
	var active int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM agent_session_launches l JOIN agent_session_sources src ON src.sandbox_id=l.sandbox_id WHERE src.session_id=? AND l.ended_at IS NULL`, item.ID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("session %s belongs to an active agent launch", item.ID)
	}
	root, _ := filepath.Abs(s.root)
	for _, path := range paths {
		absolute, _ := filepath.Abs(path)
		if absolute == root || !strings.HasPrefix(absolute, root+string(filepath.Separator)) {
			return fmt.Errorf("refusing to remove archive path outside %s", root)
		}
		if filepath.Ext(path) == ".db" {
			database, err := sql.Open("sqlite", path)
			if err != nil {
				return err
			}
			_, _ = database.Exec("PRAGMA foreign_keys=ON")
			_, err = database.ExecContext(ctx, "DELETE FROM session WHERE id=?", item.NativeSessionID)
			database.Close()
			if err != nil {
				return err
			}
		} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	var normalized string
	_ = s.db.QueryRowContext(ctx, "SELECT normalized_path FROM agent_sessions WHERE id=?", item.ID).Scan(&normalized)
	if normalized != "" {
		_ = os.Remove(normalized)
	}
	_ = os.Remove(s.manifestPath(item.ID))
	_, err = s.db.ExecContext(ctx, "DELETE FROM agent_session_sources WHERE session_id=?", item.ID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM agent_sessions WHERE id=?", item.ID)
	return err
}

func FormatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

var _ = time.Time{}
