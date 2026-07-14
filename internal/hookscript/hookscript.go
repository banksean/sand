package hookscript

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/banksean/sand/internal/sandtypes"
	"rsc.io/script"
)

const (
	bazelrcManagedStart = "# sand bazel remote cache start"
	bazelrcManagedEnd   = "# sand bazel remote cache end"
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

func installNPMAgent(ctx context.Context, exec sandtypes.HookStreamer, command, pkg, version string) error {
	if commandExists(ctx, exec, command) {
		return nil
	}
	if commandExists(ctx, exec, "apk") {
		if err := stream(ctx, exec, "apk", "add", "--no-cache", "nodejs", "npm"); err != nil {
			return err
		}
	} else if commandExists(ctx, exec, "apt-get") {
		if err := stream(ctx, exec, "apt-get", "update"); err != nil {
			return err
		}
		if err := stream(ctx, exec, "apt-get", "install", "-y", "--no-install-recommends", "nodejs", "npm"); err != nil {
			return err
		}
	}
	return stream(ctx, exec, "npm", "install", "-g", pkg+"@"+version)
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
