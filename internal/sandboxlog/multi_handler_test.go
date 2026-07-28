package sandboxlog

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestMultiHandlerFansOutToAllHandlers(t *testing.T) {
	var a, b bytes.Buffer
	logger := slog.New(NewMultiHandler(slog.NewJSONHandler(&a, nil), slog.NewJSONHandler(&b, nil)))

	logger.Info("hello", "key", "value")

	assertContains(t, a.String(), `"msg":"hello"`)
	assertContains(t, a.String(), `"key":"value"`)
	assertContains(t, b.String(), `"msg":"hello"`)
	assertContains(t, b.String(), `"key":"value"`)
}

func TestMultiHandlerWithAttrsAppliesToAllHandlers(t *testing.T) {
	var a, b bytes.Buffer
	logger := slog.New(NewMultiHandler(slog.NewJSONHandler(&a, nil), slog.NewJSONHandler(&b, nil))).With("shared", "attr")

	logger.Info("hello")

	assertContains(t, a.String(), `"shared":"attr"`)
	assertContains(t, b.String(), `"shared":"attr"`)
}

func TestMultiHandlerSingleHandlerIsUnwrapped(t *testing.T) {
	h := slog.NewJSONHandler(&bytes.Buffer{}, nil)
	if got := NewMultiHandler(h); got != slog.Handler(h) {
		t.Fatalf("expected single handler to be returned unwrapped")
	}
}
