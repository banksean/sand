package boxer

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/sandtypes"
)

func (sb *Boxer) prepareMountRequests(ctx context.Context, paths cloning.PathRegistry, directMounts, cloneMounts []string) ([]sandtypes.MountRequest, error) {
	if len(directMounts) == 0 && len(cloneMounts) == 0 {
		return nil, nil
	}

	requests := make([]sandtypes.MountRequest, 0, len(directMounts)+len(cloneMounts))
	for _, mount := range directMounts {
		parsed, err := parseBindMountRequest(mount)
		if err != nil {
			return nil, err
		}
		requests = append(requests, sandtypes.MountRequest{
			Kind:     sandtypes.MountKindBind,
			Original: mount,
			Source:   parsed.Source,
			Target:   parsed.Target,
			ReadOnly: parsed.ReadOnly,
			Runtime:  renderBindMount(parsed.Source, parsed.Target, parsed.ReadOnly),
		})
	}

	if len(cloneMounts) == 0 {
		return requests, nil
	}
	if err := sb.FileOps.MkdirAll(paths.BindMountsDir(), 0o750); err != nil {
		return nil, fmt.Errorf("create cloned bind-mount directory: %w", err)
	}

	cloneRootVolume, err := sb.FileOps.Volume(paths.SandboxRoot())
	if err != nil {
		return nil, fmt.Errorf("get clone root volume info: %w", err)
	}

	for i, mount := range cloneMounts {
		parsed, err := parseBindMountRequest(mount)
		if err != nil {
			return nil, err
		}
		fi, err := sb.FileOps.Stat(parsed.Source)
		if err != nil {
			return nil, fmt.Errorf("clone mount source %q: %w", parsed.Source, err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("clone mount source %q is not a directory", parsed.Source)
		}
		sourceVolume, err := sb.FileOps.Volume(parsed.Source)
		if err != nil {
			return nil, fmt.Errorf("get clone mount source info for %q: %w", parsed.Source, err)
		}
		if sourceVolume.DeviceID != cloneRootVolume.DeviceID {
			return nil, fmt.Errorf("can't clone bind mount %q across volumes: source volume %s vs clone root volume %s", parsed.Source, sourceVolume.MountPoint, cloneRootVolume.MountPoint)
		}

		clonePath := filepath.Join(paths.BindMountsDir(), clonedBindMountName(i, parsed.Source))
		if err := sb.FileOps.Copy(ctx, parsed.Source, clonePath); err != nil {
			return nil, fmt.Errorf("clone bind mount %q to %q: %w", parsed.Source, clonePath, err)
		}

		requests = append(requests, sandtypes.MountRequest{
			Kind:     sandtypes.MountKindClone,
			Original: mount,
			Source:   parsed.Source,
			Clone:    clonePath,
			Target:   parsed.Target,
			ReadOnly: parsed.ReadOnly,
			Runtime:  renderBindMount(clonePath, parsed.Target, parsed.ReadOnly),
		})
	}

	return requests, nil
}

type parsedBindMountRequest struct {
	Source   string
	Target   string
	ReadOnly bool
}

func parseBindMountRequest(spec string) (parsedBindMountRequest, error) {
	var ret parsedBindMountRequest
	if spec == "" {
		return ret, fmt.Errorf("mount spec is required")
	}
	seen := map[string]struct{}{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return ret, fmt.Errorf("mount %q contains an empty field", spec)
		}
		switch part {
		case "readonly":
			ret.ReadOnly = true
			continue
		case "type=bind":
			continue
		}

		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return ret, fmt.Errorf("mount %q contains unsupported field %q", spec, part)
		}
		if _, exists := seen[key]; exists {
			return ret, fmt.Errorf("mount %q contains duplicate %q", spec, key)
		}
		seen[key] = struct{}{}

		switch key {
		case "source":
			ret.Source = expandHomePath(value)
		case "target":
			ret.Target = value
		default:
			return ret, fmt.Errorf("mount %q contains unsupported field %q", spec, key)
		}
	}

	if ret.Source == "" {
		return ret, fmt.Errorf("mount %q must include source", spec)
	}
	if ret.Target == "" {
		return ret, fmt.Errorf("mount %q must include target", spec)
	}
	if !filepath.IsAbs(ret.Source) {
		return ret, fmt.Errorf("mount source %q must be absolute", ret.Source)
	}
	if !path.IsAbs(ret.Target) {
		return ret, fmt.Errorf("mount target %q must be absolute", ret.Target)
	}

	ret.Source = filepath.Clean(ret.Source)
	ret.Target = path.Clean(ret.Target)
	return ret, nil
}

func renderBindMount(source, target string, readOnly bool) string {
	mount := sandtypes.MountSpec{
		Source:   source,
		Target:   target,
		ReadOnly: readOnly,
	}
	return mount.String()
}

func clonedBindMountName(index int, source string) string {
	base := filepath.Base(filepath.Clean(source))
	if base == "." || base == string(filepath.Separator) {
		base = "root"
	}
	replacer := strings.NewReplacer(":", "-", string(filepath.Separator), "-")
	return fmt.Sprintf("%03d-%s", index, replacer.Replace(base))
}

func expandHomePath(value string) string {
	if value == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return value
	}
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(value, "~/"))
		}
	}
	return value
}
