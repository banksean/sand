package cloning

import (
	"path/filepath"
)

// PathRegistry provides centralized path management for sandbox directory structure.
// It ensures a single source of truth for where files are located within a sandbox.
type PathRegistry interface {
	// SandboxRoot returns the root directory for the sandbox on the host
	SandboxRoot() string

	// WorkDir returns the path to the cloned workspace directory
	WorkDir() string

	// DotfilesDir returns the path to the dotfiles directory
	DotfilesDir() string

	// SSHKeysDir returns the path to the SSH keys directory
	SSHKeysDir() string
}

// StandardPathRegistry implements PathRegistry with the standard sandbox directory layout:
// {root}/
//
//	app/          - cloned workspace
//	dotfiles/     - user dotfiles
//	sshkeys/      - SSH keys for container access
type StandardPathRegistry struct {
	root string
}

// NewStandardPathRegistry creates a PathRegistry for the standard sandbox layout.
func NewStandardPathRegistry(sandboxRoot string) PathRegistry {
	return &StandardPathRegistry{root: sandboxRoot}
}

func (p *StandardPathRegistry) SandboxRoot() string {
	return p.root
}

func (p *StandardPathRegistry) WorkDir() string {
	return filepath.Join(p.root, "app")
}

func (p *StandardPathRegistry) DotfilesDir() string {
	return filepath.Join(p.root, "dotfiles")
}

func (p *StandardPathRegistry) SSHKeysDir() string {
	return filepath.Join(p.root, "sshkeys")
}
