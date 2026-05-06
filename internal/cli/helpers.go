package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/posener/complete"

	"github.com/banksean/sand/internal/daemon"
)

type sandboxNamePredictor struct {
	mc daemon.Client
}

// Predict implements [complete.Predictor].
func (s *sandboxNamePredictor) Predict(args complete.Args) []string {
	sandboxes, err := s.mc.ListSandboxes(context.Background())
	if err != nil {
		return nil
	}
	ret := []string{}
	for _, box := range sandboxes {
		ret = append(ret, box.Name)
	}
	return ret
}

func NewSandboxNamePredictor(mc daemon.Client) complete.Predictor {
	return &sandboxNamePredictor{mc: mc}
}

func buildInteractiveEnv(hostname string, scrubSSHAgent bool, extraEnv map[string]string) map[string]string {
	env := map[string]string{
		"HOSTNAME": hostname,
		"LANG":     os.Getenv("LANG"),
		"TERM":     os.Getenv("TERM"),
	}
	for key, value := range extraEnv {
		env[key] = value
	}
	if scrubSSHAgent {
		// Clear the standard ssh-agent variables for the initial agent process.
		// The container may still have access to the forwarded socket, but the
		// launched command does not receive it by default.
		env["SSH_AUTH_SOCK"] = ""
		env["SSH_AGENT_PID"] = ""
	}
	return env
}

func plainCommandEnvFile(sbox *sandtypes.Box, includeProjectEnv bool) string {
	if !includeProjectEnv || sbox == nil {
		return ""
	}
	return sbox.EnvFile
}

// runShell executes an interactive shell or command in sbox's container,
// connecting the current process's stdin/stdout/stderr. shell and args are
// passed directly to ExecStream. Non-zero shell exit is logged but not
// returned as an error — an interactive session ending with a non-zero code
// is not a CLI failure.
func runShell(ctx context.Context, sbox *sandtypes.Box, shell string, args []string, scrubSSHAgent bool, envFile string, extraEnv map[string]string) error {
	if sbox.Container == nil {
		return fmt.Errorf("sandbox %s has no container", sbox.ID)
	}
	hostname := types.GetContainerHostname(sbox.Container)
	containerSvc := hostops.NewAppleContainerOps()
	wait, err := containerSvc.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				WorkDir:     "/app",
				Env:         buildInteractiveEnv(hostname, scrubSSHAgent, extraEnv),
				EnvFile:     envFile,
				User:        sbox.Username,
				UID:         sbox.Uid,
			},
		}, sbox.ContainerID, shell, os.Environ(), os.Stdin, os.Stdout, os.Stderr, args...)
	if err != nil {
		slog.ErrorContext(ctx, "runShell: ExecStream", "sandbox", sbox.ID, "error", err)
		return fmt.Errorf("failed to execute shell in sandbox %s: %w", sbox.ID, err)
	}
	if err := wait(); err != nil {
		slog.WarnContext(ctx, "runShell: shell exited with error", "sandbox", sbox.ID, "error", err)
	}
	return nil
}
