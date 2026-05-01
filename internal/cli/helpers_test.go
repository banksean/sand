package cli

import (
	"testing"

	"github.com/banksean/sand/internal/sandtypes"
)

func TestBuildInteractiveEnv(t *testing.T) {
	t.Run("default shell env", func(t *testing.T) {
		env := buildInteractiveEnv("sandbox.local", false, nil)
		if env["HOSTNAME"] != "sandbox.local" {
			t.Fatalf("HOSTNAME = %q, want sandbox.local", env["HOSTNAME"])
		}
		if _, ok := env["SSH_AUTH_SOCK"]; ok {
			t.Fatal("SSH_AUTH_SOCK unexpectedly set for normal interactive shell")
		}
		if _, ok := env["SSH_AGENT_PID"]; ok {
			t.Fatal("SSH_AGENT_PID unexpectedly set for normal interactive shell")
		}
	})

	t.Run("agent shell scrubs ssh agent vars", func(t *testing.T) {
		env := buildInteractiveEnv("sandbox.local", true, map[string]string{"OPENAI_API_KEY": "sk-test"})
		if env["SSH_AUTH_SOCK"] != "" {
			t.Fatalf("SSH_AUTH_SOCK = %q, want empty string", env["SSH_AUTH_SOCK"])
		}
		if env["SSH_AGENT_PID"] != "" {
			t.Fatalf("SSH_AGENT_PID = %q, want empty string", env["SSH_AGENT_PID"])
		}
		if env["OPENAI_API_KEY"] != "sk-test" {
			t.Fatalf("OPENAI_API_KEY = %q, want %q", env["OPENAI_API_KEY"], "sk-test")
		}
	})
}

func TestPlainCommandEnvFile(t *testing.T) {
	sbox := &sandtypes.Box{EnvFile: "/tmp/project.env"}

	if got := plainCommandEnvFile(sbox, false); got != "" {
		t.Fatalf("plainCommandEnvFile(false) = %q, want empty", got)
	}
	if got := plainCommandEnvFile(sbox, true); got != sbox.EnvFile {
		t.Fatalf("plainCommandEnvFile(true) = %q, want %q", got, sbox.EnvFile)
	}
}
