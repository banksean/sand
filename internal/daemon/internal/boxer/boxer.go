package boxer

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/banksean/sand/internal/agents"
	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/containerruntime"
	"github.com/banksean/sand/internal/daemon/internal/lifecycle"
	"github.com/banksean/sand/internal/db"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/imageprogress"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/banksean/sand/internal/sandboxlog"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/sshimmer"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "modernc.org/sqlite"
)

const containerGetErrorMsg = "[error getting]"

// SSHimmer provisions SSH keys for a new sandbox.
type SSHimmer interface {
	NewKeys(ctx context.Context, domain, username string) (*sshimmer.Keys, error)
}

// Boxer manages the lifecycle of sandboxes.
type Boxer struct {
	appRoot          string
	messenger        hostops.UserMessenger
	sqlDB            *sql.DB
	queries          *db.Queries
	ContainerService hostops.ContainerOps
	ImageService     hostops.ImageOps
	GitOps           hostops.GitOps
	FileOps          hostops.FileOps
	SSHim            SSHimmer
	AgentRegistry    *agents.AgentRegistry
	httpProxyService *HTTPProxyCacheService
}

func runtimeArtifactsFromClone(artifacts *cloning.CloneArtifacts) containerruntime.Artifacts {
	return containerruntime.Artifacts{
		SandboxWorkDir:    artifacts.SandboxWorkDir,
		WorkDir:           artifacts.PathRegistry.WorkDir(),
		DotfilesDir:       artifacts.PathRegistry.DotfilesDir(),
		SSHKeysDir:        artifacts.PathRegistry.SSHKeysDir(),
		HostGitMirrorDir:  artifacts.HostGitMirrorDir,
		Username:          artifacts.Username,
		Uid:               artifacts.Uid,
		SharedCacheMounts: artifacts.SharedCacheMounts,
	}
}

// BoxerDeps holds the injectable dependencies for a Boxer.
// Fields left nil will cause panics if the corresponding Boxer methods are called.
type BoxerDeps struct {
	ContainerService hostops.ContainerOps
	ImageService     hostops.ImageOps
	GitOps           hostops.GitOps
	FileOps          hostops.FileOps
	SSHim            SSHimmer
	AgentRegistry    *agents.AgentRegistry
	Messenger        hostops.UserMessenger
}

// NewBoxerWithDeps creates a Boxer with explicitly provided dependencies and a fresh
// SQLite database at appRoot. The appRoot directory is created with os.MkdirAll,
// making this constructor usable on all platforms without darwin-specific file ops.
func NewBoxerWithDeps(appRoot string, deps BoxerDeps) (*Boxer, error) {
	if err := os.MkdirAll(appRoot, 0o750); err != nil {
		return nil, err
	}
	sqlDB, err := db.Connect(appRoot)
	if err != nil {
		return nil, err
	}
	if deps.AgentRegistry == nil {
		deps.AgentRegistry = agents.NewAgentRegistry()
	}
	if deps.Messenger == nil {
		deps.Messenger = hostops.NewTerminalMessenger(nil)
	}
	return &Boxer{
		appRoot:          appRoot,
		messenger:        deps.Messenger,
		sqlDB:            sqlDB,
		queries:          db.New(sqlDB),
		ContainerService: deps.ContainerService,
		ImageService:     deps.ImageService,
		GitOps:           deps.GitOps,
		FileOps:          deps.FileOps,
		SSHim:            deps.SSHim,
		AgentRegistry:    deps.AgentRegistry,
	}, nil
}

func NewBoxer(appRoot, localDomain string, terminalWriter io.Writer) (*Boxer, error) {
	fileOps := hostops.NewDefaultFileOps()
	if err := fileOps.MkdirAll(appRoot, 0o750); err != nil {
		return nil, err
	}

	sqlDB, err := db.Connect(appRoot)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	sshim, err := sshimmer.NewLocalSSHimmer(ctx, localDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to create LocalSSHimmer: %w", err)
	}

	messenger := hostops.NewTerminalMessenger(terminalWriter)
	agentRegistry := agents.InitializeGlobalRegistry(appRoot, messenger, hostops.NewDefaultGitOps(), fileOps)
	containerService, err := hostops.NewAppleContainerOps()
	if err != nil {
		return nil, fmt.Errorf("failed to create apple container ops: %w", err)
	}
	imageService, err := hostops.NewAppleImageOps()
	if err != nil {
		return nil, fmt.Errorf("failed to create apple image ops: %w", err)
	}

	sb := &Boxer{
		appRoot:          appRoot,
		messenger:        hostops.NewTerminalMessenger(terminalWriter),
		sqlDB:            sqlDB,
		queries:          db.New(sqlDB),
		ContainerService: containerService,
		ImageService:     imageService,
		GitOps:           hostops.NewDefaultGitOps(),
		FileOps:          fileOps,
		SSHim:            sshim,
		AgentRegistry:    agentRegistry,
	}
	return sb, nil
}

func (sb *Boxer) Close() error {
	if sb.sqlDB != nil {
		return sb.sqlDB.Close()
	}
	return nil
}

func (sb *Boxer) newLifecycleService() *lifecycle.Service {
	return lifecycle.NewService(lifecycle.Deps{
		AppRoot:          sb.appRoot,
		ContainerService: sb.ContainerService,
		ImageService:     sb.ImageService,
		AgentRegistry:    sb.AgentRegistry,
		Store:            sb,
	})
}

// Sync tells Boxer to synchronize its internal database with the external states of
// the clone tool directory and local container service.
func (sb *Boxer) Sync(ctx context.Context) error {
	slog.InfoContext(ctx, "Boxer.Sync")
	// First, iterate through the sandbox records in the DB and update the its fiels to
	// reflect the current state of the filesystem clone root directory and container instance
	// states according to the local container service.
	sboxes, err := sb.queries.ListSandboxes(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.Sync ListSandboxes", "error", err)

		return err
	}

	// For each sandbox, update the status of its filesystem clone and its container instance.
	for _, dbBox := range sboxes {
		slog.InfoContext(ctx, "Boxer.Sync", "box", dbBox)
		box, err := sb.GetByID(ctx, dbBox.ID)
		if err != nil {
			return err
		}
		if err := sb.SyncBox(ctx, box); err != nil {
			slog.ErrorContext(ctx, "Boxer.Sync box.Sync", "error", err)
		}
	}
	return nil
}

func (b *Boxer) SyncBox(ctx context.Context, sb *sandtypes.Box) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	fi, err := os.Stat(sb.SandboxWorkDir)
	if err != nil || !fi.IsDir() {
		slog.ErrorContext(ctx, "Boxer.Sync SandboxWorkDir stat", "workdir", sb.SandboxWorkDir, "fi", fi, "error", err)
		sb.SandboxWorkDirError = "NO CLONE DIR"
	}

	return nil
}

func (b *Boxer) SyncHostGitMirror(ctx context.Context, sb *sandtypes.Box) (string, error) {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	if sb.HostOriginDir == "" {
		return "", fmt.Errorf("sandbox %s has no host origin directory", sb.ID)
	}
	hostGitTopLevel := b.GitOps.TopLevel(ctx, sb.HostOriginDir)
	if hostGitTopLevel == "" {
		return "", fmt.Errorf("sandbox %s was not created from a git repository", sb.ID)
	}
	mirror := cloning.NewGitMirror(filepath.Join(b.appRoot, "git-mirrors"), b.GitOps, b.FileOps)
	mirrorDir, err := mirror.EnsureUpdated(ctx, hostGitTopLevel)
	if err != nil {
		return "", err
	}
	if sb.ContainerID == "" || len(sb.Mounts) == 0 {
		b.hydrateMounts(sb, mirrorDir)
	}
	return mirrorDir, nil
}

func (b *Boxer) hydrateMounts(sb *sandtypes.Box, hostGitMirrorDir string) {
	pathRegistry := cloning.NewStandardPathRegistry(sb.SandboxWorkDir)
	baseConfig := containerruntime.NewBaseContainerConfiguration()
	sb.Mounts = baseConfig.GetMounts(containerruntime.Artifacts{
		HostGitMirrorDir:  hostGitMirrorDir,
		SandboxWorkDir:    sb.SandboxWorkDir,
		WorkDir:           pathRegistry.WorkDir(),
		DotfilesDir:       pathRegistry.DotfilesDir(),
		SSHKeysDir:        pathRegistry.SSHKeysDir(),
		Username:          sb.Username,
		Uid:               sb.Uid,
		SharedCacheMounts: sb.SharedCacheMounts,
	})
}

// NewSandboxOpts holds the parameters for creating a new sandbox.
type NewSandboxOpts struct {
	AgentType      string
	ID             string
	Name           string
	HostWorkDir    string
	ProfileName    string
	Profile        sandtypes.Profile
	ImageName      string
	EnvFile        string
	Username       string
	Uid            string
	AllowedDomains []string
	Mounts         []string
	CloneMounts    []string
	SharedCaches   sandtypes.SharedCacheConfig
	CPUs           int
	Memory         int
	LocalDomain    string
}

// NewSandbox creates a new sandbox based on a clone of hostWorkDir.
// TODO: clone envFile, if it exists, into the sandbox clone so agent-facing
// commands can keep using a stable copy even if the original file changes.
func (sb *Boxer) NewSandbox(ctx context.Context, opts NewSandboxOpts) (*sandtypes.Box, error) {
	ctx = sandboxlog.WithSandboxID(ctx, opts.ID)
	slog.InfoContext(ctx, "Boxer.NewSandbox", "hostWorkDir", opts.HostWorkDir, "id", opts.ID, "name", opts.Name, "agentType", opts.AgentType)
	if opts.ProfileName == "" {
		opts.ProfileName = sandtypes.DefaultProfileName
	}

	// Get agent configuration from registry
	agentConfig := sb.AgentRegistry.Get(opts.AgentType)
	envFile := opts.EnvFile
	if _, err := os.Stat(envFile); err != nil {
		envFile = ""
	}
	sharedCacheMounts, err := sb.ensureSharedCacheMounts(opts.SharedCaches, opts.LocalDomain)
	if err != nil {
		return nil, err
	}

	// Prepare workspace
	artifacts, err := agentConfig.Preparation.Prepare(ctx, cloning.CloneRequest{
		ID:                opts.ID,
		Name:              opts.Name,
		HostWorkDir:       opts.HostWorkDir,
		ProfileName:       opts.ProfileName,
		Profile:           opts.Profile,
		EnvFile:           envFile,
		Username:          opts.Username,
		Uid:               opts.Uid,
		SharedCacheMounts: sharedCacheMounts,
	})
	if err != nil {
		return nil, err
	}

	// Get mounts and hooks from configuration
	mounts := agentConfig.Configuration.GetMounts(runtimeArtifactsFromClone(artifacts))
	mountRequests, err := sb.prepareMountRequests(ctx, artifacts.PathRegistry, opts.Mounts, opts.CloneMounts)
	if err != nil {
		return nil, err
	}

	// TODO: move this to .Hydrate? Or make it a startup hook?
	sshKeysMountSpec, result, err, shouldReturn := sb.generateSSHKeysMountSpec(ctx, opts, artifacts)
	if shouldReturn {
		return result, err
	}

	// hostWorkDir may not be the same as the git root - should we save both here instead of
	// only saving the gitTopLevel?
	hostWorkDir := opts.HostWorkDir
	gitTopLevel := sb.GitOps.TopLevel(ctx, hostWorkDir)
	var gitRemote, gitBranch, gitCommit string
	var gitIsDirty bool
	slog.InfoContext(ctx, "NewSandbox", "gitTopLevel", gitTopLevel, "hostWorkDir", hostWorkDir)
	if gitTopLevel != "" {
		// Clone from git top level instead
		hostWorkDir = gitTopLevel
		gitRemote = sb.GitOps.RemoteURL(ctx, hostWorkDir, "origin")
		gitBranch = sb.GitOps.Branch(ctx, hostWorkDir)
		gitCommit = sb.GitOps.Commit(ctx, hostWorkDir)
		gitIsDirty = sb.GitOps.IsDirty(ctx, hostWorkDir)
		if artifacts.HostGitMirrorDir != "" {
			mirror := cloning.NewGitMirror(filepath.Join(sb.appRoot, "git-mirrors"), sb.GitOps, sb.FileOps)
			if err := mirror.WriteSnapshotRef(ctx, artifacts.HostGitMirrorDir, opts.ID, gitCommit); err != nil {
				slog.WarnContext(ctx, "failed to write sandbox creation snapshot ref",
					"mirror", artifacts.HostGitMirrorDir, "sandbox", opts.ID, "error", err)
			}
		}
	}

	ret := &sandtypes.Box{
		ID:                opts.ID,
		Name:              opts.Name,
		State:             "active",
		AgentType:         opts.AgentType,
		ProfileName:       opts.ProfileName,
		HostOriginDir:     hostWorkDir,
		SandboxWorkDir:    artifacts.SandboxWorkDir,
		ImageName:         opts.ImageName,
		DNSDomain:         opts.LocalDomain,
		EnvFile:           envFile,
		AllowedDomains:    opts.AllowedDomains,
		MountRequests:     mountRequests,
		SharedCacheMounts: sharedCacheMounts,
		Mounts:            append(mounts, sshKeysMountSpec),
		CPUs:              opts.CPUs,
		MemoryMB:          opts.Memory,
		Username:          opts.Username,
		Uid:               opts.Uid,
		OriginalGitDetails: &sandtypes.GitDetails{
			RemoteOrigin: gitRemote,
			Branch:       gitBranch,
			Commit:       gitCommit,
			IsDirty:      gitIsDirty,
		},
	}

	if err := sb.SaveSandbox(ctx, ret); err != nil {
		return nil, err
	}

	return ret, nil
}

func (sb *Boxer) generateSSHKeysMountSpec(ctx context.Context, opts NewSandboxOpts, artifacts *cloning.CloneArtifacts) (sandtypes.MountSpec, *sandtypes.Box, error, bool) {
	keys, err := sb.SSHim.NewKeys(ctx, sandboxSSHHostname(opts.Name, opts.LocalDomain), opts.Username)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.NewSandbox: sshim.NewKeys", "error", err)
		return sandtypes.MountSpec{}, nil, err, true
	}

	sshKeysMountSpec := sandtypes.MountSpec{
		Source:   filepath.Join(artifacts.SandboxWorkDir, "sshkeys"),
		Target:   "/sshkeys",
		ReadOnly: true,
	}

	// Write the data in keys fields to the container
	if err := sb.saveSSHKeys(sshKeysMountSpec.Source, keys); err != nil {
		return sandtypes.MountSpec{}, nil, fmt.Errorf("saveSSHKeys: %w", err), true
	}
	return sshKeysMountSpec, nil, nil, false
}

func sandboxSSHHostname(name, domain string) string {
	domain = strings.Trim(domain, ".")
	if domain == "" {
		domain = runtimedeps.DefaultDNSDomain
	}
	return name + "." + domain
}

func (sb *Boxer) saveSSHKeys(keysDir string, keys *sshimmer.Keys) error {
	if err := sb.FileOps.MkdirAll(keysDir, 0o750); err != nil {
		return err
	}
	hostPrivateKeyFile, err := sb.FileOps.Create(filepath.Join(keysDir, "ssh_host_key"))
	if err != nil {
		return err
	}
	defer hostPrivateKeyFile.Close()
	if _, err := hostPrivateKeyFile.Write(keys.HostKey); err != nil {
		return err
	}

	hostPublicKeyFile, err := sb.FileOps.Create(filepath.Join(keysDir, "ssh_host_key.pub"))
	if err != nil {
		return err
	}
	defer hostPublicKeyFile.Close()
	if _, err := hostPublicKeyFile.Write(keys.HostKeyPub); err != nil {
		return err
	}

	hostKeyCertFile, err := sb.FileOps.Create(filepath.Join(keysDir, "ssh_host_key.pub-cert"))
	if err != nil {
		return err
	}
	defer hostKeyCertFile.Close()
	if _, err := hostKeyCertFile.Write(keys.HostKeyCert); err != nil {
		return err
	}

	userCAFile, err := sb.FileOps.Create(filepath.Join(keysDir, "user_ca.pub"))
	if err != nil {
		return err
	}
	defer userCAFile.Close()
	if _, err := userCAFile.Write(keys.UserCAPub); err != nil {
		return err
	}

	return nil
}

// AttachSandbox re-connects to an existing container and sandboxWorkDir instead of creating a new one.
func (sb *Boxer) AttachSandbox(ctx context.Context, id string) (*sandtypes.Box, error) {
	ctx = sandboxlog.WithSandboxID(ctx, id)
	slog.InfoContext(ctx, "Boxer.AttachSandbox", "id", id)
	ret, err := sb.loadSandbox(ctx, id)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (sb *Boxer) List(ctx context.Context) ([]sandtypes.Box, error) {
	sandboxes, err := sb.queries.ListSandboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	boxes := make([]sandtypes.Box, len(sandboxes))
	for i, s := range sandboxes {
		box := sb.sandboxFromDB(&s)
		ctr, err := sb.GetContainer(ctx, box.ContainerID)
		if err != nil {
			box.SandboxContainerError = containerGetErrorMsg
		}
		box.Container = ctr
		box.CurrentGitDetails = sb.getCurrentGitDetails(ctx, box)
		boxes[i] = *box
	}
	return boxes, nil
}

func (sb *Boxer) Get(ctx context.Context, name string) (*sandtypes.Box, error) {
	slog.InfoContext(ctx, "Boxer.Get", "name", name)
	sandbox, err := sb.queries.GetActiveSandboxByName(ctx, name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox: %w", err)
	}

	box := sb.sandboxFromDB(&sandbox)
	ctr, err := sb.GetContainer(ctx, box.ContainerID)
	if err != nil {
		box.SandboxContainerError = containerGetErrorMsg
	}
	box.Container = ctr
	box.CurrentGitDetails = sb.getCurrentGitDetails(ctx, box)

	slog.InfoContext(ctx, "Boxer.Get", "ret", box)
	return box, nil
}

func (sb *Boxer) GetByID(ctx context.Context, id string) (*sandtypes.Box, error) {
	ctx = sandboxlog.WithSandboxID(ctx, id)
	slog.InfoContext(ctx, "Boxer.GetByID", "id", id)
	sandbox, err := sb.queries.GetSandboxByID(ctx, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox by id: %w", err)
	}
	box := sb.sandboxFromDB(&sandbox)
	ctr, err := sb.GetContainer(ctx, box.ContainerID)
	if err != nil {
		box.SandboxContainerError = containerGetErrorMsg
	}
	box.Container = ctr
	box.CurrentGitDetails = sb.getCurrentGitDetails(ctx, box)
	return box, nil
}

// sandboxNameRe enforces DNS label rules: 1-63 chars, lowercase alnum or hyphens,
// not starting or ending with a hyphen.
var sandboxNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validateSandboxName(name string) error {
	if !sandboxNameRe.MatchString(name) {
		return fmt.Errorf("sandbox name %q is invalid: must be 1-63 lowercase alphanumeric characters or hyphens, not starting or ending with a hyphen", name)
	}
	return nil
}

// RenameSandbox renames a stopped sandbox. The container is deleted and recreated
// under the new name (the container ID in apple's runtime IS the sandbox name).
// The git remote on the host is renamed best-effort; failures are logged but do not
// abort the rename.
func (sb *Boxer) RenameSandbox(ctx context.Context, oldName, newName string, progress io.Writer) (*sandtypes.Box, error) {
	sbox, err := sb.Get(ctx, oldName)
	if err != nil {
		return nil, err
	}
	if sbox == nil {
		return nil, fmt.Errorf("sandbox not found: %s", oldName)
	}
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)

	if sbox.Container != nil && sbox.Container.Status.State == "running" {
		return nil, fmt.Errorf("sandbox %s is running; stop it before renaming", oldName)
	}
	if err := validateSandboxName(newName); err != nil {
		return nil, err
	}

	enableSSHAgent := sbox.Container != nil && sbox.Container.Configuration.SSH
	oldRemoteName := sandboxRemoteName(sbox)
	keys, err := sb.SSHim.NewKeys(ctx, sandboxSSHHostname(newName, sbox.DNSDomain), sbox.Username)
	if err != nil {
		return nil, fmt.Errorf("generate ssh keys after rename: %w", err)
	}
	if err := sb.saveSSHKeys(cloning.NewStandardPathRegistry(sbox.SandboxWorkDir).SSHKeysDir(), keys); err != nil {
		return nil, fmt.Errorf("save ssh keys after rename: %w", err)
	}

	if sbox.ContainerID != "" {
		fmt.Fprintf(progress, "[sand] deleting container %s\n", sbox.ContainerID)
		if out, err := sb.ContainerService.Delete(ctx, nil, sbox.ContainerID); err != nil {
			slog.WarnContext(ctx, "Boxer.RenameSandbox delete old container", "containerID", sbox.ContainerID, "error", err, "output", out)
		}
	}

	sbox.Name = newName
	if err := sb.queries.RenameSandbox(ctx, db.RenameSandboxParams{Name: newName, ID: sbox.ID}); err != nil {
		return nil, fmt.Errorf("rename sandbox in db: %w", err)
	}

	fmt.Fprintf(progress, "[sand] creating container %s\n", newName)
	if err := sb.newLifecycleService().CreateContainer(ctx, sbox, enableSSHAgent); err != nil {
		return nil, fmt.Errorf("create container after rename: %w", err)
	}
	if err := sb.UpdateContainerID(ctx, sbox, sbox.ContainerID); err != nil {
		return nil, fmt.Errorf("update container id: %w", err)
	}

	if sbox.HostOriginDir != "" {
		oldRemote := cloning.ClonedWorkDirGitRemotePrefix + oldRemoteName
		newRemote := cloning.ClonedWorkDirGitRemotePrefix + newName
		fmt.Fprintf(progress, "[sand] renaming git remote %s -> %s\n", oldRemote, newRemote)
		if err := sb.GitOps.RenameRemote(ctx, sbox.HostOriginDir, oldRemote, newRemote); err != nil {
			slog.WarnContext(ctx, "Boxer.RenameSandbox rename git remote", "old", oldRemote, "new", newRemote, "error", err)
		}
	}

	return sbox, nil
}

func (sb *Boxer) SoftDelete(ctx context.Context, sbox *sandtypes.Box) error {
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)
	slog.InfoContext(ctx, "Boxer.SoftDelete", "id", sbox.ID, "name", sbox.Name)

	out, err := sb.ContainerService.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Stop", "error", err, "out", out)
	}

	out, err = sb.ContainerService.Delete(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete", "error", err, "out", out)
	}

	if err := sb.GitOps.RemoveRemote(ctx, sbox.HostOriginDir, cloning.ClonedWorkDirGitRemotePrefix+sandboxRemoteName(sbox)); err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete failed to remove git remote", "error", err)
	}

	trashWorkDir, err := sb.moveSandboxToTrash(ctx, sbox)
	if err != nil {
		return err
	}

	if err := sb.queries.SoftDeleteSandbox(ctx, db.SoftDeleteSandboxParams{
		ID:           sbox.ID,
		TrashWorkDir: toNullString(trashWorkDir),
	}); err != nil {
		return fmt.Errorf("failed to mark sandbox %s deleted in database: %w", sbox.ID, err)
	}

	return nil
}

func (sb *Boxer) ListDeleted(ctx context.Context) ([]sandtypes.Box, error) {
	sandboxes, err := sb.queries.ListDeletedSandboxes(ctx)
	if err != nil {
		return nil, err
	}
	ret := make([]sandtypes.Box, 0, len(sandboxes))
	for _, sandbox := range sandboxes {
		ret = append(ret, *sb.sandboxFromDB(&sandbox))
	}
	return ret, nil
}

func (sb *Boxer) Expunge(ctx context.Context, id string) error {
	ctx = sandboxlog.WithSandboxID(ctx, id)
	slog.InfoContext(ctx, "Boxer.Expunge", "id", id)

	sandbox, err := sb.queries.GetSandboxByID(ctx, id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("sandbox not found: %s", id)
	}
	if err != nil {
		return fmt.Errorf("failed to get sandbox by id: %w", err)
	}
	sbox := sb.sandboxFromDB(&sandbox)
	if sbox.State != "deleted" {
		return fmt.Errorf("sandbox %s is %s, not deleted", id, sbox.State)
	}
	if sbox.TrashWorkDir != "" {
		if err := sb.FileOps.RemoveAll(sbox.TrashWorkDir); err != nil {
			return fmt.Errorf("remove trashed sandbox workdir %s: %w", sbox.TrashWorkDir, err)
		}
	}
	if err := sb.queries.DeleteSandbox(ctx, id); err != nil {
		return fmt.Errorf("delete sandbox %s from database: %w", id, err)
	}
	return nil
}

func (sb *Boxer) Cleanup(ctx context.Context, sbox *sandtypes.Box) error {
	return sb.SoftDelete(ctx, sbox)
}

func (sb *Boxer) moveSandboxToTrash(ctx context.Context, sbox *sandtypes.Box) (string, error) {
	if sbox.SandboxWorkDir == "" {
		return "", nil
	}
	if _, err := sb.FileOps.Stat(sbox.SandboxWorkDir); errors.Is(err, os.ErrNotExist) {
		slog.InfoContext(ctx, "Boxer.SoftDelete workdir already missing", "workdir", sbox.SandboxWorkDir)
		return "", nil
	}
	trashWorkDir := filepath.Join(sb.appRoot, "trash", "sandboxes", sbox.ID)
	if err := sb.FileOps.MkdirAll(filepath.Dir(trashWorkDir), 0o750); err != nil {
		return "", fmt.Errorf("create trash directory for sandbox %s: %w", sbox.ID, err)
	}
	if err := sb.FileOps.Rename(sbox.SandboxWorkDir, trashWorkDir); err == nil {
		return trashWorkDir, nil
	} else {
		slog.InfoContext(ctx, "Boxer.SoftDelete rename to trash failed; falling back to copy", "from", sbox.SandboxWorkDir, "to", trashWorkDir, "error", err)
	}
	if err := sb.FileOps.Copy(ctx, sbox.SandboxWorkDir, trashWorkDir); err != nil {
		return "", fmt.Errorf("copy sandbox %s to trash: %w", sbox.ID, err)
	}
	if err := sb.FileOps.RemoveAll(sbox.SandboxWorkDir); err != nil {
		return "", fmt.Errorf("remove original sandbox workdir %s after trash copy: %w", sbox.SandboxWorkDir, err)
	}
	return trashWorkDir, nil
}

func (sb *Boxer) getCurrentGitDetails(ctx context.Context, box *sandtypes.Box) *sandtypes.GitDetails {
	currentGit := &sandtypes.GitDetails{}
	appDir := filepath.Join(box.SandboxWorkDir, "app")
	currentGit.Branch = sb.GitOps.Branch(ctx, appDir)
	currentGit.Commit = sb.GitOps.Commit(ctx, appDir)
	currentGit.IsDirty = sb.GitOps.IsDirty(ctx, appDir)

	return currentGit
}

func sandboxRemoteName(box *sandtypes.Box) string {
	if box.Name != "" {
		return box.Name
	}
	return box.ID
}

// Helper functions for converting between Box and db.Sandbox

func (sb *Boxer) sandboxFromDB(s *db.Sandbox) *sandtypes.Box {
	agentType := fromNullString(s.AgentType)
	if agentType == "" {
		agentType = "default" // Fallback for old sandboxes
	}
	name := s.Name
	if name == "" {
		name = s.ID
	}
	state := s.State
	if state == "" {
		state = "active"
	}
	profileName := fromNullString(s.ProfileName)
	if profileName == "" {
		profileName = sandtypes.DefaultProfileName
	}
	mountRequests := mountRequestsFromNullString(s.MountSpecs)

	return &sandtypes.Box{
		ID:                    s.ID,
		Name:                  name,
		State:                 state,
		AgentType:             agentType,
		ProfileName:           profileName,
		ContainerID:           fromNullString(s.ContainerID),
		ContainerBootstrapped: s.ContainerBootstrapped,
		HostOriginDir:         s.HostOriginDir,
		SandboxWorkDir:        s.SandboxWorkDir,
		ImageName:             s.ImageName,
		DNSDomain:             fromNullString(s.DnsDomain),
		EnvFile:               fromNullString(s.EnvFile),
		AllowedDomains:        domainsFromNullString(s.AllowedDomains),
		MountRequests:         mountRequests,
		OriginalGitDetails: &sandtypes.GitDetails{
			RemoteOrigin: fromNullString(s.OriginalGitOrigin),
			Branch:       fromNullString(s.OriginalGitBranch),
			Commit:       fromNullString(s.OriginalGitCommit),
			IsDirty:      s.OriginalGitIsDirty,
		},
		CPUs:     fromNullInt(s.Cpu),
		MemoryMB: fromNullInt(s.MemoryMb),
		Username: fromNullString(s.DefaultUsername),
		Uid:      fromNullString(s.DefaultUid),
		DeletedAt: func() time.Time {
			if s.DeletedAt.Valid {
				return s.DeletedAt.Time
			}
			return time.Time{}
		}(),
		TrashWorkDir: fromNullString(s.TrashWorkDir),
	}
}

func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func fromNullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func mountRequestsToNullString(requests []sandtypes.MountRequest) sql.NullString {
	if len(requests) == 0 {
		return sql.NullString{}
	}
	data, err := json.Marshal(requests)
	if err != nil {
		slog.Warn("failed to marshal mount requests", "error", err)
		return sql.NullString{}
	}
	return sql.NullString{String: string(data), Valid: true}
}

func mountRequestsFromNullString(ns sql.NullString) []sandtypes.MountRequest {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	var requests []sandtypes.MountRequest
	if err := json.Unmarshal([]byte(ns.String), &requests); err != nil {
		slog.Warn("failed to unmarshal mount requests", "error", err)
		return nil
	}
	return requests
}

func toNullInt(s int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(s), Valid: true}
}

func fromNullInt(ns sql.NullInt64) int {
	if ns.Valid {
		return int(ns.Int64)
	}
	return -1
}

func domainsToNullString(domains []string) sql.NullString {
	if len(domains) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.Join(domains, "\n"), Valid: true}
}

func domainsFromNullString(ns sql.NullString) []string {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	var domains []string
	for _, d := range strings.Split(ns.String, "\n") {
		if d != "" {
			domains = append(domains, d)
		}
	}
	return domains
}

func (sb *Boxer) getContainer(ctx context.Context, containerID string) (interface{}, error) {
	ctrs, err := sb.ContainerService.Inspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container for sandbox %s: %w", containerID, err)
	}
	if len(ctrs) == 0 {
		return nil, nil
	}

	return &ctrs[0], nil
}

func (sb *Boxer) GetContainer(ctx context.Context, containerID string) (*sandtypes.Container, error) {
	ctr, err := sb.getContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}
	if ctr == nil {
		return nil, nil
	}
	return ctr.(*sandtypes.Container), nil
}

func (sb *Boxer) GetContainerStats(ctx context.Context, containerID ...string) ([]sandtypes.ContainerStats, error) {
	stats, err := sb.ContainerService.Stats(ctx, containerID...)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (sb *Boxer) ensureSharedCacheMounts(cfg sandtypes.SharedCacheConfig, localDomain string) (sandtypes.SharedCacheMounts, error) {
	var mounts sandtypes.SharedCacheMounts

	if cfg.Mise {
		mounts.MiseCacheHostDir = filepath.Join(sb.appRoot, "caches", "mise")
		if err := sb.FileOps.MkdirAll(mounts.MiseCacheHostDir, 0o755); err != nil {
			return sandtypes.SharedCacheMounts{}, fmt.Errorf("create shared mise cache dir: %w", err)
		}
	}

	if cfg.APK {
		mounts.APKCacheHostDir = filepath.Join(sb.appRoot, "caches", "apk")
		if err := sb.FileOps.MkdirAll(mounts.APKCacheHostDir, 0o755); err != nil {
			return sandtypes.SharedCacheMounts{}, fmt.Errorf("create shared apk cache dir: %w", err)
		}
	}

	if cfg.Agents {
		mounts.AgentCacheHostDir = filepath.Join(sb.appRoot, "caches", "agents")
		if err := sb.FileOps.MkdirAll(mounts.AgentCacheHostDir, 0o755); err != nil {
			return sandtypes.SharedCacheMounts{}, fmt.Errorf("create shared agent cache dir: %w", err)
		}
	}

	if cfg.Bazel {
		mounts.BazelRemoteCacheURL = bazelRemoteCacheURL(localDomain)
	}

	if cfg.HTTPProxy {
		mounts.HTTPProxyURL = httpProxyCacheURL(localDomain)
		mounts.HTTPProxyCAHostPath = httpProxyCACertPath(sb.appRoot)
	}

	return mounts, nil
}

func bazelRemoteCacheURL(localDomain string) string {
	if localDomain == "" {
		localDomain = runtimedeps.DefaultDNSDomain
	}
	return "http://sand-bazel-cache." + strings.Trim(localDomain, ".") + ":8080"
}

func httpProxyCacheURL(localDomain string) string {
	if localDomain == "" {
		localDomain = runtimedeps.DefaultDNSDomain
	}
	return "http://sand-http-cache." + strings.Trim(localDomain, ".") + ":3128"
}

// EnsureImage makes sure the requested container image is present locally and up to date,
// pulling it if required. Progress messages are written to w.
func (sb *Boxer) EnsureImage(ctx context.Context, imageName string, w io.Writer) error {
	slog.InfoContext(ctx, "Boxer.EnsureImage", "imageName", imageName)
	progress := imageProgressSink(w)

	images, err := sb.ImageService.List(ctx)
	if err != nil {
		if runtimedeps.IsContainerSystemNotRunningError(err) {
			return runtimedeps.ContainerSystemNotRunningError(err)
		}
		return fmt.Errorf("failed to list images: %w", err)
	}

	imagePresent := false
	slog.InfoContext(ctx, "Boxer.EnsureImage", "image count", len(images))
	for _, image := range images {
		slog.InfoContext(ctx, "Boxer.EnsureImage", "image.Name", image.Configuration.Name)
		if image.Configuration.Name == imageName {
			slog.InfoContext(ctx, "Boxer.EnsureImage", "status", "already-present", "imageName", imageName)
			imagePresent = true
			break
		}
	}

	if !imagePresent {
		slog.InfoContext(ctx, "Boxer.EnsureImage", "status", "pulling", "imageName", imageName)
		return sb.pullImage(ctx, imageName, w)
	}

	// Image is present locally; for remote registry images, check for a newer digest.
	if strings.HasPrefix(imageName, "ghcr.io") || strings.HasPrefix(imageName, "docker.io") {
		localDigest, err := sb.localImageDigest(ctx, imageName)
		if err != nil {
			fmt.Fprintf(progress, "Failed to inspect local image %s, continuing with local version: %s\n", imageName, err)
			return nil
		}
		isLatest, err := runtimedeps.CheckImageDigestIsLatest(ctx, imageName, localDigest)
		if err != nil {
			fmt.Fprintf(progress, "Failed to check remote registry for latest version of %s, continuing with local version: %s\n", imageName, err)
		} else if !isLatest {
			fmt.Fprintf(progress, "Local image digest doesn't match latest remote digest, pulling %s\n", imageName)
			return sb.pullImage(ctx, imageName, w)
		}
	}

	return nil
}

func (sb *Boxer) localImageDigest(ctx context.Context, imageName string) (string, error) {
	imgs, err := sb.ImageService.Inspect(ctx, imageName)
	if err != nil {
		return "", err
	}
	if len(imgs) == 0 {
		return "", fmt.Errorf("not found in local registry: %s", imageName)
	}
	return imgs[0].Index.Digest, nil
}

// pullImage pulls imageName and writes progress messages to w.
func (sb *Boxer) pullImage(ctx context.Context, imageName string, w io.Writer) error {
	slog.InfoContext(ctx, "Boxer.pullImage", "imageName", imageName)
	progress := imageProgressSink(w)

	fmt.Fprintf(progress, "Pulling image %s...\n", imageName)
	start := time.Now()

	waitFn, err := sb.ImageService.Pull(ctx, imageName, progress)
	defer func() {
		if waitFn != nil {
			waitFn()
		}
	}()
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.pullImage", "error", err)
		return err
	}

	if waitFn != nil {
		if err := waitFn(); err != nil {
			slog.ErrorContext(ctx, "Boxer.pullImage wait", "error", err)
			return err
		}
	}

	fmt.Fprintf(progress, "Done pulling image. Took %v.\n", time.Since(start))
	return nil
}

func imageProgressSink(w io.Writer) imageprogress.Sink {
	if progress, ok := w.(imageprogress.Sink); ok {
		return progress
	}
	return imageprogress.NewTextSink(w)
}

// SaveSandbox persists the Sandbox to the database.
func (sb *Boxer) SaveSandbox(ctx context.Context, sbox *sandtypes.Box) error {
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)
	slog.InfoContext(ctx, "Boxer.SaveSandbox", "id", sbox.ID)
	if sbox.Name == "" {
		sbox.Name = sbox.ID
	}
	if sbox.State == "" {
		sbox.State = "active"
	}
	if sbox.ProfileName == "" {
		sbox.ProfileName = sandtypes.DefaultProfileName
	}
	upsertParams := db.UpsertSandboxParams{
		ID:                    sbox.ID,
		Name:                  sbox.Name,
		State:                 sbox.State,
		ContainerID:           toNullString(sbox.ContainerID),
		HostOriginDir:         sbox.HostOriginDir,
		SandboxWorkDir:        sbox.SandboxWorkDir,
		ImageName:             sbox.ImageName,
		DnsDomain:             toNullString(sbox.DNSDomain),
		EnvFile:               toNullString(sbox.EnvFile),
		AgentType:             toNullString(sbox.AgentType),
		ProfileName:           toNullString(sbox.ProfileName),
		AllowedDomains:        domainsToNullString(sbox.AllowedDomains),
		MountSpecs:            mountRequestsToNullString(sbox.MountRequests),
		ContainerBootstrapped: sbox.ContainerBootstrapped,
		Cpu:                   toNullInt(sbox.CPUs),
		MemoryMb:              toNullInt(sbox.MemoryMB),
		DefaultUsername:       toNullString(sbox.Username),
		DefaultUid:            toNullString(sbox.Uid),
		DeletedAt:             sql.NullTime{Time: sbox.DeletedAt, Valid: !sbox.DeletedAt.IsZero()},
		TrashWorkDir:          toNullString(sbox.TrashWorkDir),
	}
	if sbox.OriginalGitDetails != nil {
		upsertParams.OriginalGitOrigin = toNullString(sbox.OriginalGitDetails.RemoteOrigin)
		upsertParams.OriginalGitBranch = toNullString(sbox.OriginalGitDetails.Branch)
		upsertParams.OriginalGitCommit = toNullString(sbox.OriginalGitDetails.Commit)
		upsertParams.OriginalGitIsDirty = sbox.OriginalGitDetails.IsDirty
	}
	err := sb.queries.UpsertSandbox(ctx, upsertParams)
	if err != nil {
		return fmt.Errorf("failed to save sandbox: %w", err)
	}
	return nil
}

// UpdateContainerID updates the ContainerID field of a sandbox and persists it.
func (sb *Boxer) UpdateContainerID(ctx context.Context, sbox *sandtypes.Box, containerID string) error {
	sbox.ContainerID = containerID
	sbox.ContainerBootstrapped = false
	err := sb.queries.UpdateContainerID(ctx, db.UpdateContainerIDParams{
		ContainerID: toNullString(containerID),
		ID:          sbox.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to update container ID: %w", err)
	}
	return nil
}

func (sb *Boxer) UpdateContainerBootstrapped(ctx context.Context, sbox *sandtypes.Box, bootstrapped bool) error {
	sbox.ContainerBootstrapped = bootstrapped
	if err := sb.queries.UpdateContainerBootstrapped(ctx, db.UpdateContainerBootstrappedParams{
		ContainerBootstrapped: bootstrapped,
		ID:                    sbox.ID,
	}); err != nil {
		return fmt.Errorf("failed to update container bootstrap state: %w", err)
	}
	return nil
}

// StopContainer stops a sandbox's container without deleting it.
func (sb *Boxer) StopContainer(ctx context.Context, sbox *sandtypes.Box) error {
	ctx = sandboxlog.WithSandboxID(ctx, sbox.ID)
	if sbox.ContainerID == "" {
		return fmt.Errorf("sandbox %s has no container ID", sbox.ID)
	}

	out, err := sb.ContainerService.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.StopContainer", "containerID", sbox.ContainerID, "error", err, "out", out)
		return fmt.Errorf("failed to stop container for sandbox %s: %w", sbox.ID, err)
	}
	slog.InfoContext(ctx, "Boxer.StopContainer", "containerID", sbox.ContainerID, "out", out)
	return nil
}

// loadSandbox reads a Sandbox from the database.
func (sb *Boxer) loadSandbox(ctx context.Context, id string) (*sandtypes.Box, error) {
	ctx = sandboxlog.WithSandboxID(ctx, id)
	slog.InfoContext(ctx, "Boxer.loadSandbox", "id", id)

	sandbox, err := sb.queries.GetSandboxByID(ctx, id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("sandbox not found for id %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load sandbox: %w", err)
	}

	box := sb.sandboxFromDB(&sandbox)
	return box, nil
}
