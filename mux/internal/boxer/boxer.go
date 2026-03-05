package boxer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/banksean/sand/applecontainer/options"
	"github.com/banksean/sand/applecontainer/types"
	"github.com/banksean/sand/box"
	"github.com/banksean/sand/cloning"
	"github.com/banksean/sand/db"
	"github.com/banksean/sand/sandtypes"
	"github.com/banksean/sand/sshimmer"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
	"golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"
)

// Boxer manages the lifecycle of sandboxes.
type Boxer struct {
	appRoot          string
	messenger        box.UserMessenger
	sqlDB            *sql.DB
	queries          *db.Queries
	ContainerService box.ContainerOps
	imageService     box.ImageOps
	gitOps           box.GitOps
	fileOps          box.FileOps
	sshim            *sshimmer.LocalSSHimmer
	agentRegistry    *cloning.AgentRegistry
}

func NewBoxer(appRoot string, terminalWriter io.Writer) (*Boxer, error) {
	fileOps := box.NewDefaultFileOps()
	if err := fileOps.MkdirAll(appRoot, 0o750); err != nil {
		return nil, err
	}

	sqlDB, err := db.Connect(appRoot)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	sshim, err := sshimmer.NewLocalSSHimmer(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create LocalSSHimmer: %w", err)
	}

	// Initialize global agent registry
	messenger := box.NewTerminalMessenger(terminalWriter)
	agentRegistry := cloning.InitializeGlobalRegistry(appRoot, messenger, box.NewDefaultGitOps(), fileOps)

	sb := &Boxer{
		appRoot:          appRoot,
		messenger:        box.NewTerminalMessenger(terminalWriter),
		sqlDB:            sqlDB,
		queries:          db.New(sqlDB),
		ContainerService: box.NewAppleContainerOps(),
		imageService:     box.NewAppleImageOps(),
		gitOps:           box.NewDefaultGitOps(),
		fileOps:          fileOps,
		sshim:            sshim,
		agentRegistry:    agentRegistry,
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
		box, err := sb.Get(ctx, dbBox.ID)
		if err != nil {
			return err
		}
		if err := box.Sync(ctx); err != nil {
			slog.ErrorContext(ctx, "Boxer.Sync box.Sync", "error", err)
		}
	}
	return nil
}

// NewSandbox creates a new sandbox based on a clone of hostWorkDir.
// TODO: clone envFile, if it exists, into the sandbox clone so every command exec'd in that sandbox container
// uses the same env file, even if the original .env file has changed on the host machine.
func (sb *Boxer) NewSandbox(ctx context.Context, agentType, id, hostWorkDir, imageName, envFile string) (*box.Box, error) {
	slog.InfoContext(ctx, "Boxer.NewSandbox", "hostWorkDir", hostWorkDir, "id", id, "agentType", agentType)

	// Get agent configuration from registry
	agentConfig := sb.agentRegistry.Get(agentType)

	// Prepare workspace
	artifacts, err := agentConfig.Preparation.Prepare(ctx, cloning.CloneRequest{
		ID:          id,
		HostWorkDir: hostWorkDir,
		EnvFile:     envFile,
	})
	if err != nil {
		return nil, err
	}

	// Get mounts and hooks from configuration
	mounts := agentConfig.Configuration.GetMounts(*artifacts)
	hooks := agentConfig.Configuration.GetStartupHooks(*artifacts)

	// TODO: move this to .Hydrate? Or make it a startup hook?
	keys, err := sb.sshim.NewKeys(ctx, id+".test")
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.NewSanbox: sshim.Povision", "error", err)
		return nil, err
	}
	// TODO: save the data in keys fields to sandboxWorkDir (or to the db)?

	// TODO: write the data in keys fields to the container
	sshKeysMountSpec := sandtypes.MountSpec{
		Source:   filepath.Join(artifacts.SandboxWorkDir, "sshkeys"),
		Target:   "/sshkeys",
		ReadOnly: true,
	}
	if err := sb.saveSSHKeys(sshKeysMountSpec.Source, keys); err != nil {
		return nil, fmt.Errorf("saveSSHKeys: %w", err)
	}
	ret := &box.Box{
		ID:             id,
		AgentType:      agentType,
		HostOriginDir:  hostWorkDir,
		SandboxWorkDir: artifacts.SandboxWorkDir,
		ImageName:      imageName,
		EnvFile:        envFile,
		Mounts:         append(mounts, sshKeysMountSpec),
		ContainerHooks: hooks,
		Keys:           keys,
		//ContainerService: sb.ContainerService,
	}

	if err := sb.SaveSandbox(ctx, ret); err != nil {
		return nil, err
	}

	return ret, nil
}

func (sb *Boxer) saveSSHKeys(keysDir string, keys *sshimmer.Keys) error {
	if err := sb.fileOps.MkdirAll(keysDir, 0o750); err != nil {
		return err
	}
	hostPrivateKeyFile, err := sb.fileOps.Create(filepath.Join(keysDir, "ssh_host_key"))
	if err != nil {
		return err
	}
	defer hostPrivateKeyFile.Close()
	if _, err := hostPrivateKeyFile.Write(keys.HostKey); err != nil {
		return err
	}

	hostPublicKeyFile, err := sb.fileOps.Create(filepath.Join(keysDir, "ssh_host_key.pub"))
	if err != nil {
		return err
	}
	defer hostPublicKeyFile.Close()
	if _, err := hostPublicKeyFile.Write(keys.HostKeyPub); err != nil {
		return err
	}

	hostKeyCertFile, err := sb.fileOps.Create(filepath.Join(keysDir, "ssh_host_key.pub-cert"))
	if err != nil {
		return err
	}
	defer hostKeyCertFile.Close()
	if _, err := hostKeyCertFile.Write(keys.HostKeyCert); err != nil {
		return err
	}

	userCAFile, err := sb.fileOps.Create(filepath.Join(keysDir, "user_ca.pub"))
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
func (sb *Boxer) AttachSandbox(ctx context.Context, id string) (*box.Box, error) {
	slog.InfoContext(ctx, "Boxer.AttachSandbox", "id", id)
	ret, err := sb.loadSandbox(ctx, id)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (sb *Boxer) List(ctx context.Context) ([]box.Box, error) {
	sandboxes, err := sb.queries.ListSandboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	boxes := make([]box.Box, len(sandboxes))
	for i, s := range sandboxes {
		box := sb.sandboxFromDB(&s)
		boxes[i] = *box
	}
	return boxes, nil
}

func (sb *Boxer) Get(ctx context.Context, id string) (*box.Box, error) {
	slog.InfoContext(ctx, "Boxer.Get", "id", id)
	sandbox, err := sb.queries.GetSandbox(ctx, id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox: %w", err)
	}

	box := sb.sandboxFromDB(&sandbox)
	slog.InfoContext(ctx, "Boxer.Get", "ret", box)
	return box, nil
}

func (sb *Boxer) Cleanup(ctx context.Context, sbox *box.Box) error {
	slog.InfoContext(ctx, "Boxer.Cleanup", "id", sbox.ID)

	out, err := sb.ContainerService.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Stop", "sandbox", sbox.ID, "error", err, "out", out)
	}

	out, err = sb.ContainerService.Delete(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete", "sandbox", sbox.ID, "error", err, "out", out)
	}

	if err := sb.gitOps.RemoveRemote(ctx, sbox.HostOriginDir, cloning.ClonedWorkDirGitRemotePrefix+sbox.ID); err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete failed to remove git remote", "sandbox", sbox.ID, "error", err)
	}

	if err := sb.fileOps.RemoveAll(sbox.SandboxWorkDir); err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete failed to remove workdir", "sandbox", sbox.ID, "error", err)
	}

	// Finally, remove from database
	if err := sb.queries.DeleteSandbox(ctx, sbox.ID); err != nil {
		return fmt.Errorf("failed to delete sandbox %s from database: %w", sbox.ID, err)
	}

	return nil
}

// Helper functions for converting between Box and db.Sandbox

func (sb *Boxer) sandboxFromDB(s *db.Sandbox) *box.Box {
	agentType := fromNullString(s.AgentType)
	if agentType == "" {
		agentType = "default" // Fallback for old sandboxes
	}
	return &box.Box{
		ID:             s.ID,
		AgentType:      agentType,
		ContainerID:    fromNullString(s.ContainerID),
		HostOriginDir:  s.HostOriginDir,
		SandboxWorkDir: s.SandboxWorkDir,
		ImageName:      s.ImageName,
		DNSDomain:      fromNullString(s.DnsDomain),
		EnvFile:        fromNullString(s.EnvFile),
		//ContainerService: sb.ContainerService,
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

// TODO: Move this code to mux/internal/boxer#Boxer.GetContainer and change callers of this function to use mux.MuxClient instead
// GetContainer returns the container.
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

// CreateContainer creates a new container instance. The container image must exist.
func (sber *Boxer) CreateContainer(ctx context.Context, sb *box.Box) error {
	mounts := sb.EffectiveMounts()
	mountOpts := make([]string, 0, len(mounts))
	for _, m := range mounts {
		mountOpts = append(mountOpts, m.String())
	}

	containerID, err := sber.ContainerService.Create(ctx,
		&options.CreateContainer{
			ProcessOptions: options.ProcessOptions{
				Interactive: true,
				TTY:         true,
				EnvFile:     sb.EnvFile,
			},
			ManagementOptions: options.ManagementOptions{
				// TODO: Try to name the container after the sandbox, and handle collisions
				// if the name is already in use (e.g. append random chars to sb.ID).
				Name:      sb.ID,
				SSH:       true,
				DNSDomain: sb.DNSDomain,
				Remove:    false,
				Mount:     mountOpts,
			},
		},
		sb.ImageName, nil)
	if err != nil {
		slog.ErrorContext(ctx, "createContainer", "sandbox", sb.ID, "imageName", sb.ImageName, "error", err, "output", containerID)
		return fmt.Errorf("failed to create container for sandbox %s: %w", sb.ID, err)
	}
	sb.ContainerID = containerID
	return nil
}

// StartContainer starts a container instance. The container must exist, and it should not be in the "running" state.
func (sber *Boxer) StartContainer(ctx context.Context, sb *box.Box) error {
	// Reconstruct runtime configuration from agent type
	pathRegistry := cloning.NewStandardPathRegistry(sb.SandboxWorkDir)
	artifacts := cloning.CloneArtifacts{
		SandboxWorkDir: sb.SandboxWorkDir,
		PathRegistry:   pathRegistry,
	}

	// Get agent config to reconstruct hooks
	agentConfig := cloning.GetGlobalRegistry().Get(sb.AgentType)
	hooks := agentConfig.Configuration.GetStartupHooks(artifacts)

	slog.InfoContext(ctx, "Box.StartContainer", "box", *sb, "ContainerHooks", len(hooks))
	if err := sber.startContainerProcess(ctx, sb.ContainerID); err != nil {
		return err
	}

	return sber.executeHooks(ctx, sb, hooks)
}

func (sb *Boxer) startContainerProcess(ctx context.Context, containerID string) error {
	slog.InfoContext(ctx, "Box.startContainerProcess", "containerID", containerID)
	output, err := sb.ContainerService.Start(ctx, nil, containerID)
	if err != nil {
		slog.ErrorContext(ctx, "startContainerProcess", "containerID", containerID, "error", err, "output", output)
		return fmt.Errorf("failed to start container for sandbox %s: %w", containerID, err)
	}
	slog.InfoContext(ctx, "Box.startContainerProcess succeeded", "sandbox", containerID, "output", output)
	return nil
}

func (sber *Boxer) executeHooks(ctx context.Context, sb *box.Box, hooks []sandtypes.ContainerStartupHook) error {
	var hookErrs []error
	for _, hook := range hooks {
		slog.InfoContext(ctx, "Box.executeHooks running hook", "hook", hook.Name())
		// Need something that can call GetContaner and Exec on sb, since sb can no longer do those things.
		ctr, err := sber.GetContainer(ctx, sb.ContainerID)
		if err != nil {
			return err
		}
		if err := hook.OnStart(ctx, ctr, func(ctx context.Context, shellCmd string, args ...string) (string, error) {
			output, err := sber.ContainerService.Exec(ctx,
				&options.ExecContainer{
					ProcessOptions: options.ProcessOptions{
						Interactive: false,
						TTY:         true,
						WorkDir:     "/app",
						EnvFile:     sb.EnvFile,
					},
				}, sb.ContainerID, shellCmd, os.Environ(), args...)
			if err != nil {
				slog.ErrorContext(ctx, "shell: containerService.Exec", "sandbox", sb.ID, "error", err, "output", output)
				return output, fmt.Errorf("failed to execute command for sandbox %s: %w", sb.ID, err)
			}
			return output, nil
		}); err != nil {
			slog.ErrorContext(ctx, "Box.executeHooks hook error", "hook", hook.Name(), "error", err)
			hookErrs = append(hookErrs, fmt.Errorf("%s: %w", hook.Name(), err))
		}
	}
	if len(hookErrs) > 0 {
		return errors.Join(hookErrs...)
	}
	return nil
}

// EnsureImage makes sure the requested container image is present locally, pulling it if required.
func (sb *Boxer) EnsureImage(ctx context.Context, imageName string) error {
	slog.InfoContext(ctx, "Boxer.EnsureImage", "imageName", imageName)

	images, err := sb.imageService.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	for _, image := range images {
		if image.Reference == imageName {
			slog.InfoContext(ctx, "Boxer.EnsureImage", "status", "already-present", "imageName", imageName)
			return nil
		}
	}

	slog.InfoContext(ctx, "Boxer.EnsureImage", "status", "pulling", "imageName", imageName)
	return sb.pullImage(ctx, imageName)
}

// pullImage wraps the apple container image pull helper with user feedback.
func (sb *Boxer) pullImage(ctx context.Context, imageName string) error {
	slog.InfoContext(ctx, "Boxer.pullImage", "imageName", imageName)

	sb.messenger.Message(ctx, fmt.Sprintf("This may take a while: pulling container image %s...", imageName))
	start := time.Now()

	waitFn, err := sb.imageService.Pull(ctx, imageName)
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

	sb.messenger.Message(ctx, fmt.Sprintf("Done pulling container image. Took %v.", time.Since(start)))
	return nil
}

// SaveSandbox persists the Sandbox to the database.
func (sb *Boxer) SaveSandbox(ctx context.Context, sbox *box.Box) error {
	slog.InfoContext(ctx, "Boxer.SaveSandbox", "id", sbox.ID)

	err := sb.queries.UpsertSandbox(ctx, db.UpsertSandboxParams{
		ID:             sbox.ID,
		ContainerID:    toNullString(sbox.ContainerID),
		HostOriginDir:  sbox.HostOriginDir,
		SandboxWorkDir: sbox.SandboxWorkDir,
		ImageName:      sbox.ImageName,
		DnsDomain:      toNullString(sbox.DNSDomain),
		EnvFile:        toNullString(sbox.EnvFile),
		AgentType:      toNullString(sbox.AgentType),
	})
	if err != nil {
		return fmt.Errorf("failed to save sandbox: %w", err)
	}
	return nil
}

// UpdateContainerID updates the ContainerID field of a sandbox and persists it.
func (sb *Boxer) UpdateContainerID(ctx context.Context, sbox *box.Box, containerID string) error {
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
func (sb *Boxer) StopContainer(ctx context.Context, sbox *box.Box) error {
	if sbox.ContainerID == "" {
		return fmt.Errorf("sandbox %s has no container ID", sbox.ID)
	}

	out, err := sb.ContainerService.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.StopContainer", "sandbox", sbox.ID, "containerID", sbox.ContainerID, "error", err, "out", out)
		return fmt.Errorf("failed to stop container for sandbox %s: %w", sbox.ID, err)
	}
	slog.InfoContext(ctx, "Boxer.StopContainer", "sandbox", sbox.ID, "containerID", sbox.ContainerID, "out", out)
	return nil
}

// loadSandbox reads a Sandbox from the database.
func (sb *Boxer) loadSandbox(ctx context.Context, id string) (*box.Box, error) {
	slog.InfoContext(ctx, "Boxer.loadSandbox", "id", id)

	sandbox, err := sb.queries.GetSandbox(ctx, id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("sandbox not found for id %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load sandbox: %w", err)
	}

	box := sb.sandboxFromDB(&sandbox)

	return box, nil
}

func writeKeyToFile(fileOps box.FileOps, keyBytes []byte, filename string) error {
	err := fileOps.WriteFile(filename, keyBytes, 0o600)
	return err
}

func genHostKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	return publicKey, privateKey, err
}

// encodePrivateKeyToPEM encodes an Ed25519 private key for storage
func encodePrivateKeyToPEM(privateKey ed25519.PrivateKey) []byte {
	// No need to create a signer first, we can directly marshal the key

	// Format and encode as a binary private key format
	pkBytes, err := ssh.MarshalPrivateKey(privateKey, "sketch key")
	if err != nil {
		panic(fmt.Sprintf("failed to marshal private key: %v", err))
	}

	// Return PEM encoded bytes
	return pem.EncodeToMemory(pkBytes)
}

func createKeyPairIfMissing(fileOps box.FileOps, idPath string) (ssh.PublicKey, error) {
	if _, err := fileOps.Stat(idPath); err == nil {
		return nil, nil
	}

	publicKey, privateKey, err := genHostKeyPair()
	if err != nil {
		return nil, fmt.Errorf("error generating key pair: %w", err)
	}

	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("error converting to SSH public key: %w", err)
	}

	privateKeyPEM := encodePrivateKeyToPEM(privateKey)

	err = writeKeyToFile(fileOps, privateKeyPEM, idPath)
	if err != nil {
		return nil, fmt.Errorf("error writing private key to file %w", err)
	}
	pubKeyBytes := ssh.MarshalAuthorizedKey(sshPublicKey)

	err = writeKeyToFile(fileOps, []byte(pubKeyBytes), idPath+".pub")
	if err != nil {
		return nil, fmt.Errorf("error writing public key to file %w", err)
	}
	return sshPublicKey, nil
}
