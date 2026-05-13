package boxer

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/banksean/sand/internal/applecontainer/options"
	"github.com/banksean/sand/internal/applecontainer/types"
	"github.com/banksean/sand/internal/cloning"
	"github.com/banksean/sand/internal/db"
	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/banksean/sand/internal/runtimepaths"
	"github.com/banksean/sand/internal/sandboxlog"
	"github.com/banksean/sand/internal/sandtypes"
	"github.com/banksean/sand/internal/sshimmer"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "modernc.org/sqlite"
)

const containerGetErrorMsg = "[error getting]"

const innieSocketPermissionScript = `set -e
if [ -d /run/host-services ]; then
	chmod 755 /run/host-services
fi
for socket in /run/host-services/sandd.grpc.sock /run/host-services/sandd.sock; do
	if [ -e "$socket" ]; then
		chmod 666 "$socket"
	fi
done`

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
	AgentRegistry    *cloning.AgentRegistry
}

type hookExecutor struct {
	ctx         context.Context
	sandboxID   string
	containerID string
	container   hostops.ContainerOps
	progress    io.Writer
}

func (h hookExecutor) Exec(ctx context.Context, shellCmd string, args ...string) (string, error) {
	output, err := h.container.Exec(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: false,
				TTY:         true,
				WorkDir:     "/app",
			},
		}, h.containerID, shellCmd, os.Environ(), args...)
	if err != nil {
		slog.ErrorContext(h.ctx, "shell: containerService.Exec", "sandbox", h.sandboxID, "error", err, "output", output)
		return output, fmt.Errorf("failed to execute command for sandbox %s: %w", h.sandboxID, err)
	}
	return output, nil
}

func (h hookExecutor) ExecStream(ctx context.Context, stdout, stderr io.Writer, shellCmd string, args ...string) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	if h.progress != nil {
		stdout = io.MultiWriter(stdout, h.progress)
		stderr = io.MultiWriter(stderr, h.progress)
	}

	wait, err := h.container.ExecStream(ctx,
		&options.ExecContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: false,
				TTY:         true,
				WorkDir:     "/app",
			},
		}, h.containerID, shellCmd, os.Environ(),
		nil, stdout, stderr, args...)
	if err != nil {
		slog.ErrorContext(h.ctx, "shell: containerService.ExecStream", "sandbox", h.sandboxID, "error", err, "command", shellCmd)
		return fmt.Errorf("failed to start command for sandbox %s: %w", h.sandboxID, err)
	}
	if err := wait(); err != nil {
		slog.ErrorContext(h.ctx, "shell: containerService.ExecStream wait", "sandbox", h.sandboxID, "error", err, "command", shellCmd)
		return fmt.Errorf("failed to execute command for sandbox %s: %w", h.sandboxID, err)
	}
	return nil
}

func boxerStartHooks(hooks []sandtypes.ContainerHook) []sandtypes.ContainerHook {
	systemHooks := []sandtypes.ContainerHook{
		innieSocketPermissionHook(),
	}
	return append(systemHooks, hooks...)
}

func innieSocketPermissionHook() sandtypes.ContainerHook {
	return sandtypes.NewContainerHook("repair host service socket permissions", func(ctx context.Context, ctr *types.Container, exec sandtypes.HookStreamer) error {
		out, err := exec.Exec(ctx, "sh", "-c", innieSocketPermissionScript)
		if err != nil {
			if out != "" {
				return fmt.Errorf("repair host service socket permissions: %w: %s", err, strings.TrimSpace(out))
			}
			return fmt.Errorf("repair host service socket permissions: %w", err)
		}
		return nil
	})
}

// BoxerDeps holds the injectable dependencies for a Boxer.
// Fields left nil will cause panics if the corresponding Boxer methods are called.
type BoxerDeps struct {
	ContainerService hostops.ContainerOps
	ImageService     hostops.ImageOps
	GitOps           hostops.GitOps
	FileOps          hostops.FileOps
	SSHim            SSHimmer
	AgentRegistry    *cloning.AgentRegistry
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
		deps.AgentRegistry = cloning.NewAgentRegistry()
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
	agentRegistry := cloning.InitializeGlobalRegistry(appRoot, messenger, hostops.NewDefaultGitOps(), fileOps)

	sb := &Boxer{
		appRoot:          appRoot,
		messenger:        hostops.NewTerminalMessenger(terminalWriter),
		sqlDB:            sqlDB,
		queries:          db.New(sqlDB),
		ContainerService: hostops.NewAppleContainerOps(),
		ImageService:     hostops.NewAppleImageOps(),
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
	baseConfig := cloning.NewBaseContainerConfiguration()
	sb.Mounts = baseConfig.GetMounts(cloning.CloneArtifacts{
		HostWorkDir:       sb.HostOriginDir,
		HostGitMirrorDir:  hostGitMirrorDir,
		SandboxWorkDir:    sb.SandboxWorkDir,
		PathRegistry:      pathRegistry,
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
	Volumes        []string
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
	sharedCacheMounts, err := sb.ensureSharedCacheMounts(opts.SharedCaches)
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
	mounts := agentConfig.Configuration.GetMounts(*artifacts)

	// TODO: move this to .Hydrate? Or make it a startup hook?
	keys, err := sb.SSHim.NewKeys(ctx, opts.Name+"."+opts.LocalDomain, opts.Username)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.NewSanbox: sshim.Povision", "error", err)
		return nil, err
	}

	sshKeysMountSpec := sandtypes.MountSpec{
		Source:   filepath.Join(artifacts.SandboxWorkDir, "sshkeys"),
		Target:   "/sshkeys",
		ReadOnly: true,
	}

	// Write the data in keys fields to the container
	if err := sb.saveSSHKeys(sshKeysMountSpec.Source, keys); err != nil {
		return nil, fmt.Errorf("saveSSHKeys: %w", err)
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
		EnvFile:           envFile,
		AllowedDomains:    opts.AllowedDomains,
		Volumes:           opts.Volumes,
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
	sb.addHostBranchRelativeGitDetails(ctx, box, currentGit)

	return currentGit
}

func (sb *Boxer) addHostBranchRelativeGitDetails(ctx context.Context, box *sandtypes.Box, currentGit *sandtypes.GitDetails) {
	if box.HostOriginDir == "" || currentGit.Branch == "" || currentGit.Commit == "" {
		return
	}
	hostGitTopLevel := sb.GitOps.TopLevel(ctx, box.HostOriginDir)
	if hostGitTopLevel == "" {
		return
	}
	if !sb.GitOps.LocalBranchExists(ctx, hostGitTopLevel, currentGit.Branch) {
		return
	}
	sandboxRemote := cloning.ClonedWorkDirGitRemotePrefix + sandboxRemoteName(box)
	if err := sb.GitOps.Fetch(ctx, hostGitTopLevel, sandboxRemote); err != nil {
		slog.InfoContext(ctx, "Boxer.addHostBranchRelativeGitDetails fetch sandbox remote", "remote", sandboxRemote, "error", err)
	}
	ahead, behind, ok := sb.GitOps.CommitDivergence(ctx, hostGitTopLevel, "refs/heads/"+currentGit.Branch, currentGit.Commit)
	if ok {
		currentGit.HasRelative = true
		currentGit.Ahead = ahead
		currentGit.Behind = behind
	}
}

func sandboxRemoteName(box *sandtypes.Box) string {
	if box.Name != "" {
		return box.Name
	}
	return box.ID
}

func sandboxContainerName(box *sandtypes.Box) string {
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

	return &sandtypes.Box{
		ID:             s.ID,
		Name:           name,
		State:          state,
		AgentType:      agentType,
		ProfileName:    profileName,
		ContainerID:    fromNullString(s.ContainerID),
		HostOriginDir:  s.HostOriginDir,
		SandboxWorkDir: s.SandboxWorkDir,
		ImageName:      s.ImageName,
		DNSDomain:      fromNullString(s.DnsDomain),
		EnvFile:        fromNullString(s.EnvFile),
		AllowedDomains: domainsFromNullString(s.AllowedDomains),
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

func (sb *Boxer) GetContainer(ctx context.Context, containerID string) (*types.Container, error) {
	ctr, err := sb.getContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}
	if ctr == nil {
		return nil, nil
	}
	return ctr.(*types.Container), nil
}

func (sb *Boxer) GetContainerStats(ctx context.Context, containerID ...string) ([]types.ContainerStats, error) {
	stats, err := sb.ContainerService.Stats(ctx, containerID...)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (b *Boxer) EffectiveMounts(sb *sandtypes.Box) []sandtypes.MountSpec {
	if len(sb.Mounts) > 0 {
		return sb.Mounts
	}
	if sb.SandboxWorkDir == "" {
		return nil
	}

	// Fallback: reconstruct mounts from PathRegistry
	pathRegistry := cloning.NewStandardPathRegistry(sb.SandboxWorkDir)
	baseConfig := cloning.NewBaseContainerConfiguration()
	return baseConfig.GetMounts(cloning.CloneArtifacts{
		SandboxWorkDir:    sb.SandboxWorkDir,
		PathRegistry:      pathRegistry,
		SharedCacheMounts: sb.SharedCacheMounts,
	})
}

func (sb *Boxer) ensureSharedCacheMounts(cfg sandtypes.SharedCacheConfig) (sandtypes.SharedCacheMounts, error) {
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

	return mounts, nil
}

// CreateContainer creates a new container instance. The container image must exist.
func (sber *Boxer) CreateContainer(ctx context.Context, sb *sandtypes.Box, enableSSHAgent bool) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	mounts := sber.EffectiveMounts(sb)
	mountOpts := make([]string, 0, len(mounts))
	for _, m := range mounts {
		mountOpts = append(mountOpts, m.String())
	}

	volumeOpts := append([]string(nil), sb.Volumes...)
	volumeOpts = append(volumeOpts, runtimepaths.ContainerHTTPSocketPath(sb.ID)+":/run/host-services/sandd.sock")
	volumeOpts = append(volumeOpts, runtimepaths.ContainerGRPCSocketPath(sb.ID)+":/run/host-services/sandd.grpc.sock")

	mgmtOpts := options.ManagementOptions{
		Name:      sandboxContainerName(sb),
		SSH:       enableSSHAgent,
		DNSDomain: sb.DNSDomain,
		Remove:    false,
		Mount:     mountOpts,
		Volume:    volumeOpts,
	}
	resOpts := options.ResourceOptions{
		CPUs:   sb.CPUs,
		Memory: fmt.Sprintf("%dM", sb.MemoryMB),
	}
	if len(sb.AllowedDomains) > 0 {
		mgmtOpts.InitImage = runtimedeps.CustomInitImage
		mgmtOpts.DNS = "127.0.0.1"
		mgmtOpts.Kernel = filepath.Join(sber.appRoot, "kernel", runtimedeps.CustomKernelReleaseVersion, "vmlinux")
	}
	if err := sber.checkImageHasEntrypoint(ctx, sb.ImageName); err != nil {
		mgmtOpts.Entrypoint = "/bin/sh"
	}

	containerID, err := sber.ContainerService.Create(ctx,
		&options.CreateContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
			},
			ManagementOptions: mgmtOpts,
			ResourceOptions:   resOpts,
		},
		sb.ImageName, nil)
	if err != nil {
		slog.ErrorContext(ctx, "createContainer", "imageName", sb.ImageName, "error", err, "output", containerID)
		return fmt.Errorf("failed to create container for sandbox %s: %w", sb.ID, err)
	}

	sb.ContainerID = containerID
	return nil
}

func (sber *Boxer) RecreateContainer(ctx context.Context, sb *sandtypes.Box, enableSSHAgent bool) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	if sb.ContainerID != "" {
		out, err := sber.ContainerService.Stop(ctx, nil, sb.ContainerID)
		if err != nil {
			slog.WarnContext(ctx, "Boxer.RecreateContainer stop old container", "containerID", sb.ContainerID, "error", err, "output", out)
		}

		out, err = sber.ContainerService.Delete(ctx, nil, sb.ContainerID)
		if err != nil {
			return fmt.Errorf("delete old container for sandbox %s: %w", sb.ID, err)
		}
	}

	if err := sber.CreateContainer(ctx, sb, enableSSHAgent); err != nil {
		return err
	}
	if err := sber.UpdateContainerID(ctx, sb, sb.ContainerID); err != nil {
		return err
	}
	return nil
}

func (sber *Boxer) checkImageHasEntrypoint(ctx context.Context, imageName string) error {
	if imageName != "" {
		img, err := sber.ImageService.Inspect(ctx, imageName)
		if err != nil {
			return err
		}
		if len(img) == 0 {
			return fmt.Errorf("image not found: %s", imageName)
		}
		for _, v := range img[0].Variants {
			if len(v.Config.Config.Cmd) != 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("image %q has no command or entrypoint specified for container process", imageName)
}

// StartNewContainer starts a new container instance. The container must exist, and it should not be in the "running" state.
func (sber *Boxer) StartNewContainer(ctx context.Context, sb *sandtypes.Box, progress io.Writer) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	// Reconstruct runtime configuration from agent type
	pathRegistry := cloning.NewStandardPathRegistry(sb.SandboxWorkDir)

	artifacts := cloning.CloneArtifacts{
		SandboxWorkDir:    sb.SandboxWorkDir,
		PathRegistry:      pathRegistry,
		Username:          sb.Username,
		Uid:               sb.Uid,
		SharedCacheMounts: sb.SharedCacheMounts,
	}

	// Get agent config to reconstruct hooks
	agentConfig := sber.AgentRegistry.Get(sb.AgentType)
	hooks := boxerStartHooks(agentConfig.Configuration.GetFirstStartHooks(artifacts))

	slog.InfoContext(ctx, "Boxer.StartNewContainer", "box", *sb, "ContainerHooks", len(hooks))
	if err := sber.startContainerProcess(ctx, sb.ID, sb.ContainerID); err != nil {
		return err
	}

	return sber.executeHooks(ctx, sb, hooks, progress)
}

// StartExistingContainer starts an existing (previously-started) container instance.
// The container must exist, and it should be in the "stopped" state.
func (sber *Boxer) StartExistingContainer(ctx context.Context, sb *sandtypes.Box) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	// Reconstruct runtime configuration from agent type
	pathRegistry := cloning.NewStandardPathRegistry(sb.SandboxWorkDir)

	artifacts := cloning.CloneArtifacts{
		SandboxWorkDir:    sb.SandboxWorkDir,
		PathRegistry:      pathRegistry,
		Username:          sb.Username,
		Uid:               sb.Uid,
		SharedCacheMounts: sb.SharedCacheMounts,
	}

	// Get agent config to reconstruct hooks
	agentConfig := sber.AgentRegistry.Get(sb.AgentType)
	hooks := boxerStartHooks(agentConfig.Configuration.GetStartHooks(artifacts))

	slog.InfoContext(ctx, "Boxer.StartExistingContainer", "box", *sb, "ContainerHooks", len(hooks))
	if err := sber.startContainerProcess(ctx, sb.ID, sb.ContainerID); err != nil {
		return err
	}

	return sber.executeHooks(ctx, sb, hooks, nil)
}

func (sb *Boxer) startContainerProcess(ctx context.Context, sandboxID, containerID string) error {
	ctx = sandboxlog.WithSandboxID(ctx, sandboxID)
	slog.InfoContext(ctx, "Boxer.startContainerProcess", "containerID", containerID)
	output, err := sb.ContainerService.Start(ctx, nil, containerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainerProcess", "containerID", containerID, "error", err, "output", output)
		return fmt.Errorf("failed to start container for sandbox %s: %w", sandboxID, err)
	}
	slog.InfoContext(ctx, "Boxer.startContainerProcess succeeded", "output", output)
	return nil
}

func (sber *Boxer) executeHooks(ctx context.Context, sb *sandtypes.Box, hooks []sandtypes.ContainerHook, progress io.Writer) error {
	ctx = sandboxlog.WithSandboxID(ctx, sb.ID)
	var hookErrs []error
	for _, hook := range hooks {
		slog.InfoContext(ctx, "Boxer.executeHooks running hook", "hook", hook.Name())
		if progress != nil {
			fmt.Fprintf(progress, "[sand] %s\n", hook.Name())
		}
		// Need something that can call GetContaner and Exec on sb, since sb can no longer do those things.
		ctr, err := sber.GetContainer(ctx, sb.ContainerID)
		if err != nil {
			return err
		}
		exec := hookExecutor{
			ctx:         ctx,
			sandboxID:   sb.ID,
			containerID: sb.ContainerID,
			container:   sber.ContainerService,
			progress:    progress,
		}
		if err := hook.Run(ctx, ctr, exec); err != nil {
			slog.ErrorContext(ctx, "Boxer.executeHooks hook error", "hook", hook.Name(), "error", err)
			hookErrs = append(hookErrs, fmt.Errorf("%s: %w", hook.Name(), err))
		}
	}
	if len(hookErrs) > 0 {
		return errors.Join(hookErrs...)
	}
	return nil
}

// EnsureImage makes sure the requested container image is present locally and up to date,
// pulling it if required. Progress messages are written to w.
func (sb *Boxer) EnsureImage(ctx context.Context, imageName string, w io.Writer) error {
	slog.InfoContext(ctx, "Boxer.EnsureImage", "imageName", imageName)

	images, err := sb.ImageService.List(ctx)
	if err != nil {
		if runtimedeps.IsContainerSystemNotRunningError(err) {
			return runtimedeps.ContainerSystemNotRunningError(err)
		}
		return fmt.Errorf("failed to list images: %w", err)
	}

	imagePresent := false
	for _, image := range images {
		if image.Reference == imageName {
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
		isLatest, err := runtimedeps.CheckImageIsLatest(ctx, imageName)
		if err != nil {
			fmt.Fprintf(w, "Failed to check remote registry for latest version of %s, continuing with local version: %s\n", imageName, err)
		} else if !isLatest {
			fmt.Fprintf(w, "Local image digest doesn't match latest remote digest, pulling %s\n", imageName)
			return sb.pullImage(ctx, imageName, w)
		}
	}

	return nil
}

// pullImage pulls imageName and writes progress messages to w.
func (sb *Boxer) pullImage(ctx context.Context, imageName string, w io.Writer) error {
	slog.InfoContext(ctx, "Boxer.pullImage", "imageName", imageName)

	fmt.Fprintf(w, "Pulling image %s...\n", imageName)
	start := time.Now()

	waitFn, err := sb.ImageService.Pull(ctx, imageName, w)
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

	fmt.Fprintf(w, "Done pulling image. Took %v.\n", time.Since(start))
	return nil
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
		ID:              sbox.ID,
		Name:            sbox.Name,
		State:           sbox.State,
		ContainerID:     toNullString(sbox.ContainerID),
		HostOriginDir:   sbox.HostOriginDir,
		SandboxWorkDir:  sbox.SandboxWorkDir,
		ImageName:       sbox.ImageName,
		DnsDomain:       toNullString(sbox.DNSDomain),
		EnvFile:         toNullString(sbox.EnvFile),
		AgentType:       toNullString(sbox.AgentType),
		ProfileName:     toNullString(sbox.ProfileName),
		AllowedDomains:  domainsToNullString(sbox.AllowedDomains),
		Cpu:             toNullInt(sbox.CPUs),
		MemoryMb:        toNullInt(sbox.MemoryMB),
		DefaultUsername: toNullString(sbox.Username),
		DefaultUid:      toNullString(sbox.Uid),
		DeletedAt:       sql.NullTime{Time: sbox.DeletedAt, Valid: !sbox.DeletedAt.IsZero()},
		TrashWorkDir:    toNullString(sbox.TrashWorkDir),
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
	err := sb.queries.UpdateContainerID(ctx, db.UpdateContainerIDParams{
		ContainerID: toNullString(containerID),
		ID:          sbox.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to update container ID: %w", err)
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
