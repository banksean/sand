package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/profiles"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/posener/complete"
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

func mergeEnv(envs ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, env := range envs {
		for key, value := range env {
			merged[key] = value
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

type plainCommandEnv struct {
	EnvFile string
	Env     map[string]string
	cleanup func()
}

func (e plainCommandEnv) Cleanup() {
	if e.cleanup != nil {
		e.cleanup()
	}
}

func plainCommandProjectEnv(sbox *sandtypes.Box, includeProjectEnv bool) (plainCommandEnv, error) {
	if !includeProjectEnv || sbox == nil {
		return plainCommandEnv{}, nil
	}

	policy, configured, err := selectedProfileEnvPolicy(sbox)
	if err != nil {
		return plainCommandEnv{}, err
	}
	if !configured {
		return plainCommandEnv{EnvFile: sbox.EnvFile}, nil
	}

	envFile, cleanup, err := projectEnvFile(policy.Files)
	if err != nil {
		return plainCommandEnv{}, err
	}
	return plainCommandEnv{
		EnvFile: envFile,
		Env:     projectEnvVars(policy.Vars),
		cleanup: cleanup,
	}, nil
}

func resolveAgentLaunchEnv(ctx context.Context, mc daemon.Client, agent string, sbox *sandtypes.Box) (map[string]string, error) {
	opts := daemon.ResolveAgentLaunchEnvOpts{Agent: agent}
	if sbox != nil {
		opts.EnvFile = sbox.EnvFile
		opts.ProfileName = sbox.ProfileName
		profileEnv, profileEnvConfigured, err := selectedProfileEnvPolicy(sbox)
		if err != nil {
			return nil, err
		}
		opts.ProfileEnv = profileEnv
		opts.ProfileEnvConfigured = profileEnvConfigured
	}
	return mc.ResolveAgentLaunchEnv(ctx, opts)
}

func selectedProfileEnvPolicy(sbox *sandtypes.Box) (sandtypes.EnvPolicy, bool, error) {
	if sbox == nil {
		return sandtypes.EnvPolicy{}, false, nil
	}
	profileName := sbox.ProfileName
	if profileName == "" {
		profileName = sandtypes.DefaultProfileName
	}
	cfg, err := profiles.LoadConfigForDir(sbox.HostOriginDir)
	if err != nil {
		return sandtypes.EnvPolicy{}, false, err
	}
	profile, ok := cfg.Profiles[profileName]
	if !ok {
		if profileName == sandtypes.DefaultProfileName {
			return sandtypes.EnvPolicy{}, false, nil
		}
		return sandtypes.EnvPolicy{}, false, fmt.Errorf("profile %q not found", profileName)
	}
	return resolveEnvPolicyPaths(profile.Env, sbox.HostOriginDir), true, nil
}

func resolveEnvPolicyPaths(policy sandtypes.EnvPolicy, baseDir string) sandtypes.EnvPolicy {
	if baseDir == "" {
		return policy
	}
	for i := range policy.Files {
		path := policy.Files[i].Path
		if path != "" && !filepath.IsAbs(path) {
			policy.Files[i].Path = filepath.Join(baseDir, path)
		}
	}
	return policy
}

func projectEnvFile(files []sandtypes.EnvFileRef) (string, func(), error) {
	var paths []string
	for _, file := range files {
		if !envScopeAllowsProject(file.Scope) || strings.TrimSpace(file.Path) == "" {
			continue
		}
		if info, err := os.Stat(file.Path); err == nil && !info.IsDir() {
			paths = append(paths, file.Path)
		}
	}
	if len(paths) == 0 {
		return "", nil, nil
	}
	if len(paths) == 1 {
		return paths[0], nil, nil
	}

	tmp, err := os.CreateTemp("", "sand-project-env-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating project env file: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	for _, path := range paths {
		if err := appendEnvFile(tmp, path); err != nil {
			_ = tmp.Close()
			cleanup()
			return "", nil, err
		}
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("closing project env file: %w", err)
	}
	return tmp.Name(), cleanup, nil
}

func appendEnvFile(dst *os.File, path string) error {
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reading project env file %q: %w", path, err)
	}
	defer src.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copying project env file %q: %w", path, err)
	}
	if _, err := dst.WriteString("\n"); err != nil {
		return fmt.Errorf("writing project env file: %w", err)
	}
	return nil
}

func projectEnvVars(vars []sandtypes.EnvVarRule) map[string]string {
	env := map[string]string{}
	for _, variable := range vars {
		if !envScopeAllowsProject(variable.Scope) || strings.TrimSpace(variable.Name) == "" {
			continue
		}
		if value, ok := os.LookupEnv(variable.Name); ok && strings.TrimSpace(value) != "" {
			env[variable.Name] = value
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

func envScopeAllowsProject(scope sandtypes.EnvScope) bool {
	return scope == sandtypes.EnvScopeProject || scope == sandtypes.EnvScopeAll
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
