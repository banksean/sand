package runtimepaths

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const socketNameHashLen = 16

func SocketRoot() string {
	return filepath.Join("/tmp", fmt.Sprintf("sand-%d", os.Getuid()))
}

func ContainerHTTPSocketDir() string {
	return filepath.Join(SocketRoot(), "cs")
}

func ContainerGRPCSocketDir() string {
	return filepath.Join(SocketRoot(), "cg")
}

func ContainerHTTPSocketPath(sandboxID string) string {
	return filepath.Join(ContainerHTTPSocketDir(), shortSocketName(sandboxID))
}

func ContainerGRPCSocketPath(sandboxID string) string {
	return filepath.Join(ContainerGRPCSocketDir(), shortSocketName(sandboxID))
}

func shortSocketName(sandboxID string) string {
	sum := sha256.Sum256([]byte(sandboxID))
	return hex.EncodeToString(sum[:])[:socketNameHashLen] + ".sock"
}
