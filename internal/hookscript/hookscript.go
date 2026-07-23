package hookscript

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/banksean/sand/internal/sandtypes"
	"rsc.io/script"
)

const (
	bazelrcManagedStart = "# sand bazel remote cache start"
	bazelrcManagedEnd   = "# sand bazel remote cache end"
	npmAgentNodeVersion = "22.23.1"
	nodeDownloadBaseURL = "https://nodejs.org/download/release"
)

// Execute runs a small container-oriented script against exec. The engine is
// intentionally limited to commands that route through HookStreamer.
func Execute(ctx context.Context, exec sandtypes.HookStreamer, name, body string, log io.Writer) error {
	engine := NewEngine(exec)
	state, err := script.NewState(ctx, "/app", nil)
	if err != nil {
		return err
	}
	defer state.CloseAndWait(io.Discard) //nolint:errcheck
	return engine.Execute(state, name, bufio.NewReader(strings.NewReader(body)), log)
}

func NewEngine(exec sandtypes.HookStreamer) *script.Engine {
	return &script.Engine{
		Cmds: map[string]script.Cmd{
			"exec":                   execCmd(exec),
			"stream":                 streamCmd(exec),
			"write-managed-bazelrc":  writeManagedBazelrcCmd(exec),
			"write-http-proxy-env":   writeHTTPProxyEnvCmd(exec),
			"install-npm-agent":      installNPMAgentCmd(exec),
			"install-opencode-agent": installOpenCodeAgentCmd(exec),
		},
		Conds: map[string]script.Cond{
			"cmd":    commandExistsCond(exec),
			"exists": pathExistsCond(exec),
		},
	}
}

func execCmd(exec sandtypes.HookStreamer) script.Cmd {
	return script.Command(script.CmdUsage{Summary: "execute a command in the container", Args: "cmd [args...]"}, func(s *script.State, args ...string) (script.WaitFunc, error) {
		if len(args) == 0 {
			return nil, script.ErrUsage
		}
		out, err := exec.Exec(s.Context(), args[0], args[1:]...)
		return func(*script.State) (string, string, error) {
			return out, "", err
		}, nil
	})
}

func streamCmd(exec sandtypes.HookStreamer) script.Cmd {
	return script.Command(script.CmdUsage{Summary: "stream a command in the container", Args: "cmd [args...]"}, func(s *script.State, args ...string) (script.WaitFunc, error) {
		if len(args) == 0 {
			return nil, script.ErrUsage
		}
		var stdout, stderr bytes.Buffer
		err := exec.ExecStream(s.Context(), &stdout, &stderr, args[0], args[1:]...)
		return func(*script.State) (string, string, error) {
			return stdout.String(), stderr.String(), err
		}, nil
	})
}

func writeManagedBazelrcCmd(exec sandtypes.HookStreamer) script.Cmd {
	return script.Command(script.CmdUsage{Summary: "replace sand managed bazelrc block", Args: "path remote-cache-url"}, func(s *script.State, args ...string) (script.WaitFunc, error) {
		if len(args) != 2 {
			return nil, script.ErrUsage
		}
		err := writeManagedBazelrc(s.Context(), exec, args[0], args[1])
		return func(*script.State) (string, string, error) {
			return "", "", err
		}, nil
	})
}

func writeHTTPProxyEnvCmd(exec sandtypes.HookStreamer) script.Cmd {
	return script.Command(script.CmdUsage{Summary: "write shared HTTP proxy environment", Args: "proxy-url [ca-cert-path]"}, func(s *script.State, args ...string) (script.WaitFunc, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, script.ErrUsage
		}
		caCertPath := ""
		if len(args) == 2 {
			caCertPath = args[1]
		}
		err := writeHTTPProxyEnv(s.Context(), exec, args[0], caCertPath)
		return func(*script.State) (string, string, error) {
			return "", "", err
		}, nil
	})
}

func installNPMAgentCmd(exec sandtypes.HookStreamer) script.Cmd {
	return script.Command(script.CmdUsage{Summary: "install an npm-backed agent if missing", Args: "command package version"}, func(s *script.State, args ...string) (script.WaitFunc, error) {
		if len(args) != 3 {
			return nil, script.ErrUsage
		}
		err := installNPMAgent(s.Context(), exec, args[0], args[1], args[2])
		return func(*script.State) (string, string, error) {
			return "", "", err
		}, nil
	})
}

func installOpenCodeAgentCmd(exec sandtypes.HookStreamer) script.Cmd {
	return script.Command(script.CmdUsage{Summary: "install opencode if missing", Args: "command version"}, func(s *script.State, args ...string) (script.WaitFunc, error) {
		if len(args) != 2 {
			return nil, script.ErrUsage
		}
		err := installOpenCodeAgent(s.Context(), exec, args[0], args[1])
		return func(*script.State) (string, string, error) {
			return "", "", err
		}, nil
	})
}

func commandExistsCond(exec sandtypes.HookStreamer) script.Cond {
	return script.PrefixCondition("command exists in container", func(s *script.State, suffix string) (bool, error) {
		if suffix == "" {
			return false, script.ErrUsage
		}
		_, err := exec.Exec(s.Context(), "which", suffix)
		return err == nil, nil
	})
}

func pathExistsCond(exec sandtypes.HookStreamer) script.Cond {
	return script.PrefixCondition("path exists in container", func(s *script.State, suffix string) (bool, error) {
		if suffix == "" {
			return false, script.ErrUsage
		}
		_, err := exec.Exec(s.Context(), "test", "-e", suffix)
		return err == nil, nil
	})
}

func writeManagedBazelrc(ctx context.Context, exec sandtypes.HookStreamer, filename, remoteCacheURL string) error {
	current, err := exec.Exec(ctx, "cat", filename)
	if err != nil {
		current = ""
	}
	block := fmt.Sprintf("%s\nbuild --remote_cache=%s\nbuild --experimental_guard_against_concurrent_changes\n%s\n",
		bazelrcManagedStart, remoteCacheURL, bazelrcManagedEnd)
	next := stripManagedBlock(current) + block

	dir := path.Dir(filename)
	if _, err := exec.Exec(ctx, "mkdir", "-p", dir); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp := filename + ".sand.tmp"
	if err := exec.ExecStreamInput(ctx, strings.NewReader(next), io.Discard, io.Discard, "tee", tmp); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if _, err := exec.Exec(ctx, "mv", tmp, filename); err != nil {
		return fmt.Errorf("mv %s %s: %w", tmp, filename, err)
	}
	return nil
}

func stripManagedBlock(s string) string {
	lines := strings.SplitAfter(s, "\n")
	var out strings.Builder
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSuffix(line, "\n")
		switch {
		case trimmed == bazelrcManagedStart:
			inBlock = true
		case trimmed == bazelrcManagedEnd && inBlock:
			inBlock = false
		case !inBlock:
			out.WriteString(line)
		}
	}
	return out.String()
}

func writeHTTPProxyEnv(ctx context.Context, exec sandtypes.HookStreamer, proxyURL, caCertPath string) error {
	env := sandtypes.SharedHTTPProxyEnv(proxyURL)
	if len(env) == 0 {
		return nil
	}
	if caCertPath != "" {
		if err := installHTTPProxyCA(ctx, exec, caCertPath); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var profile strings.Builder
	profile.WriteString("# sand shared HTTP proxy cache\n")
	for _, key := range keys {
		profile.WriteString("export ")
		profile.WriteString(key)
		profile.WriteString("=")
		profile.WriteString(shellQuote(env[key]))
		profile.WriteString("\n")
	}

	if _, err := exec.Exec(ctx, "mkdir", "-p", "/etc/profile.d"); err != nil {
		return fmt.Errorf("mkdir /etc/profile.d: %w", err)
	}
	profilePath := "/etc/profile.d/sand-http-cache.sh"
	if err := exec.ExecStreamInput(ctx, strings.NewReader(profile.String()), io.Discard, io.Discard, "tee", profilePath); err != nil {
		return fmt.Errorf("write %s: %w", profilePath, err)
	}
	if _, err := exec.Exec(ctx, "chmod", "0644", profilePath); err != nil {
		return fmt.Errorf("chmod %s: %w", profilePath, err)
	}

	current, err := exec.Exec(ctx, "cat", "/etc/environment")
	if err != nil {
		current = ""
	}
	next := stripHTTPProxyEnv(current, keys)
	for _, key := range keys {
		next += key + "=" + env[key] + "\n"
	}
	tmp := "/etc/environment.sand.tmp"
	if err := exec.ExecStreamInput(ctx, strings.NewReader(next), io.Discard, io.Discard, "tee", tmp); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if _, err := exec.Exec(ctx, "mv", tmp, "/etc/environment"); err != nil {
		return fmt.Errorf("mv %s /etc/environment: %w", tmp, err)
	}
	return nil
}

func installHTTPProxyCA(ctx context.Context, exec sandtypes.HookStreamer, caCertPath string) error {
	if _, err := exec.Exec(ctx, "test", "-f", caCertPath); err != nil {
		return fmt.Errorf("shared HTTP proxy CA certificate is not mounted at %s: %w", caCertPath, err)
	}
	if !commandExists(ctx, exec, "update-ca-certificates") {
		switch {
		case commandExists(ctx, exec, "apk"):
			if err := stream(ctx, exec, "apk", "--no-check-certificate", "add", "--no-cache", "ca-certificates"); err != nil {
				return fmt.Errorf("install ca-certificates with apk: %w", err)
			}
		case commandExists(ctx, exec, "apt-get"):
			aptTLSBootstrapOptions := []string{
				"-o", "Acquire::https::Verify-Peer=false",
				"-o", "Acquire::https::Verify-Host=false",
			}
			if err := stream(ctx, exec, "apt-get", append(aptTLSBootstrapOptions, "update")...); err != nil {
				return fmt.Errorf("apt-get update for ca-certificates: %w", err)
			}
			if err := stream(ctx, exec, "apt-get", append(aptTLSBootstrapOptions, "install", "-y", "--no-install-recommends", "ca-certificates")...); err != nil {
				return fmt.Errorf("install ca-certificates with apt-get: %w", err)
			}
		default:
			return fmt.Errorf("shared HTTP proxy HTTPS caching requires ca-certificates support: update-ca-certificates is missing and neither apk nor apt-get is available")
		}
	}
	if err := stream(ctx, exec, "update-ca-certificates"); err != nil {
		return fmt.Errorf("update CA certificates for shared HTTP proxy: %w", err)
	}
	return nil
}

func stripHTTPProxyEnv(current string, keys []string) string {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(current, "\n") {
		if line == "" {
			continue
		}
		name, _, found := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
		if found {
			if _, ok := keySet[name]; ok {
				continue
			}
		}
		out.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func installNPMAgent(ctx context.Context, exec sandtypes.HookStreamer, command, pkg, version string) error {
	if commandExists(ctx, exec, command) {
		return nil
	}
	nodeDir, err := ensureNPMAgentNode(ctx, exec)
	if err != nil {
		return err
	}

	installPrefix := "/usr/local/lib/sand-npm-agents/" + command
	if _, err := exec.Exec(ctx, "mkdir", "-p", installPrefix); err != nil {
		return fmt.Errorf("create npm agent install prefix: %w", err)
	}
	nodeBinDir := nodeDir + "/bin"
	installPath := nodeBinDir + ":/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	if err := stream(ctx, exec, "env", "PATH="+installPath, nodeBinDir+"/npm", "install", "-g", "--prefix", installPrefix, pkg+"@"+version); err != nil {
		return err
	}

	wrapperPath := "/usr/local/bin/" + command
	wrapper := "#!/bin/sh\nexec " + nodeBinDir + "/node " + installPrefix + "/bin/" + command + " \"$@\"\n"
	tmpWrapper := wrapperPath + ".sand.tmp"
	if err := exec.ExecStreamInput(ctx, strings.NewReader(wrapper), io.Discard, io.Discard, "tee", tmpWrapper); err != nil {
		return fmt.Errorf("write %s: %w", tmpWrapper, err)
	}
	if _, err := exec.Exec(ctx, "chmod", "0755", tmpWrapper); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpWrapper, err)
	}
	if _, err := exec.Exec(ctx, "mv", tmpWrapper, wrapperPath); err != nil {
		return fmt.Errorf("install %s wrapper: %w", command, err)
	}
	return nil
}

func ensureNPMAgentNode(ctx context.Context, exec sandtypes.HookStreamer) (string, error) {
	archOut, err := exec.Exec(ctx, "uname", "-m")
	if err != nil {
		return "", fmt.Errorf("uname -m: %w", err)
	}
	archiveArch, err := nodeArchiveArch(strings.TrimSpace(archOut))
	if err != nil {
		return "", err
	}

	cacheRoot := agentCacheRoot(ctx, exec)
	runtimeDir := cacheRoot + "/node/" + npmAgentNodeVersion + "/linux-" + archiveArch
	if nodeRuntimeValid(ctx, exec, runtimeDir) {
		return runtimeDir, nil
	}
	if _, err := exec.Exec(ctx, "mkdir", "-p", path.Dir(runtimeDir)); err != nil {
		return "", fmt.Errorf("create Node cache parent: %w", err)
	}
	lockDir := runtimeDir + ".lock"
	if err := acquireLock(ctx, exec, lockDir); err != nil {
		return "", err
	}
	defer exec.Exec(context.WithoutCancel(ctx), "rm", "-rf", lockDir) //nolint:errcheck
	if nodeRuntimeValid(ctx, exec, runtimeDir) {
		return runtimeDir, nil
	}
	if _, err := exec.Exec(ctx, "test", "-e", runtimeDir); err == nil {
		if _, err := exec.Exec(ctx, "rm", "-rf", runtimeDir); err != nil {
			return "", fmt.Errorf("remove invalid cached Node runtime: %w", err)
		}
	}

	tmpDir, err := exec.Exec(ctx, "mktemp", "-d")
	if err != nil {
		return "", fmt.Errorf("mktemp -d: %w", err)
	}
	tmpDir = strings.TrimSpace(tmpDir)
	if tmpDir == "" {
		return "", fmt.Errorf("mktemp -d returned an empty path")
	}
	defer exec.Exec(context.WithoutCancel(ctx), "rm", "-rf", tmpDir) //nolint:errcheck

	archive := fmt.Sprintf("node-v%s-linux-%s.tar.gz", npmAgentNodeVersion, archiveArch)
	releaseURL := nodeDownloadBaseURL + "/v" + npmAgentNodeVersion
	checksums, err := exec.Exec(ctx, "curl", "-fsSL", releaseURL+"/SHASUMS256.txt")
	if err != nil {
		return "", fmt.Errorf("download Node checksums: %w", err)
	}
	wantChecksum, err := checksumForArchive(checksums, archive)
	if err != nil {
		return "", err
	}
	archivePath := tmpDir + "/" + archive
	if err := stream(ctx, exec, "curl", "-fsSL", "-o", archivePath, releaseURL+"/"+archive); err != nil {
		return "", fmt.Errorf("download Node %s: %w", npmAgentNodeVersion, err)
	}
	checksumOut, err := exec.Exec(ctx, "sha256sum", archivePath)
	if err != nil {
		return "", fmt.Errorf("checksum Node archive: %w", err)
	}
	gotChecksum := strings.Fields(checksumOut)
	if len(gotChecksum) == 0 || gotChecksum[0] != wantChecksum {
		return "", fmt.Errorf("Node archive checksum mismatch")
	}

	stagedDir := tmpDir + "/runtime"
	if _, err := exec.Exec(ctx, "mkdir", "-p", stagedDir); err != nil {
		return "", fmt.Errorf("create staged Node runtime: %w", err)
	}
	if err := stream(ctx, exec, "tar", "-xzf", archivePath, "-C", stagedDir, "--strip-components=1"); err != nil {
		return "", fmt.Errorf("extract Node archive: %w", err)
	}
	if !nodeRuntimeValid(ctx, exec, stagedDir) {
		return "", fmt.Errorf("downloaded Node runtime is not v%s", npmAgentNodeVersion)
	}
	if _, err := exec.Exec(ctx, "mv", stagedDir, runtimeDir); err != nil {
		return "", fmt.Errorf("publish Node runtime: %w", err)
	}
	return runtimeDir, nil
}

func agentCacheRoot(ctx context.Context, exec sandtypes.HookStreamer) string {
	const shared = "/opt/sand-agent-cache"
	if _, err := exec.Exec(ctx, "test", "-d", shared); err != nil {
		return "/tmp/sand-agent-cache"
	}
	if _, err := exec.Exec(ctx, "test", "-w", shared); err != nil {
		return "/tmp/sand-agent-cache"
	}
	return shared
}

func nodeRuntimeValid(ctx context.Context, exec sandtypes.HookStreamer, runtimeDir string) bool {
	node := runtimeDir + "/bin/node"
	if _, err := exec.Exec(ctx, "test", "-x", node); err != nil {
		return false
	}
	version, err := exec.Exec(ctx, node, "--version")
	return err == nil && strings.TrimSpace(version) == "v"+npmAgentNodeVersion
}

func nodeArchiveArch(arch string) (string, error) {
	switch arch {
	case "x86_64", "amd64":
		return "x64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("Node %s is unavailable for architecture %q", npmAgentNodeVersion, arch)
	}
}

func checksumForArchive(checksums, archive string) (string, error) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == archive {
			if len(fields[0]) != 64 {
				break
			}
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("Node checksum manifest does not contain %s", archive)
}

func installOpenCodeAgent(ctx context.Context, exec sandtypes.HookStreamer, command, version string) error {
	if commandExists(ctx, exec, command) {
		return nil
	}
	if commandExists(ctx, exec, "apk") {
		if err := stream(ctx, exec, "apk", "add", "--no-cache", "curl", "bash", "git", "libc6-compat", "libstdc++"); err != nil {
			return err
		}
	} else if commandExists(ctx, exec, "apt-get") {
		if err := stream(ctx, exec, "apt-get", "update"); err != nil {
			return err
		}
		if err := stream(ctx, exec, "apt-get", "install", "-y", "--no-install-recommends", "curl", "bash", "git", "libc6", "libstdc++6"); err != nil {
			return err
		}
	}

	archOut, err := exec.Exec(ctx, "uname", "-m")
	if err != nil {
		return fmt.Errorf("uname -m: %w", err)
	}
	arch := normalizeArch(strings.TrimSpace(archOut))
	cacheRoot := "/opt/sand-agent-cache"
	if _, err := exec.Exec(ctx, "test", "-d", cacheRoot); err != nil {
		cacheRoot = "/tmp/sand-agent-cache"
	} else if _, err := exec.Exec(ctx, "test", "-w", cacheRoot); err != nil {
		cacheRoot = "/tmp/sand-agent-cache"
	}
	cacheDir := cacheRoot + "/opencode/" + version + "/" + arch
	lockDir := cacheDir + ".lock"
	if _, err := exec.Exec(ctx, "mkdir", "-p", path.Dir(cacheDir)); err != nil {
		return fmt.Errorf("mkdir %s: %w", path.Dir(cacheDir), err)
	}
	if err := acquireLock(ctx, exec, lockDir); err != nil {
		return err
	}
	defer exec.Exec(context.WithoutCancel(ctx), "rm", "-rf", lockDir) //nolint:errcheck

	cachedBin := cacheDir + "/opencode"
	if _, err := exec.Exec(ctx, "test", "-x", cachedBin); err != nil {
		tmpHome, err := exec.Exec(ctx, "mktemp", "-d")
		if err != nil {
			return fmt.Errorf("mktemp -d: %w", err)
		}
		tmpHome = strings.TrimSpace(tmpHome)
		defer exec.Exec(context.WithoutCancel(ctx), "rm", "-rf", tmpHome) //nolint:errcheck

		installer, err := exec.Exec(ctx, "curl", "-fsSL", "https://opencode.ai/install")
		if err != nil {
			return fmt.Errorf("download opencode installer: %w", err)
		}
		if err := exec.ExecStreamInput(ctx, strings.NewReader(installer), io.Discard, io.Discard, "env", "HOME="+tmpHome, "bash", "-s", "--", "--version", version); err != nil {
			return fmt.Errorf("run opencode installer: %w", err)
		}
		if _, err := exec.Exec(ctx, "mkdir", "-p", cacheDir); err != nil {
			return fmt.Errorf("mkdir %s: %w", cacheDir, err)
		}
		tmpBin := cacheDir + "/opencode.tmp"
		if _, err := exec.Exec(ctx, "cp", tmpHome+"/.opencode/bin/opencode", tmpBin); err != nil {
			return fmt.Errorf("copy opencode to cache: %w", err)
		}
		if _, err := exec.Exec(ctx, "chmod", "+x", tmpBin); err != nil {
			return fmt.Errorf("chmod cached opencode: %w", err)
		}
		if _, err := exec.Exec(ctx, "mv", tmpBin, cachedBin); err != nil {
			return fmt.Errorf("publish cached opencode: %w", err)
		}
	}
	if _, err := exec.Exec(ctx, "cp", cachedBin, "/usr/local/bin/opencode"); err != nil {
		return fmt.Errorf("install opencode: %w", err)
	}
	if _, err := exec.Exec(ctx, "chmod", "+x", "/usr/local/bin/opencode"); err != nil {
		return fmt.Errorf("chmod /usr/local/bin/opencode: %w", err)
	}
	return nil
}

func commandExists(ctx context.Context, exec sandtypes.HookStreamer, command string) bool {
	_, err := exec.Exec(ctx, "which", command)
	return err == nil
}

func stream(ctx context.Context, exec sandtypes.HookStreamer, command string, args ...string) error {
	var buf bytes.Buffer
	if err := exec.ExecStream(ctx, &buf, &buf, command, args...); err != nil {
		out := strings.TrimSpace(buf.String())
		if out != "" {
			return fmt.Errorf("%s: %w: %s", strings.Join(append([]string{command}, args...), " "), err, out)
		}
		return fmt.Errorf("%s: %w", strings.Join(append([]string{command}, args...), " "), err)
	}
	return nil
}

func normalizeArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

func acquireLock(ctx context.Context, exec sandtypes.HookStreamer, lockDir string) error {
	for {
		if _, err := exec.Exec(ctx, "mkdir", lockDir); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
