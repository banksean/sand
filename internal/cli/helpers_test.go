package cli

import "testing"

func TestBuildInteractiveEnv(t *testing.T) {
	t.Run("default shell env", func(t *testing.T) {
		env := buildInteractiveEnv("sandbox.local", false)
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
		env := buildInteractiveEnv("sandbox.local", true)
		if env["SSH_AUTH_SOCK"] != "" {
			t.Fatalf("SSH_AUTH_SOCK = %q, want empty string", env["SSH_AUTH_SOCK"])
		}
		if env["SSH_AGENT_PID"] != "" {
			t.Fatalf("SSH_AGENT_PID = %q, want empty string", env["SSH_AGENT_PID"])
		}
	})
}
