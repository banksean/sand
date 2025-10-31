package sand

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/banksean/sand/db"
	"github.com/banksean/sand/sshimmer"
	"golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"
)

//go:generate sh -c "sqlc generate"

//go:embed db/schema.sql
var schemaSQL string

// Boxer manages the lifecycle of sandboxes.
type Boxer struct {
	appRoot          string
	messenger        UserMessenger
	sqlDB            *sql.DB
	queries          *db.Queries
	containerService ContainerOps
	imageService     ImageOps
	gitOps           GitOps
	fileOps          FileOps
}

func NewBoxer(appRoot string, terminalWriter io.Writer) (*Boxer, error) {
	fileOps := NewDefaultFileOps()
	if err := fileOps.MkdirAll(appRoot, 0o750); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(appRoot, "sand.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	// Enable WAL mode for better concurrency
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Initialize schema
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	sb := &Boxer{
		appRoot:          appRoot,
		messenger:        NewTerminalMessenger(terminalWriter),
		sqlDB:            sqlDB,
		queries:          db.New(sqlDB),
		containerService: NewAppleContainerOps(),
		imageService:     NewAppleImageOps(),
		gitOps:           NewDefaultGitOps(),
		fileOps:          fileOps,
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
// the clone rool directory and local container service.
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
func (sb *Boxer) NewSandbox(ctx context.Context, cloner WorkspaceCloner, id, hostWorkDir, imageName, envFile string) (*Box, error) {
	slog.InfoContext(ctx, "Boxer.NewSandbox", "hostWorkDir", hostWorkDir, "id", id)

	// TODO: move this to .Hydrate? Or make it a startup hook?
	sshim, err := sshimmer.NewLocalSSHimmer(ctx, id, id+".test", "22")
	slog.InfoContext(ctx, "Boxer.NewSandbox", "sshim", *sshim, "error", err)
	if err != nil {
		return nil, err
	}

	provisionResult, err := cloner.Prepare(ctx, CloneRequest{
		ID:          id,
		HostWorkDir: hostWorkDir,
		EnvFile:     envFile,
	})
	if err != nil {
		return nil, err
	}

	ret := &Box{
		ID:               id,
		HostOriginDir:    hostWorkDir,
		SandboxWorkDir:   provisionResult.SandboxWorkDir,
		ImageName:        imageName,
		EnvFile:          envFile,
		Mounts:           provisionResult.Mounts,
		ContainerHooks:   provisionResult.ContainerHooks,
		containerService: sb.containerService,
		sshim:            sshim,
	}

	if err := sb.SaveSandbox(ctx, ret); err != nil {
		return nil, err
	}

	return ret, nil
}

// AttachSandbox re-connects to an existing container and sandboxWorkDir instead of creating a new one.
func (sb *Boxer) AttachSandbox(ctx context.Context, id string) (*Box, error) {
	slog.InfoContext(ctx, "Boxer.AttachSandbox", "id", id)
	ret, err := sb.loadSandbox(ctx, id)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (sb *Boxer) List(ctx context.Context) ([]Box, error) {
	sandboxes, err := sb.queries.ListSandboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	boxes := make([]Box, len(sandboxes))
	for i, s := range sandboxes {
		box := sb.sandboxFromDB(&s)
		boxes[i] = *box
	}
	return boxes, nil
}

func (sb *Boxer) Get(ctx context.Context, id string) (*Box, error) {
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

func (sb *Boxer) Cleanup(ctx context.Context, sbox *Box) error {
	slog.InfoContext(ctx, "Boxer.Cleanup", "id", sbox.ID)

	out, err := sb.containerService.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Stop", "sandbox", sbox.ID, "error", err, "out", out)
	}

	out, err = sb.containerService.Delete(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete", "sandbox", sbox.ID, "error", err, "out", out)
	}

	if err := sb.gitOps.RemoveRemote(ctx, sbox.HostOriginDir, ClonedWorkDirRemotePrefix+sbox.ID); err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete failed to remove git remote", "sandbox", sbox.ID, "error", err)
	}

	if err := sb.fileOps.RemoveAll(sbox.SandboxWorkDir); err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete failed to remove workdir", "sandbox", sbox.ID, "error", err)
	}

	if sbox.sshim != nil {
		if err := sbox.sshim.Cleanup(); err != nil {
			slog.ErrorContext(ctx, "Boxer Containers.Delete failed to clean up ssh shim", "sandbox", sbox.ID, "error", err)
		}
	}

	// Finally, remove from database
	if err := sb.queries.DeleteSandbox(ctx, sbox.ID); err != nil {
		return fmt.Errorf("failed to delete sandbox %s from database: %w", sbox.ID, err)
	}

	return nil
}

// Helper functions for converting between Box and db.Sandbox

func (sb *Boxer) sandboxFromDB(s *db.Sandbox) *Box {
	return &Box{
		ID:               s.ID,
		ContainerID:      fromNullString(s.ContainerID),
		HostOriginDir:    s.HostOriginDir,
		SandboxWorkDir:   s.SandboxWorkDir,
		ImageName:        s.ImageName,
		DNSDomain:        fromNullString(s.DnsDomain),
		EnvFile:          fromNullString(s.EnvFile),
		containerService: sb.containerService,
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
func (sb *Boxer) SaveSandbox(ctx context.Context, sbox *Box) error {
	slog.InfoContext(ctx, "Boxer.SaveSandbox", "id", sbox.ID)

	err := sb.queries.UpsertSandbox(ctx, db.UpsertSandboxParams{
		ID:             sbox.ID,
		ContainerID:    toNullString(sbox.ContainerID),
		HostOriginDir:  sbox.HostOriginDir,
		SandboxWorkDir: sbox.SandboxWorkDir,
		ImageName:      sbox.ImageName,
		DnsDomain:      toNullString(sbox.DNSDomain),
		EnvFile:        toNullString(sbox.EnvFile),
	})
	if err != nil {
		return fmt.Errorf("failed to save sandbox: %w", err)
	}
	return nil
}

// UpdateContainerID updates the ContainerID field of a sandbox and persists it.
func (sb *Boxer) UpdateContainerID(ctx context.Context, sbox *Box, containerID string) error {
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
func (sb *Boxer) StopContainer(ctx context.Context, sbox *Box) error {
	if sbox.ContainerID == "" {
		return fmt.Errorf("sandbox %s has no container ID", sbox.ID)
	}

	out, err := sb.containerService.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.StopContainer", "sandbox", sbox.ID, "containerID", sbox.ContainerID, "error", err, "out", out)
		return fmt.Errorf("failed to stop container for sandbox %s: %w", sbox.ID, err)
	}
	slog.InfoContext(ctx, "Boxer.StopContainer", "sandbox", sbox.ID, "containerID", sbox.ContainerID, "out", out)
	return nil
}

// loadSandbox reads a Sandbox from the database.
func (sb *Boxer) loadSandbox(ctx context.Context, id string) (*Box, error) {
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

func writeKeyToFile(fileOps FileOps, keyBytes []byte, filename string) error {
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

func createKeyPairIfMissing(fileOps FileOps, idPath string) (ssh.PublicKey, error) {
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
