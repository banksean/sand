package version

import (
	"runtime/debug"
)

var (
	// These will be set via -ldflags during build
	GitRepo   string
	GitBranch string
	GitCommit string
	BuildTime string
)

// Info returns a struct containing all version information
type Info struct {
	GitRepo   string           `json:"gitRepo,omitempty"`
	GitBranch string           `json:"gitBranch,omitempty"`
	GitCommit string           `json:"gitCommit,omitempty"`
	BuildTime string           `json:"buildTime,omitempty"`
	BuildInfo *debug.BuildInfo `json:"buildInfo,omitempty"`
}

// Get returns the version information
func Get() Info {
	buildInfo, ok := debug.ReadBuildInfo()
	ret := Info{
		GitRepo:   GitRepo,
		GitBranch: GitBranch,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
	}
	if ok {
		ret.BuildInfo = buildInfo
	}
	return ret
}

// Equal checks if two version infos represent the same version
// Two versions are considered equal if they have the same git commit
// and build info settings.
func (v Info) Equal(other Info) bool {
	if v.BuildInfo != nil {
		if other.BuildInfo == nil {
			return false
		}
	}
	if v.BuildTime != other.BuildTime ||
		v.GitBranch != other.GitBranch ||
		v.GitCommit != other.GitCommit ||
		v.GitRepo != other.GitRepo {
		return false
	}
	if other.BuildInfo != nil && v.BuildInfo == nil ||
		other.BuildInfo == nil && v.BuildInfo != nil {
		return false
	}
	if v.BuildInfo == nil {
		return true
	}
	if len(other.BuildInfo.Settings) != len(v.BuildInfo.Settings) {
		return false
	}
	for i, setting := range v.BuildInfo.Settings {
		if other.BuildInfo.Settings[i] != setting {
			return false
		}
	}
	return true
}
