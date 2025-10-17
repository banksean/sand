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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ac "github.com/banksean/sand/applecontainer"
	"github.com/banksean/sand/db"
	"golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"
)

//go:generate sh -c "sqlc generate"

//go:embed db/schema.sql
var schemaSQL string

// Boxer manages the lifecycle of sandboxes.
type Boxer struct {
	appRoot        string
	sandBoxes      map[string]*Box
	terminalWriter io.Writer
	sqlDB          *sql.DB
	queries        *db.Queries
	provisioner    WorkspaceProvisioner
}

const hostKeyFilename = "ssh_host_ed25519_key"

func NewBoxer(appRoot string, terminalWriter io.Writer) (*Boxer, error) {
	if err := os.MkdirAll(appRoot, 0o750); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(appRoot, "sand.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
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

	// Generate ssh host key pair if it doesn't exist already.
	if _, err := createKeyPairIfMissing(filepath.Join(appRoot, hostKeyFilename)); err != nil {
		return nil, fmt.Errorf("could not create host key pair: %w", err)
	}

	sb := &Boxer{
		appRoot:        appRoot,
		sandBoxes:      map[string]*Box{},
		terminalWriter: terminalWriter,
		sqlDB:          sqlDB,
		queries:        db.New(sqlDB),
	}
	sb.provisioner = NewDefaultWorkspaceProvisioner(appRoot, terminalWriter)
	return sb, nil
}

func (sb *Boxer) Close() error {
	if sb.sqlDB != nil {
		return sb.sqlDB.Close()
	}
	return nil
}

// SetProvisioner allows callers (primarily tests) to swap the default workspace provisioner.
func (sb *Boxer) SetProvisioner(p WorkspaceProvisioner) {
	sb.provisioner = p
}

func (sb *Boxer) ensureProvisioner() {
	if sb.provisioner == nil {
		sb.provisioner = NewDefaultWorkspaceProvisioner(sb.appRoot, sb.terminalWriter)
	}
}

func (sb *Boxer) hydrateBox(ctx context.Context, box *Box) error {
	if box == nil {
		return fmt.Errorf("nil box to hydrate")
	}
	sb.ensureProvisioner()
	return sb.provisioner.Hydrate(ctx, box)
}

// NewSandbox creates a new sandbox based on a clone of hostWorkDir.
// TODO: clone envFile, if it exists, into the sandbox clone so every command exec'd in that sandbox container
// uses the same env file, even if the original .env file has changed on the host machine.
func (sb *Boxer) NewSandbox(ctx context.Context, id, hostWorkDir, imageName, envFile string) (*Box, error) {
	slog.InfoContext(ctx, "Boxer.NewSandbox", "hostWorkDir", hostWorkDir, "id", id)

	sb.ensureProvisioner()
	provisionResult, err := sb.provisioner.Prepare(ctx, ProvisionRequest{
		ID:          id,
		HostWorkDir: hostWorkDir,
		EnvFile:     envFile,
	})
	if err != nil {
		return nil, err
	}

	ret := &Box{
		ID:             id,
		HostOriginDir:  hostWorkDir,
		SandboxWorkDir: provisionResult.SandboxWorkDir,
		ImageName:      imageName,
		EnvFile:        envFile,
		Mounts:         provisionResult.Mounts,
		ContainerHooks: provisionResult.ContainerHooks,
	}
	sb.sandBoxes[id] = ret

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
		box := sandboxFromDB(&s)
		if err := sb.hydrateBox(ctx, box); err != nil {
			return nil, err
		}
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

	box := sandboxFromDB(&sandbox)
	if err := sb.hydrateBox(ctx, box); err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "Boxer.Get", "ret", box)
	return box, nil
}

func (sb *Boxer) Cleanup(ctx context.Context, sbox *Box) error {
	slog.InfoContext(ctx, "Boxer.Cleanup", "id", sbox.ID)

	out, err := ac.Containers.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Stop", "error", err, "out", out)
	}

	out, err = ac.Containers.Delete(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer Containers.Delete", "error", err, "out", out)
	}

	gitRmCloneRemote := exec.CommandContext(ctx, "git", "remote", "remove", ClonedWorkDirRemotePrefix+sbox.ID)
	gitRmCloneRemote.Dir = sbox.HostOriginDir
	slog.InfoContext(ctx, "Cleanup gitRmCloneRemote", "cmd", strings.Join(gitRmCloneRemote.Args, " "))
	output, err := gitRmCloneRemote.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "Cleanup gitRmCloneRemote", "error", err, "output", string(output))
		return err
	}

	if err := os.RemoveAll(sbox.SandboxWorkDir); err != nil {
		return err
	}

	// Finally, remove from database
	if err := sb.queries.DeleteSandbox(ctx, sbox.ID); err != nil {
		return fmt.Errorf("failed to delete sandbox from database: %w", err)
	}

	return nil
}

// Helper functions for converting between Box and db.Sandbox

func sandboxFromDB(s *db.Sandbox) *Box {
	return &Box{
		ID:             s.ID,
		ContainerID:    fromNullString(s.ContainerID),
		HostOriginDir:  s.HostOriginDir,
		SandboxWorkDir: s.SandboxWorkDir,
		ImageName:      s.ImageName,
		DNSDomain:      fromNullString(s.DnsDomain),
		EnvFile:        fromNullString(s.EnvFile),
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

	images, err := ac.Images.List(ctx)
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

	sb.userMsg(ctx, fmt.Sprintf("This may take a while: pulling container image %s...", imageName))
	start := time.Now()

	waitFn, err := ac.Images.Pull(ctx, imageName)
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

	sb.userMsg(ctx, fmt.Sprintf("Done pulling container image. Took %v.", time.Since(start)))
	return nil
}

func (sb *Boxer) userMsg(ctx context.Context, msg string) {
	if sb.terminalWriter == nil {
		slog.DebugContext(ctx, "userMsg (no terminalWriter)", "msg", msg)
		return
	}
	fmt.Fprintln(sb.terminalWriter, "\033[90m"+msg+"\033[0m")
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

	out, err := ac.Containers.Stop(ctx, nil, sbox.ContainerID)
	if err != nil {
		slog.ErrorContext(ctx, "Boxer.StopContainer", "error", err, "out", out)
		return err
	}
	slog.InfoContext(ctx, "Boxer.StopContainer", "id", sbox.ID, "containerID", sbox.ContainerID, "out", out)
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

	box := sandboxFromDB(&sandbox)
	if err := sb.hydrateBox(ctx, box); err != nil {
		return nil, err
	}

	return box, nil
}

func writeKeyToFile(keyBytes []byte, filename string) error {
	err := os.WriteFile(filename, keyBytes, 0o600)
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

func createKeyPairIfMissing(idPath string) (ssh.PublicKey, error) {
	if _, err := os.Stat(idPath); err == nil {
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

	err = writeKeyToFile(privateKeyPEM, idPath)
	if err != nil {
		return nil, fmt.Errorf("error writing private key to file %w", err)
	}
	pubKeyBytes := ssh.MarshalAuthorizedKey(sshPublicKey)

	err = writeKeyToFile([]byte(pubKeyBytes), idPath+".pub")
	if err != nil {
		return nil, fmt.Errorf("error writing public key to file %w", err)
	}
	return sshPublicKey, nil
}
