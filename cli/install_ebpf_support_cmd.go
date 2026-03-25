package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/banksean/sand/runtimedeps"
	"golang.org/x/sync/errgroup"
)

// InstallEBPFSupportCmd downloads a linux kernel binary that has been
// built with flags to enable BPFFS, which enables kernel-level packet
// filtering for sand's sandbox containers.
type InstallEBPFSupportCmd struct{}

func downloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func hashFileSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("hashFileSHA256 open: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("hashFileSHA256 close: %w", cerr)
		}
	}()

	hash := sha256.New()

	if _, err = io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hashFileSHA256 io.Copy: %w", err)
	}

	hashInBytes := hash.Sum(nil)
	hashString := fmt.Sprintf("%x", hashInBytes)

	return hashString, err
}

func (c *InstallEBPFSupportCmd) downloadKernel(cctx *CLIContext) error {
	kernelDir := filepath.Join(cctx.AppBaseDir, "kernel", runtimedeps.CustomKernelReleaseVersion)
	if err := os.MkdirAll(kernelDir, 0o750); err != nil {
		return fmt.Errorf("unable to create directory %s: %w", kernelDir, err)
	}
	kernelFile := filepath.Join(kernelDir, "vmlinux")
	_, err := os.Stat(kernelFile)
	if err != nil {
		downloadURI := fmt.Sprintf("https://github.com/banksean/containerization/releases/download/%s/vmlinux", runtimedeps.CustomKernelReleaseVersion)
		fmt.Printf("downloading kernel from: %s\n", downloadURI)
		if err := downloadFile(kernelFile, downloadURI); err != nil {
			return fmt.Errorf("unable to download %s: %w", downloadURI, err)
		}
	}

	hash, err := hashFileSHA256(kernelFile)
	if err != nil {
		return fmt.Errorf("unable to get sha256 of %s (you may need to manually remove it): %w", kernelFile, err)
	}
	if hash != runtimedeps.CustomKernelHash {
		return fmt.Errorf("sha256 check failed for %s (you may need to manually remove it), expected: %s, actual:%s", kernelFile, runtimedeps.CustomKernelHash, hash)
	}

	return nil
}

func (c *InstallEBPFSupportCmd) pullInitImage(cctx *CLIContext) error {
	pullCmd := exec.CommandContext(cctx.Context, "container", "image", "pull", runtimedeps.CustomInitImage)
	_, err := pullCmd.Output()
	return err
}

func (c *InstallEBPFSupportCmd) Run(cctx *CLIContext) error {
	g := errgroup.Group{}
	g.Go(func() error { return c.downloadKernel(cctx) })
	g.Go(func() error { return c.pullInitImage(cctx) })

	return g.Wait()
}
