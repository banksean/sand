package sand

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
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
	_ "modernc.org/sqlite"
)

//go:generate sh -c "sqlc generate"

//go:embed db/schema.sql
var schemaSQL string

// Boxer manages the lifecycle of sandboxes.
type Boxer struct {
	appRoot        string
	cloneRoot      string
	sandBoxes      map[string]*Box
	terminalWriter io.Writer
	sqlDB          *sql.DB
	queries        *db.Queries
}

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

	return &Boxer{
		appRoot:        appRoot,
		cloneRoot:      filepath.Join(appRoot, "clones"),
		sandBoxes:      map[string]*Box{},
		terminalWriter: terminalWriter,
		sqlDB:          sqlDB,
		queries:        db.New(sqlDB),
	}, nil
}

func (sb *Boxer) Close() error {
	if sb.sqlDB != nil {
		return sb.sqlDB.Close()
	}
	return nil
}

// NewSandbox creates a new sandbox based on a clone of hostWorkDir.
// TODO: clone envFile, if it exists, into sb.cloneRoot/id/env, so every command exec'd in that sandbox container
// uses the same env file, even if the original .env file has changed on the host machine.
func (sb *Boxer) NewSandbox(ctx context.Context, id, hostWorkDir, imageName, envFile string) (*Box, error) {
	slog.InfoContext(ctx, "Boxer.NewSandbox", "hostWorkDir", hostWorkDir, "id", id)

	if err := sb.cloneWorkDir(ctx, id, hostWorkDir); err != nil {
		return nil, err
	}

	if err := sb.cloneClaudeDir(ctx, id); err != nil {
		return nil, err
	}

	if err := sb.cloneDotfiles(ctx, id); err != nil {
		return nil, err
	}

	ret := &Box{
		ID:             id,
		HostOriginDir:  hostWorkDir,
		SandboxWorkDir: filepath.Join(sb.cloneRoot, id),
		ImageName:      imageName,
		EnvFile:        envFile,
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
		boxes[i] = *sandboxFromDB(&s)
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

const (
	originalWorkdDirRemoteName = "origin-host-workdir"
	ClonedWorkDirRemotePrefix  = "sand/"
)

// cloneWorkDir creates a recursive, copy-on-write copy of hostWorkDir, under the Boxer's root directory.
// "cp -c" uses APFS's clonefile(2) function to make the destination dir contents be COW.
// Git stuff:
// Set up bi-drectional "remotes" to link the two checkouts:
// - in cloneRoot/id/app, remote "origin-host-workdir" -> hostWorkDir
// - in hostWorkDir, remote "sand/<sandbox-id>" -> cloneRoot/id/app
// TODO: figure out how to deal with the inconsistency that the container's /app dir checkout now
// has remotes that point to host filesystem paths, not container filesystem paths.  This means
// "git fetch clonedfrom" works on the *host* OS, but not from inside the container, since those paths
// only exist on the host OS.
// We need to give the container some way to ask *something* that's running
// in the host OS to run the "git fetch clonedfrom" command in the cloneWorkDir
// on the container's behalf. This will update the sandbox clone's git checkout to reflect the latest
// contents of the host machine's working directory.
func (sb *Boxer) cloneWorkDir(ctx context.Context, id, hostWorkDir string) error {
	sb.userMsg(ctx, "Cloning "+hostWorkDir)
	if err := os.MkdirAll(filepath.Join(sb.cloneRoot, id), 0o750); err != nil {
		return err
	}
	hostCloneDir := filepath.Join(sb.cloneRoot, "/", id, "app")
	cpCmd := exec.CommandContext(ctx, "cp", "-Rc", hostWorkDir, hostCloneDir)
	slog.InfoContext(ctx, "cloneWorkDir cpCmd", "cmd", strings.Join(cpCmd.Args, " "))
	output, err := cpCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir cpCmd", "error", err, "output", output)
		return err
	}

	gitRemoteCloneToWorkDirCmd := exec.CommandContext(ctx, "git", "remote", "add", originalWorkdDirRemoteName, hostWorkDir)
	gitRemoteCloneToWorkDirCmd.Dir = hostCloneDir
	slog.InfoContext(ctx, "cloneWorkDir gitRemoteCloneToWorkDirCmd", "cmd", strings.Join(gitRemoteCloneToWorkDirCmd.Args, " "))
	output, err = gitRemoteCloneToWorkDirCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitRemoteCloneToWorkDirCmd", "error", err, "output", string(output))
		return err
	}

	gitRemoteWorkDirToCloneCmd := exec.CommandContext(ctx, "git", "remote", "add", ClonedWorkDirRemotePrefix+id, hostCloneDir)
	gitRemoteWorkDirToCloneCmd.Dir = hostWorkDir
	slog.InfoContext(ctx, "cloneWorkDir gitRemoteWorkDirToCloneCmd", "cmd", strings.Join(gitRemoteWorkDirToCloneCmd.Args, " "))
	output, err = gitRemoteWorkDirToCloneCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitRemoteWorkDirToCloneCmd", "error", err, "output", string(output))
		return err
	}

	gitFetchCloneToWorkDirCmd := exec.CommandContext(ctx, "git", "fetch", originalWorkdDirRemoteName)
	gitFetchCloneToWorkDirCmd.Dir = hostCloneDir
	slog.InfoContext(ctx, "cloneWorkDir gitFetchCloneToWorkDirCmd", "cmd", strings.Join(gitFetchCloneToWorkDirCmd.Args, " "))
	output, err = gitFetchCloneToWorkDirCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitFetchCloneToWorkDirCmd", "error", err, "output", string(output))
		return err
	}

	gitFetchWorkDirToCloneCmd := exec.CommandContext(ctx, "git", "fetch", ClonedWorkDirRemotePrefix+id)
	gitFetchWorkDirToCloneCmd.Dir = hostWorkDir
	slog.InfoContext(ctx, "cloneWorkDir gitFetchWorkDirToCloneCmd", "cmd", strings.Join(gitFetchWorkDirToCloneCmd.Args, " "))
	output, err = gitFetchWorkDirToCloneCmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneWorkDir gitFetchWorkDirToCloneCmd", "error", err, "output", string(output))
		return err
	}

	return nil
}

func (sb *Boxer) cloneClaudeDir(ctx context.Context, id string) error {
	if err := os.MkdirAll(filepath.Join(sb.cloneRoot, id), 0o750); err != nil {
		return err
	}
	cloneClaude := filepath.Join(sb.cloneRoot, "/", id, "dotfiles")
	dotClaude := filepath.Join(os.Getenv("HOME"), ".claude")
	if _, err := os.Stat(dotClaude); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(cloneClaude)
		if err != nil {
			return err
		}
		defer f.Close()
		return nil
	}
	cmd := exec.CommandContext(ctx, "cp", "-Rc", dotClaude, cloneClaude)
	slog.InfoContext(ctx, "cloneClaudeDir", "cmd", strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.InfoContext(ctx, "cloneClaudeDir", "error", err, "output", string(output))
		return err
	}

	return nil
}

func (sb *Boxer) cloneDotfiles(ctx context.Context, id string) error {
	sb.userMsg(ctx, "Cloning dotfiles...")
	dotfiles := []string{
		".claude.json",
		".gitconfig",
		".p10k.zsh",
		".zshrc",
		".omp.json",
		".ssh/id_ed25519.pub",
	}
	if err := os.MkdirAll(filepath.Join(sb.cloneRoot, id, "dotfiles"), 0o750); err != nil {
		return err
	}
	for _, dotfile := range dotfiles {
		clone := filepath.Join(sb.cloneRoot, "/", id, "dotfiles", dotfile)
		original := filepath.Join(os.Getenv("HOME"), dotfile)
		fi, err := os.Lstat(original)
		if errors.Is(err, os.ErrNotExist) {
			sb.userMsg(ctx, "skipping "+original)
			f, err := os.Create(clone)
			if err != nil {
				return err
			}
			f.Close()
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			destination, err := os.Readlink(original)
			if err != nil {
				slog.ErrorContext(ctx, "Boxer.cloneDotfiles error reading symbolic link", "original", original, "error", err)
				continue
			}
			if !filepath.IsAbs(destination) {
				destination = filepath.Join(os.Getenv("HOME"), destination)
			}
			// Now verify that the file that the symlink points to actually exists.
			_, err = os.Lstat(destination)
			if errors.Is(err, os.ErrNotExist) {
				slog.ErrorContext(ctx, "Boxer.cloneDotfiles symbolic link points to nonexistent file",
					"original", original, "destination", destination, "error", err)
				f, err := os.Create(clone)
				if err != nil {
					return err
				}
				f.Close()
				continue
			}
			slog.InfoContext(ctx, "Boxer.cloneDotfiles resolved symbolic link",
				"original", original, "destination", destination)
			original = destination
		}
		cloneDir := filepath.Dir(clone)
		if err := os.MkdirAll(cloneDir, 0o750); err != nil {
			slog.ErrorContext(ctx, "cloneDotfiles couldn't make clone dir", "cloneDir", cloneDir, "error", err)
			return err
		}
		cmd := exec.CommandContext(ctx, "cp", "-Rc", original, clone)
		slog.InfoContext(ctx, "cloneDotfiles", "cmd", strings.Join(cmd.Args, " "))
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.InfoContext(ctx, "cloneDotfiles", "error", err, "output", output)
			return err
		}
		sb.userMsg(ctx, "cloned "+original)
	}

	return nil
}

func (sb *Boxer) pullImage(ctx context.Context, imageName string) error {
	slog.InfoContext(ctx, "Boxer.pullImage", "imageName", imageName)
	sb.userMsg(ctx, fmt.Sprintf("This may take a while: pulling container image %s...", imageName))
	start := time.Now()
	wait, err := ac.Images.Pull(ctx, imageName)
	if err != nil {
		slog.ErrorContext(ctx, "pullImage: Images.Pull", "error", err)
		return err
	}

	err = wait()
	if err == nil {
		sb.userMsg(ctx, fmt.Sprintf("Done pulling container image. Took %v.", time.Since(start)))
	}

	return err
}

func (sb *Boxer) EnsureImage(ctx context.Context, imageName string) error {
	manifests, err := ac.Images.Inspect(ctx, imageName)
	if err != nil {
		slog.ErrorContext(ctx, "checkForImage: Images.Inspect", "error", err, "manifests", len(manifests))
		if imageName != DefaultImageName {
			return err
		}
		return sb.pullImage(ctx, imageName)
	}
	if len(manifests) == 0 {
		return fmt.Errorf("no images named %s ", imageName)
	}
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

	return sandboxFromDB(&sandbox), nil
}
