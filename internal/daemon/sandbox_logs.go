package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/banksean/sand/internal/daemon/internal/boxer"
)

const sandboxIDAttrKey = boxer.SandboxIDAttrKey

// TODO: clean up all the logging calls that include a sandbox ID, but
// first ...just prune all the noisy calls in there now.
const legacySandboxAttrKey = "sandbox"

func SandboxLogsDir(logFile string) string {
	if logFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(logFile), "sandboxes")
}

func sandboxLogPath(logFile, sandboxID string) string {
	return filepath.Join(SandboxLogsDir(logFile), sandboxID+".log")
}

type sandboxFanoutManager struct {
	dir      string
	opts     slog.HandlerOptions
	mu       sync.Mutex
	handlers map[string]slog.Handler
	files    map[string]*os.File
}

func NewSandboxFanoutHandler(next slog.Handler, dir string, opts *slog.HandlerOptions) (slog.Handler, error) {
	if dir == "" {
		return next, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	manager := &sandboxFanoutManager{
		dir:      dir,
		handlers: map[string]slog.Handler{},
		files:    map[string]*os.File{},
	}

	if opts != nil {
		manager.opts = *opts
	}
	return &sandboxFanoutHandler{
		next:    next,
		manager: manager,
	}, nil
}

func (m *sandboxFanoutManager) handlerFor(sandboxID string) (slog.Handler, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if h, ok := m.handlers[sandboxID]; ok {
		return h, nil
	}

	path := filepath.Join(m.dir, sandboxID+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	h := slog.NewJSONHandler(f, &m.opts)
	m.files[sandboxID] = f
	m.handlers[sandboxID] = h
	return h, nil
}

type sandboxFanoutHandler struct {
	next    slog.Handler
	manager *sandboxFanoutManager
	attrs   []slog.Attr
	groups  []string
}

func (h *sandboxFanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *sandboxFanoutHandler) Handle(ctx context.Context, rec slog.Record) error {
	fanoutRec := rec.Clone()
	err := h.next.Handle(ctx, rec)

	sandboxIDs := h.sandboxIDs(fanoutRec)
	if len(sandboxIDs) == 0 {
		return err
	}

	var fanoutErrs []error
	for _, sandboxID := range sandboxIDs {
		handler, handlerErr := h.manager.handlerFor(sandboxID)
		if handlerErr != nil {
			fanoutErrs = append(fanoutErrs, handlerErr)
			continue
		}
		if handlerErr := handler.Handle(ctx, fanoutRec.Clone()); handlerErr != nil {
			fanoutErrs = append(fanoutErrs, handlerErr)
		}
	}

	return errors.Join(append([]error{err}, fanoutErrs...)...)
}

func (h *sandboxFanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := append(slices.Clip(h.attrs), attrs...)
	return &sandboxFanoutHandler{
		next:    h.next.WithAttrs(attrs),
		manager: h.manager,
		attrs:   merged,
		groups:  slices.Clip(h.groups),
	}
}

func (h *sandboxFanoutHandler) WithGroup(name string) slog.Handler {
	groups := append(slices.Clip(h.groups), name)
	return &sandboxFanoutHandler{
		next:    h.next.WithGroup(name),
		manager: h.manager,
		attrs:   slices.Clip(h.attrs),
		groups:  groups,
	}
}

func (h *sandboxFanoutHandler) sandboxIDs(rec slog.Record) []string {
	ids := map[string]struct{}{}
	for _, attr := range applyGroups(h.groups, h.attrs) {
		collectSandboxIDs(ids, attr)
	}
	var recordAttrs []slog.Attr
	rec.Attrs(func(attr slog.Attr) bool {
		recordAttrs = append(recordAttrs, attr)
		return true
	})
	for _, attr := range applyGroups(h.groups, recordAttrs) {
		collectSandboxIDs(ids, attr)
	}
	return sortedKeys(ids)
}

func applyGroups(groups []string, attrs []slog.Attr) []slog.Attr {
	if len(groups) == 0 || len(attrs) == 0 {
		return attrs
	}
	grouped := slices.Clip(attrs)
	for i := len(groups) - 1; i >= 0; i-- {
		grouped = []slog.Attr{{Key: groups[i], Value: slog.GroupValue(grouped...)}}
	}
	return grouped
}

func collectSandboxIDs(ids map[string]struct{}, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Key == sandboxIDAttrKey || attr.Key == legacySandboxAttrKey {
		if sandboxID := attr.Value.String(); sandboxID != "" {
			ids[sandboxID] = struct{}{}
		}
		return
	}
	if attr.Value.Kind() != slog.KindGroup {
		return
	}
	for _, nested := range attr.Value.Group() {
		collectSandboxIDs(ids, nested)
	}
}

func sortedKeys(ids map[string]struct{}) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func copySandboxLog(logFile, sandboxID string, w io.Writer) error {
	path := sandboxLogPath(logFile, sandboxID)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no logs found for sandbox %q", sandboxID)
		}
		return err
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}
