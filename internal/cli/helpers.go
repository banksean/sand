package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/banksean/sand/internal/daemon"
	"github.com/banksean/sand/internal/profiles"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/sshimmer"
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
	profile, configured, err := selectedProfile(sbox)
	if err != nil || !configured {
		return sandtypes.EnvPolicy{}, configured, err
	}
	return profile.Env, true, nil
}

func selectedProfile(sbox *sandtypes.Box) (sandtypes.Profile, bool, error) {
	if sbox == nil {
		return sandtypes.Profile{}, false, nil
	}
	profileName := sbox.ProfileName
	if profileName == "" {
		profileName = sandtypes.DefaultProfileName
	}
	cfg, err := profiles.LoadConfigForDir(sbox.HostOriginDir)
	if err != nil {
		return sandtypes.Profile{}, false, err
	}
	profile, ok := cfg.Profiles[profileName]
	if !ok {
		if profileName == sandtypes.DefaultProfileName {
			return sandtypes.Profile{}, false, nil
		}
		return sandtypes.Profile{}, false, fmt.Errorf("profile %q not found", profileName)
	}
	return resolveProfilePaths(profile, sbox.HostOriginDir), true, nil
}

func resolveProfilePaths(profile sandtypes.Profile, baseDir string) sandtypes.Profile {
	profile.Env = resolveEnvPolicyPaths(profile.Env, baseDir)
	profile.Dotfiles = resolveDotfilePolicyPaths(profile.Dotfiles, baseDir)
	if baseDir != "" && profile.Network.AllowedDomainsFile != "" && !filepath.IsAbs(profile.Network.AllowedDomainsFile) {
		profile.Network.AllowedDomainsFile = filepath.Join(baseDir, profile.Network.AllowedDomainsFile)
	}
	return profile
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

func resolveDotfilePolicyPaths(policy sandtypes.DotfilePolicy, baseDir string) sandtypes.DotfilePolicy {
	if baseDir == "" {
		return policy
	}
	for i := range policy.Files {
		source := policy.Files[i].Source
		if source != "" && !filepath.IsAbs(source) && !strings.HasPrefix(source, "~") {
			policy.Files[i].Source = filepath.Join(baseDir, source)
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

var (
	sshCommand           = exec.CommandContext
	checkSSHReachability = sshimmer.CheckSSHReachability
)

// runShell executes an interactive shell or command in sbox's container over SSH,
// connecting the current process's stdin/stdout/stderr. Non-zero shell exit is
// logged but not returned as an error — an interactive session ending with a
// non-zero code is not a CLI failure.
func runShell(ctx context.Context, sbox *sandtypes.Box, shell string, args []string, scrubSSHAgent bool, envFile string, extraEnv map[string]string) error {
	if sbox.Container == nil {
		return fmt.Errorf("sandbox %s has no container", sbox.ID)
	}
	hostname := sandtypes.GetContainerHostname(sbox.Container)
	if err := ensureSSHReachability(ctx, hostname); err != nil {
		return err
	}

	env, err := interactiveSSHEnv(hostname, scrubSSHAgent, envFile, extraEnv)
	if err != nil {
		return err
	}
	cmd := sshStreamCommand(ctx, hostname, true, env, shell, args)
	slog.InfoContext(ctx, "runShell: ssh", "sandbox", sbox.ID, "hostname", hostname, "shell", shell)
	if err := cmd.Run(); err != nil {
		slog.WarnContext(ctx, "runShell: shell exited with error", "sandbox", sbox.ID, "error", err)
	}
	return nil
}

func runSSHOutput(ctx context.Context, sbox *sandtypes.Box, envFile string, extraEnv map[string]string, shell string, args ...string) (string, error) {
	if sbox.Container == nil {
		return "", fmt.Errorf("sandbox %s has no container", sbox.ID)
	}
	hostname := sandtypes.GetContainerHostname(sbox.Container)
	if err := ensureSSHReachability(ctx, hostname); err != nil {
		return "", err
	}
	env, err := sshCommandEnv(hostname, envFile, extraEnv)
	if err != nil {
		return "", err
	}
	cmd := sshOutputCommand(ctx, hostname, env, shell, args)
	slog.InfoContext(ctx, "runSSHOutput: ssh", "sandbox", sbox.ID, "hostname", hostname, "shell", shell)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runSSHStream(ctx context.Context, sbox *sandtypes.Box, tty bool, envFile string, extraEnv map[string]string, shell string, args ...string) error {
	if sbox.Container == nil {
		return fmt.Errorf("sandbox %s has no container", sbox.ID)
	}
	hostname := sandtypes.GetContainerHostname(sbox.Container)
	if err := ensureSSHReachability(ctx, hostname); err != nil {
		return err
	}
	env, err := interactiveSSHEnv(hostname, true, envFile, extraEnv)
	if err != nil {
		return err
	}
	cmd := sshStreamCommand(ctx, hostname, tty, env, shell, args)
	slog.InfoContext(ctx, "runSSHStream: ssh", "sandbox", sbox.ID, "hostname", hostname, "shell", shell, "tty", tty)
	return cmd.Run()
}

func sshOutputCommand(ctx context.Context, hostname string, env map[string]string, shell string, args []string) *exec.Cmd {
	return sshCommand(ctx, "ssh", hostname, remoteInteractiveCommand(env, shell, args))
}

func sshStreamCommand(ctx context.Context, hostname string, tty bool, env map[string]string, shell string, args []string) *exec.Cmd {
	sshArgs := []string{}
	if tty {
		sshArgs = append(sshArgs, "-tt")
	}
	sshArgs = append(sshArgs, hostname, remoteInteractiveCommand(env, shell, args))
	cmd := sshCommand(ctx, "ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func ensureSSHReachability(ctx context.Context, hostname string) error {
	updateSSHConfFunc, err := checkSSHReachability(ctx, hostname)
	if err != nil {
		slog.ErrorContext(ctx, "sshimmer.CheckSSHReachability", "error", err)
	}
	if updateSSHConfFunc == nil {
		return nil
	}
	if os.Getenv("SMOKE_TEST") == "" {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("\nTo enable you to use ssh to connect to local sand containers, we need to add one line to the top of your ssh config. Proceed [y/N]? ")
		text, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("couldn't read from stdin: %w", err)
		}
		text = strings.TrimSpace(strings.ToLower(text))
		if text != "y" {
			return fmt.Errorf("user declined to edit ssh config file")
		}
	}
	if err := updateSSHConfFunc(); err != nil {
		return err
	}
	return nil
}

func interactiveSSHEnv(hostname string, scrubSSHAgent bool, envFile string, extraEnv map[string]string) (map[string]string, error) {
	env, err := sshCommandEnv(hostname, envFile, extraEnv)
	if err != nil {
		return nil, err
	}
	for key, value := range buildInteractiveEnv(hostname, scrubSSHAgent, nil) {
		env[key] = value
	}
	return env, nil
}

func sshCommandEnv(hostname string, envFile string, extraEnv map[string]string) (map[string]string, error) {
	env := map[string]string{}
	fileEnv, err := readEnvFile(envFile)
	if err != nil {
		return nil, err
	}
	for key, value := range fileEnv {
		env[key] = value
	}
	env["HOSTNAME"] = hostname
	for key, value := range extraEnv {
		env[key] = value
	}
	return env, nil
}

func readEnvFile(path string) (map[string]string, error) {
	env := map[string]string{}
	if strings.TrimSpace(path) == "" {
		return env, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return env, nil
		}
		return nil, fmt.Errorf("reading env file %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		env[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading env file %s: %w", path, err)
	}
	return env, nil
}

func remoteInteractiveCommand(env map[string]string, shell string, args []string) string {
	parts := []string{"cd", shellQuote("/app"), "&&", "env"}
	keys := make([]string, 0, len(env))
	for key := range env {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, shellQuote(key+"="+env[key]))
	}
	parts = append(parts, shellQuote(shell))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
