package cloning

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/banksean/sand/internal/agentdefs"
	"github.com/banksean/sand/internal/sandtypes"
)

// DefinitionContainerConfiguration combines the shared base container setup
// with hooks declared by an agent definition.
type DefinitionContainerConfiguration struct {
	base       *BaseContainerConfiguration
	agentName  string
	install    *agentdefs.InstallSpec
	startHooks []string
}

func NewDefinitionContainerConfiguration(definition agentdefs.Definition) *DefinitionContainerConfiguration {
	return &DefinitionContainerConfiguration{
		base:       NewBaseContainerConfiguration(),
		agentName:  definition.Name,
		install:    definition.Install,
		startHooks: definition.StartHooks,
	}
}

var _ ContainerConfiguration = &DefinitionContainerConfiguration{}

func (c *DefinitionContainerConfiguration) GetMounts(artifacts CloneArtifacts) []sandtypes.MountSpec {
	return c.base.GetMounts(artifacts)
}

func (c *DefinitionContainerConfiguration) GetFirstStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	hooks := c.base.GetFirstStartHooks(artifacts)
	if c.install != nil {
		hooks = append(hooks, c.installAgentHook())
	}
	hooks = append(hooks, c.namedHooks(artifacts)...)
	return hooks
}

func (c *DefinitionContainerConfiguration) GetStartHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	return c.namedHooks(artifacts)
}

func (c *DefinitionContainerConfiguration) installAgentHook() sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("install "+c.agentName+" agent", func(ctx context.Context, ctr *sandtypes.Container, exec sandtypes.HookStreamer) error {
		script, err := agentInstallScript(c.agentName, *c.install)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := exec.ExecStream(ctx, &buf, &buf, "sh", "-c", script); err != nil {
			out := strings.TrimSpace(buf.String())
			if out != "" {
				return fmt.Errorf("install %s agent: %w: %s", c.agentName, err, out)
			}
			return fmt.Errorf("install %s agent: %w", c.agentName, err)
		}
		return nil
	})
}

var safeShellToken = regexp.MustCompile(`^[A-Za-z0-9@._/+:-]+$`)

func agentInstallScript(agentName string, install agentdefs.InstallSpec) (string, error) {
	if err := validateInstallSpec(agentName, install); err != nil {
		return "", err
	}

	switch install.Kind {
	case agentdefs.InstallerNPM:
		return npmAgentInstallScript(agentName, install), nil
	case agentdefs.InstallerOpenCode:
		return openCodeAgentInstallScript(agentName, install), nil
	default:
		return "", fmt.Errorf("unknown installer kind %q for agent %q", install.Kind, agentName)
	}
}

func validateInstallSpec(agentName string, install agentdefs.InstallSpec) error {
	for label, value := range map[string]string{
		"agent":   agentName,
		"kind":    install.Kind,
		"version": install.Version,
		"command": install.Command,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required for agent install spec", label)
		}
		if !safeShellToken.MatchString(value) {
			return fmt.Errorf("%s %q contains unsafe shell characters", label, value)
		}
	}
	if install.Kind == agentdefs.InstallerNPM {
		if strings.TrimSpace(install.Package) == "" {
			return fmt.Errorf("package is required for npm agent install spec")
		}
		if !safeShellToken.MatchString(install.Package) {
			return fmt.Errorf("package %q contains unsafe shell characters", install.Package)
		}
	}
	return nil
}

func agentInstallScriptPrelude(agentName, version string) string {
	return fmt.Sprintf(`set -eu
AGENT=%s
VERSION=%s
ARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
CACHE_ROOT=%s
if [ ! -d "$CACHE_ROOT" ] || [ ! -w "$CACHE_ROOT" ]; then
	CACHE_ROOT=/tmp/sand-agent-cache
fi
CACHE_DIR="$CACHE_ROOT/$AGENT/$VERSION/$ARCH"
LOCK_DIR="$CACHE_DIR.lock"
mkdir -p "$(dirname "$CACHE_DIR")"
while ! mkdir "$LOCK_DIR" 2>/dev/null; do
	sleep 1
done
cleanup_lock() {
	rm -rf "$LOCK_DIR"
}
trap cleanup_lock EXIT INT TERM
`, agentName, version, agentCachePath)
}

func npmAgentInstallScript(agentName string, install agentdefs.InstallSpec) string {
	return agentInstallScriptPrelude(agentName, install.Version) + fmt.Sprintf(`
if ! command -v %s >/dev/null 2>&1; then
	if command -v apk >/dev/null 2>&1; then
		apk add --no-cache nodejs npm
	elif command -v apt-get >/dev/null 2>&1; then
		apt-get update
		apt-get install -y --no-install-recommends nodejs npm
	fi
	INSTALL_TGZ="$CACHE_DIR/install.tgz"
	if [ ! -s "$INSTALL_TGZ" ]; then
		TMP_DIR="$(mktemp -d)"
		trap 'rm -rf "$TMP_DIR"; cleanup_lock' EXIT INT TERM
		npm install -g --prefix "$TMP_DIR/prefix" %s@%s
		mkdir -p "$CACHE_DIR"
		TMP_TGZ="$CACHE_DIR/install.tgz.tmp.$$"
		tar -C "$TMP_DIR/prefix" -czf "$TMP_TGZ" .
		mv "$TMP_TGZ" "$INSTALL_TGZ"
		rm -rf "$TMP_DIR"
		trap cleanup_lock EXIT INT TERM
	fi
	tar -C /usr/local -xzf "$INSTALL_TGZ"
fi
`, install.Command, install.Package, install.Version)
}

func openCodeAgentInstallScript(agentName string, install agentdefs.InstallSpec) string {
	return agentInstallScriptPrelude(agentName, install.Version) + fmt.Sprintf(`
if ! command -v %s >/dev/null 2>&1; then
	if command -v apk >/dev/null 2>&1; then
		apk add --no-cache curl bash git libc6-compat libstdc++
	elif command -v apt-get >/dev/null 2>&1; then
		apt-get update
		apt-get install -y --no-install-recommends curl bash git libc6 libstdc++6
	fi
	CACHED_BIN="$CACHE_DIR/opencode"
	if [ ! -x "$CACHED_BIN" ]; then
		TMP_HOME="$(mktemp -d)"
		trap 'rm -rf "$TMP_HOME"; cleanup_lock' EXIT INT TERM
		HOME="$TMP_HOME" curl -fsSL https://opencode.ai/install | HOME="$TMP_HOME" bash -s -- --version %s
		mkdir -p "$CACHE_DIR"
		TMP_BIN="$CACHE_DIR/opencode.tmp.$$"
		cp "$TMP_HOME/.opencode/bin/opencode" "$TMP_BIN"
		chmod +x "$TMP_BIN"
		mv "$TMP_BIN" "$CACHED_BIN"
		rm -rf "$TMP_HOME"
		trap cleanup_lock EXIT INT TERM
	fi
	cp "$CACHED_BIN" /usr/local/bin/opencode
	chmod +x /usr/local/bin/opencode
fi
`, install.Command, install.Version)
}

func (c *DefinitionContainerConfiguration) namedHooks(artifacts CloneArtifacts) []sandtypes.ContainerHook {
	hooks := make([]sandtypes.ContainerHook, 0, len(c.startHooks))
	for _, name := range c.startHooks {
		switch name {
		case agentdefs.HookOpenCodeTunnel:
			hooks = append(hooks, openCodeSSHTunnelHook(artifacts.Username))
		default:
			hooks = append(hooks, unknownAgentHook(name))
		}
	}
	return hooks
}

func unknownAgentHook(name string) sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("unknown agent hook "+name, func(ctx context.Context, ctr *sandtypes.Container, exec sandtypes.HookStreamer) error {
		return fmt.Errorf("unknown agent hook %q", name)
	})
}
