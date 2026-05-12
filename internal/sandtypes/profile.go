package sandtypes

type DotfileMode string

const DefaultProfileName = "default"

const (
	DotfileModeNone      DotfileMode = "none"
	DotfileModeMinimal   DotfileMode = "minimal"
	DotfileModeAllowlist DotfileMode = "allowlist"
)

type EnvScope string

const (
	EnvScopeAuth    EnvScope = "auth"
	EnvScopeProject EnvScope = "project"
	EnvScopeShell   EnvScope = "shell"
	EnvScopeAll     EnvScope = "all"
)

type SSHAgentMode string

const (
	SSHAgentModeOff   SSHAgentMode = "off"
	SSHAgentModeOptIn SSHAgentMode = "opt-in"
	SSHAgentModeOn    SSHAgentMode = "on"
)

type GitConfigPolicy string

const (
	GitConfigPolicyNone      GitConfigPolicy = "none"
	GitConfigPolicySanitized GitConfigPolicy = "sanitized"
	GitConfigPolicyCopy      GitConfigPolicy = "copy"
)

// Profile describes what host-side material sand may expose to a sandbox.
// It stores policy and references only; resolved secret values do not belong here.
type Profile struct {
	Name     string        `json:"name,omitempty" yaml:"-"`
	Dotfiles DotfilePolicy `json:"dotfiles,omitempty" yaml:"dotfiles,omitempty"`
	Env      EnvPolicy     `json:"env,omitempty" yaml:"env,omitempty"`
	SSH      SSHPolicy     `json:"ssh,omitempty" yaml:"ssh,omitempty"`
	Git      GitPolicy     `json:"git,omitempty" yaml:"git,omitempty"`
	Network  NetworkPolicy `json:"network,omitempty" yaml:"network,omitempty"`
}

type DotfilePolicy struct {
	Mode  DotfileMode   `json:"mode,omitempty" yaml:"mode,omitempty"`
	Files []DotfileRule `json:"files,omitempty" yaml:"files,omitempty"`
}

type DotfileRule struct {
	Source           string `json:"source,omitempty" yaml:"source,omitempty"`
	Target           string `json:"target,omitempty" yaml:"target,omitempty"`
	AllowSymlink     bool   `json:"allowSymlink,omitempty" yaml:"allowSymlink,omitempty"`
	AllowOutsideHome bool   `json:"allowOutsideHome,omitempty" yaml:"allowOutsideHome,omitempty"`
}

type EnvPolicy struct {
	Files []EnvFileRef `json:"files,omitempty" yaml:"files,omitempty"`
	Vars  []EnvVarRule `json:"vars,omitempty" yaml:"vars,omitempty"`
}

type EnvFileRef struct {
	Path  string   `json:"path,omitempty" yaml:"path,omitempty"`
	Scope EnvScope `json:"scope,omitempty" yaml:"scope,omitempty"`
}

type EnvVarRule struct {
	Name  string   `json:"name,omitempty" yaml:"name,omitempty"`
	Scope EnvScope `json:"scope,omitempty" yaml:"scope,omitempty"`
}

type SSHPolicy struct {
	AgentForwarding SSHAgentMode `json:"agentForwarding,omitempty" yaml:"agentForwarding,omitempty"`
}

type GitPolicy struct {
	Config GitConfigPolicy `json:"config,omitempty" yaml:"config,omitempty"`
}

type NetworkPolicy struct {
	AllowedDomainsFile string `json:"allowedDomainsFile,omitempty" yaml:"allowedDomainsFile,omitempty"`
}
