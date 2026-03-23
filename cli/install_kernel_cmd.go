package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

// InstallKernelCmd configures the container command to use specific linux kernel binary that has been
// built with flags to enable BPFFS (which enables kernel-level packet filtering for sand's
// sandbox containers).
type InstallKernelCmd struct{}

const (
	customKernelReleaseVersion = "v0.0.1"
	customKernelHash           = "fce4baecf9f814d0dc17e55c185f25b49bd462b61c81fb8520e306990b0c65c1"
)

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

func (c *InstallKernelCmd) Run(cctx *CLIContext) error {
	// TODO: Check which kernel is currently installed. Apple's container command
	// doesn't provide a way to get this informatiojn, as of this writing.
	downloadURI := fmt.Sprintf("https://github.com/banksean/containerization/releases/download/%s/vmlinux", customKernelReleaseVersion)
	kernelDir := filepath.Join(cctx.AppBaseDir, "kernel", customKernelReleaseVersion)
	if err := os.MkdirAll(kernelDir, 0o750); err != nil {
		return fmt.Errorf("unable to create directory %s: %w", kernelDir, err)
	}
	kernelFile := filepath.Join(kernelDir, "vmlinux")
	_, err := os.Stat(kernelFile)
	if err != nil {
		fmt.Printf("downloading kernel from: %s\n", downloadURI)
		if err := downloadFile(kernelFile, downloadURI); err != nil {
			return fmt.Errorf("unable to download %s: %w", downloadURI, err)
		}
	} else {
		fmt.Printf("found exsiting kernel download, checking sha256: %v\n", kernelFile)
		hash, err := hashFileSHA256(kernelFile)
		if err != nil {
			return fmt.Errorf("unable to get sha256 of %s (you may need to manually remove it): %w", kernelFile, err)
		}
		if hash != customKernelHash {
			return fmt.Errorf("sha256 check failed for %s (you may need to manually remove it), expected: %s, actual:%s", kernelFile, customKernelHash, hash)
		}
	}

	kernelSetOut, err := exec.Command("container", "system", "kernel", "set", "--arch", "arm64", "--binary", kernelFile, "--force").CombinedOutput()
	if err != nil {
		return fmt.Errorf("unable to create exec.Cmd %w: %s", err, kernelSetOut)
	}
	fmt.Printf("Installation succeeded. You can reset to Apple's default container kernel by running the following command:\n\n\tcontainer system kernel set --recommended\n\n")

	return nil
}
