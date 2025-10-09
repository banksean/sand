package version

var (
	// These will be set via -ldflags during build
	GitRepo   string
	GitBranch string
	GitCommit string
	BuildTime string
)

// Info returns a struct containing all version information
type Info struct {
	GitRepo   string `json:"gitRepo,omitempty"`
	GitBranch string `json:"gitBranch,omitempty"`
	GitCommit string `json:"gitCommit,omitempty"`
	BuildTime string `json:"buildTime,omitempty"`
}

// Get returns the version information
func Get() Info {
	return Info{
		GitRepo:   GitRepo,
		GitBranch: GitBranch,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
	}
}

// Equal checks if two version infos represent the same version
// Two versions are considered equal if they have the same git commit
func (v Info) Equal(other Info) bool {
	// If both have empty commits, they're equal (both built from source without version info)
	if v.GitCommit == "" && other.GitCommit == "" {
		return true
	}
	// Otherwise, they must have the same commit
	return v.GitCommit == other.GitCommit
}
