package sandboxlog

import (
	"context"
	"log/slog"
	"slices"
)

// NewContextHandler injects sandbox_id from context into slog records.
func NewContextHandler(next slog.Handler) slog.Handler {
	return &contextHandler{next: next}
}

type contextHandler struct {
	next   slog.Handler
	attrs  []slog.Attr
	groups []string
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	sandboxID, ok := SandboxIDFromContext(ctx)
	if ok && !h.hasSandboxID(rec) {
		rec = rec.Clone()
		rec.AddAttrs(slog.String(SandboxIDAttrKey, sandboxID))
	}
	return h.next.Handle(ctx, rec)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := append(slices.Clip(h.attrs), attrs...)
	return &contextHandler{
		next:   h.next.WithAttrs(attrs),
		attrs:  merged,
		groups: slices.Clip(h.groups),
	}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	groups := append(slices.Clip(h.groups), name)
	return &contextHandler{
		next:   h.next.WithGroup(name),
		attrs:  slices.Clip(h.attrs),
		groups: groups,
	}
}

func (h *contextHandler) hasSandboxID(rec slog.Record) bool {
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
	return len(ids) > 0
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
	if attr.Key == SandboxIDAttrKey {
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
