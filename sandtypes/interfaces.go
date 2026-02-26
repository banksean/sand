package sandtypes

import (
	"context"
	"os"
)

// UserMessenger provides a way to send messages to the user.
type UserMessenger interface {
	Message(ctx context.Context, msg string)
}

// GitOps provides git operations for workspace cloning.
type GitOps interface {
	AddRemote(ctx context.Context, dir, name, url string) error
	RemoveRemote(ctx context.Context, dir, name string) error
	Fetch(ctx context.Context, dir, remote string) error
	TopLevel(ctx context.Context, dir string) string
}

// FileOps provides file system operations.
type FileOps interface {
	MkdirAll(path string, perm os.FileMode) error
	Copy(ctx context.Context, src, dst string) error
	Stat(path string) (os.FileInfo, error)
	Lstat(path string) (os.FileInfo, error)
	Readlink(path string) (string, error)
	Create(path string) (*os.File, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	RemoveAll(path string) error
}
