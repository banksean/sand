package sandboxlog

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactionHandlerRedactsCommandSecrets(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewRedactionHandler(slog.NewJSONHandler(&out, nil)))

	logger.Info("exec",
		"cmd", "container exec --env OPENAI_API_KEY=sk-test --env-file /repo/.env --token abc123 sandbox sh",
		"output", "ANTHROPIC_API_KEY=secret-value",
	)

	got := out.String()
	assertContains(t, got, "OPENAI_API_KEY="+redactedValue)
	assertContains(t, got, "--env-file "+redactedValue)
	assertContains(t, got, "--token "+redactedValue)
	assertContains(t, got, "ANTHROPIC_API_KEY="+redactedValue)
	assertNotContains(t, got, "sk-test")
	assertNotContains(t, got, "/repo/.env")
	assertNotContains(t, got, "abc123")
	assertNotContains(t, got, "secret-value")
}

func TestRedactionHandlerRedactsWithAttrs(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewRedactionHandler(slog.NewJSONHandler(&out, nil))).With(
		"cmd", "container create --env ANTHROPIC_API_KEY=anthropic-secret image",
	)

	logger.Info("create")

	got := out.String()
	assertContains(t, got, "ANTHROPIC_API_KEY="+redactedValue)
	assertNotContains(t, got, "anthropic-secret")
}

func TestRedactionHandlerRedactsCompositeValues(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewRedactionHandler(slog.NewJSONHandler(&out, nil)))

	type createOpts struct {
		ID      string `json:"id"`
		EnvFile string `json:"envFile"`
		Env     map[string]string
		Nested  struct {
			AccessToken string
			Plain       string
		}
	}

	opts := createOpts{
		ID:      "sand-1",
		EnvFile: "/repo/.env",
		Env: map[string]string{
			"OPENAI_API_KEY": "sk-test",
		},
	}
	opts.Nested.AccessToken = "nested-secret"
	opts.Nested.Plain = "plain-value"

	logger.InfoContext(context.Background(), "create", "opts", opts)

	got := out.String()
	assertContains(t, got, redactedValue)
	assertContains(t, got, "sand-1")
	assertContains(t, got, "plain-value")
	assertNotContains(t, got, "/repo/.env")
	assertNotContains(t, got, "sk-test")
	assertNotContains(t, got, "nested-secret")
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("log output does not contain %q:\n%s", want, got)
	}
}

func assertNotContains(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Fatalf("log output contains %q:\n%s", want, got)
	}
}
