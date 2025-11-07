package sshimmer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
)

/**
TODO: sort out all the .pub-cert vs -cert.pub shit. ssh expects one but no the other when deriving the cert filename from the private key filename.
*/

// Keys is the set of ssh keys and certificates to be installed on a newly created sandbox container.
type Keys struct {
	HostKey     []byte // host private key
	HostKeyPub  []byte // host public key
	HostKeyCert []byte // host key certificate
	UserCAPub   []byte // public key for user certificate authority
}

type LocalSSHimmer struct {
	localDomain string

	knownHostsPath   string
	userIdentityPath string
	userIdentity     []byte

	hostCAPath      string
	hostCA          ssh.Signer
	hostCAPublicKey ssh.PublicKey

	userCAPath      string
	userCertPath    string
	userCertificate []byte
	userCA          ssh.Signer
	userCAPublicKey ssh.PublicKey

	fs FileSystem
	kg KeyGenerator
}

// NewLocalSSHimmer will set up everything so that you can use ssh on localhost to connect to
// a local sand container without Trust On First Use (TOFU). To achive this, LocalSSHimmer uses its own
// certificate authorities to sign user and host certificates. Sand will configure ssh (and sshd inside
// containers) so that it relies on certificate-based two-way authentication.  This ensures that your
// ssh client can verify that it's connecting to the container sshd that you think it is, and also that
// the container sshd can verify that it's you connecting to it.
//
// This CA-based approach requires minimal changes to your ~/.ssh/config.
// It adds a single Include line, once, automatically the first time you use sand.
// Everything else (host CA, user CA and user identity keys) is maintained by updating files in
// ~/.config/sand.
func NewLocalSSHimmer(ctx context.Context) (*LocalSSHimmer, error) {
	return newLocalSSHimmerWithDeps(ctx, &RealFileSystem{}, &RealKeyGenerator{})
}

// newLocalSSHimmerWithDeps creates a new LocalSSHimmer with the specified dependencies
func newLocalSSHimmerWithDeps(ctx context.Context, fs FileSystem, kg KeyGenerator) (*LocalSSHimmer, error) {
	base := filepath.Join(os.Getenv("HOME"), ".config", "sand")
	if _, err := fs.Stat(base); err != nil {
		if err := fs.MkdirAll(base, 0o777); err != nil {
			return nil, fmt.Errorf("couldn't create %s: %w", base, err)
		}
	}

	s := &LocalSSHimmer{
		localDomain:      "test", // TODO: pass this in.
		knownHostsPath:   filepath.Join(base, "known_hosts"),
		userIdentityPath: filepath.Join(base, "user_key"),

		hostCAPath:   filepath.Join(base, "host_ca"),
		userCAPath:   filepath.Join(base, "user_ca"),
		userCertPath: filepath.Join(base, "user_cert"),
		fs:           fs,
		kg:           kg,
	}

	// Load or create the host CA
	slog.DebugContext(ctx, "newLocalSSHimmerWithDeps", "getOrCreateCA userCAPath", s.userCAPath)
	userCASigner, userCAPublicKey, err := s.getOrCreateCA(s.userCAPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't get user CA from %s: %w", s.userCAPath, err)
	}
	s.userCA = userCASigner
	s.userCAPublicKey = userCAPublicKey

	// Load or create the user keypair
	userPubKey, _, err := s.getOrCreateKeyPair(s.userIdentityPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't create user identity from %s: %w", s.userIdentityPath, err)
	}

	// Issue a user certificate (TODO: skip this if the user key cert file already exits)
	userCert, err := s.issueUserCertificate(userPubKey)
	if err != nil {
		return nil, fmt.Errorf("couldn't issue user cert: %w", err)
	}
	s.userCertificate = userCert.Marshal()
	userCertBytes := ssh.MarshalAuthorizedKey(userCert)
	s.writeKeyToFile(userCertBytes, s.userIdentityPath+"-cert.pub")
	if err := writeSandSSHConfig(s.fs); err != nil {
		return nil, fmt.Errorf("writeSandSSHConfig: %w", err)
	}
	// Load or create the host CA
	slog.InfoContext(ctx, "newLocalSSHimmerWithDeps", "getOrCreateCA hostCAPath", s.hostCAPath)
	hostCASigner, hostCAPublicKey, err := s.getOrCreateCA(s.hostCAPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't get host CA from %s: %w", s.hostCAPath, err)
	}
	s.hostCA = hostCASigner
	s.hostCAPublicKey = hostCAPublicKey
	if err := s.addHostCAToKnownHosts(); err != nil {
		return nil, fmt.Errorf("addHostCAToKnownHosts: %w", err)
	}

	return s, nil
}

func (s *LocalSSHimmer) NewKeys(ctx context.Context, hostName string) (*Keys, error) {
	privateKey, publicKey, err := s.kg.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("error generating key pair: %w", err)
	}

	hostPubKey, err := s.kg.ConvertToSSHPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("error converting to SSH public key: %w", err)
	}

	hostPrivKey := encodePrivateKeyToPEM(privateKey)

	// Issue a host certificate
	hostCert, err := s.issueHostCertificate(hostName, hostPubKey)
	if err != nil {
		return nil, fmt.Errorf("couldn't issue host cert: %w", err)
	}

	ret := &Keys{
		HostKey:     hostPrivKey,
		HostKeyPub:  ssh.MarshalAuthorizedKey(hostPubKey),
		HostKeyCert: ssh.MarshalAuthorizedKey(hostCert),
		UserCAPub:   ssh.MarshalAuthorizedKey(s.userCAPublicKey),
	}
	return ret, nil
}

func (s *LocalSSHimmer) writeKeyToFile(keyBytes []byte, filename string) error {
	return s.fs.WriteFile(filename, keyBytes, 0o600)
}

// TODO: return ssh.Signer instead of []byte for the private key?
func (s *LocalSSHimmer) getOrCreateKeyPair(idPath string) (ssh.PublicKey, []byte, error) {
	// TODO: fix this - it should read the key pair from these files if they exist, rather than return nils.
	if _, err := s.fs.Stat(idPath); err == nil {
		pubkeyBytes, err := s.fs.ReadFile(idPath + ".pub")
		if err != nil {
			return nil, nil, fmt.Errorf("reading public key from %s: %w", idPath+".pub", err)
		}
		slog.Debug("getOrCreateKeyPair", "pubkeyBytes", string(pubkeyBytes))
		pubkey, _, _, _, err := ssh.ParseAuthorizedKey(pubkeyBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing public key from %s: %w", idPath+".pub", err)
		}
		privateKeyBytes, err := s.fs.ReadFile(idPath)
		if err != nil {
			return nil, nil, fmt.Errorf("reading private key from %s: %w", idPath, err)
		}
		//privKey, err := ssh.ParsePrivateKey(privateKeyBytes)

		return pubkey, privateKeyBytes, nil
	}

	privateKey, publicKey, err := s.kg.GenerateKeyPair()
	if err != nil {
		return nil, nil, fmt.Errorf("error generating key pair: %w", err)
	}

	sshPublicKey, err := s.kg.ConvertToSSHPublicKey(publicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting to SSH public key: %w", err)
	}

	privateKeyPEM := encodePrivateKeyToPEM(privateKey)

	err = s.writeKeyToFile(privateKeyPEM, idPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error writing private key to file %w", err)
	}
	pubKeyBytes := ssh.MarshalAuthorizedKey(sshPublicKey)

	err = s.writeKeyToFile([]byte(pubKeyBytes), idPath+".pub")
	if err != nil {
		return nil, nil, fmt.Errorf("error writing public key to file %w", err)
	}
	return sshPublicKey, privateKeyPEM, nil
}

func (s *LocalSSHimmer) issueHostCertificate(hostName string, certPub ssh.PublicKey) (*ssh.Certificate, error) {
	// Create a new certificate
	cert := &ssh.Certificate{
		Key:             certPub,
		Serial:          1,
		CertType:        ssh.HostCert,
		KeyId:           hostName + " host key",
		ValidPrincipals: []string{hostName},                             // Only valid for root user in container
		ValidAfter:      uint64(time.Now().Add(-24 * time.Hour).Unix()), // Valid from 1 day ago
		ValidBefore:     uint64(time.Now().Add(720 * time.Hour).Unix()), // Valid for 30 days
		Permissions: ssh.Permissions{
			Extensions: map[string]string{
				"permit-pty":              "",
				"permit-agent-forwarding": "",
				"permit-port-forwarding":  "",
			},
		},
	}
	// Sign the certificate with the host CA
	if err := cert.SignCert(rand.Reader, s.hostCA); err != nil {
		return nil, fmt.Errorf("signing host certificate: %w", err)
	}

	return cert, nil
}

func (c *LocalSSHimmer) addHostCAToKnownHosts() error {
	// Instead of adding individual host entries, we'll use a CA-based approach
	// by adding a single "@cert-authority" entry

	// Format the CA public key line for the known_hosts file
	var caPublicKeyLine string
	if c.hostCAPublicKey != nil {
		// Create a line that trusts only localhost hosts with a certificate signed by our CA
		// This restricts the CA authority to only localhost addresses for security
		caLine := "@cert-authority *." + c.localDomain + " " + string(ssh.MarshalAuthorizedKey(c.hostCAPublicKey))
		caPublicKeyLine = strings.TrimSpace(caLine)
	}

	// Read existing known_hosts content or start with empty if the file doesn't exist
	outputLines := []string{}
	existingContent, err := c.fs.ReadFile(c.knownHostsPath)
	if err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(existingContent))
		for scanner.Scan() {
			line := scanner.Text()
			// Skip existing CA lines to avoid duplicates
			if caPublicKeyLine != "" && strings.HasPrefix(line, "@cert-authority * ") {
				continue
			}
			// Skip existing host key lines for this host:port
			// if strings.Contains(line, c.sshHost+":"+c.sshPort) {
			// 	continue
			// }
			outputLines = append(outputLines, line)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("couldn't read known_hosts file: %w", err)
	}

	// Add the CA public key line if available
	if caPublicKeyLine != "" {
		outputLines = append(outputLines, caPublicKeyLine)
	}

	// Safely write the updated content to the file
	if err := c.fs.SafeWriteFile(c.knownHostsPath, []byte(strings.Join(outputLines, "\n")), 0o644); err != nil {
		return fmt.Errorf("couldn't safely write updated known_hosts to %s: %w", c.knownHostsPath, err)
	}

	return nil
}

func (s *LocalSSHimmer) issueUserCertificate(certPub ssh.PublicKey) (*ssh.Certificate, error) {
	// Create a new user certificate
	cert := &ssh.Certificate{
		Key:             certPub,
		Serial:          1,
		CertType:        ssh.UserCert,
		KeyId:           "sand-user",
		ValidPrincipals: []string{"root"},                               // Only valid for root user in container
		ValidAfter:      uint64(time.Now().Add(-24 * time.Hour).Unix()), // Valid from 1 day ago
		ValidBefore:     uint64(time.Now().Add(720 * time.Hour).Unix()), // Valid for 30 days
		Permissions: ssh.Permissions{
			Extensions: map[string]string{
				"permit-pty":              "",
				"permit-agent-forwarding": "",
				"permit-port-forwarding":  "",
			},
		},
	}

	slog.Debug("s.userCA", "ca", s.userCA, "publicKey", s.userCA.PublicKey())
	if err := cert.SignCert(rand.Reader, s.userCA); err != nil {
		return nil, fmt.Errorf("signing user certificate : %w", err)
	}

	return cert, nil
}

// getOrCreateCA creates a new certificate authority keypair at path.
func (s *LocalSSHimmer) getOrCreateCA(path string) (ssh.Signer, ssh.PublicKey, error) {
	// Check if CA keypair already exists
	if _, err := s.fs.Stat(path); err == nil {
		// CA keypair exists, verify it's still valid
		caPrivKeyPEM, err := s.fs.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("reading CA file %s: %w", path, err)
		}

		// Parse certificate to check validity
		privKey, err := ssh.ParsePrivateKey(caPrivKeyPEM)
		if err != nil {
			// Invalid certificate, something went wrong and we can't recover from it here.
			return nil, nil, err
		} else {
			return privKey, privKey.PublicKey(), nil
		}
		// Otherwise, certificate is invalid or expired, so fall through and regenerate it
	}

	pri, pub, err := s.kg.GenerateKeyPair()
	if err != nil {
		return nil, nil, fmt.Errorf("generating key pair: %w", err)
	}

	// Write the CA public key. First convert to ssh public key:
	caPublicKey, err := s.kg.ConvertToSSHPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("convertiung to ssh public key: %w", err)
	}
	// Then write the converted public key.
	caPubKeyBytes := ssh.MarshalAuthorizedKey(caPublicKey)
	if err := s.writeKeyToFile(caPubKeyBytes, path+".pub"); err != nil {
		return nil, nil, fmt.Errorf("writing CA public key to file: %w", err)
	}

	// Write the CA private key
	caPrivKeyPEM := encodePrivateKeyToPEM(pri)
	if err := s.writeKeyToFile(caPrivKeyPEM, path); err != nil {
		return nil, nil, fmt.Errorf("writing CA private key to file: %w", err)
	}

	// Create a signer from the private key
	caSigner, err := ssh.NewSignerFromKey(pri)
	if err != nil {
		return nil, nil, fmt.Errorf("creating CA signer from private key: %w", err)
	}

	return caSigner, caPublicKey, nil
}

func checkSSHHostResolve(ctx context.Context, hostname string) error {
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", hostname)
	slog.InfoContext(ctx, "checkSSHResolve", "cmd", strings.Join(cmd.Args, " "))
	out, err := cmd.CombinedOutput()
	slog.InfoContext(ctx, "checkSSHResolve", "hostname", hostname, "out", string(out), "error", err)
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

// CheckForIncludeWithFS verifies that the user's ~/.ssh/ssh_config has the necessary "Include" statement
// for sand's ssh_config file.
func CheckForIncludeWithFS(ctx context.Context, fs FileSystem) (func() error, error) {
	sandSSHPathInclude := "Include " + filepath.Join(os.Getenv("HOME"), ".config", "sand", "ssh_config")
	defaultSSHPath := filepath.Join(os.Getenv("HOME"), ".ssh", "config")

	slog.InfoContext(ctx, "CheckForIncludeWithFS", "sandSSHPathInclude", sandSSHPathInclude, "defaultSSHPath", defaultSSHPath)

	// Read the existing SSH config file
	existingContent, err := fs.ReadFile(defaultSSHPath)
	if err != nil {
		// If the file doesn't exist, create a new one with just the include line
		if os.IsNotExist(err) {
			return nil, fs.SafeWriteFile(defaultSSHPath, []byte(sandSSHPathInclude+"\n"), 0o644)
		}
		return nil, fmt.Errorf("⚠️  SSH connections are disabled. cannot open SSH config file: %s: %w", defaultSSHPath, err)
	}

	// Parse the config file
	cfg, err := ssh_config.Decode(bytes.NewReader(existingContent))
	if err != nil {
		return nil, fmt.Errorf("couldn't decode ssh_config: %w", err)
	}

	var sandInludePos *ssh_config.Position
	var firstNonIncludePos *ssh_config.Position
	for _, host := range cfg.Hosts {
		for _, node := range host.Nodes {
			inc, ok := node.(*ssh_config.Include)
			if ok {
				if strings.TrimSpace(inc.String()) == sandSSHPathInclude {
					pos := inc.Pos()
					sandInludePos = &pos
				}
			} else if firstNonIncludePos == nil && !strings.HasPrefix(strings.TrimSpace(node.String()), "#") {
				pos := node.Pos()
				firstNonIncludePos = &pos
			}
		}
	}

	slog.InfoContext(ctx, "CheckForIncludeWithFS", "sandInludePos", sandInludePos)

	if sandInludePos == nil {
		return func() error {
			// Include line not found, add it to the top of the file
			return modifySSHConfig(cfg, sandSSHPathInclude, fs, defaultSSHPath)
		}, nil
	}

	if firstNonIncludePos != nil && firstNonIncludePos.Line < sandInludePos.Line {
		fmt.Printf("⚠️  SSH confg warning: the location of the Include statement for sand's ssh config on line %d of %s may prevent ssh from working with sand containers. try moving it to the top of the file (before any 'Host' lines) if ssh isn't working for you.\n", sandInludePos.Line, defaultSSHPath)
	}
	return nil, nil
}

func writeSandSSHConfig(fs FileSystem) error {
	identityPath := filepath.Join(os.Getenv("HOME"), ".config", "sand", "user_key")
	sandSSHConfigPath := filepath.Join(os.Getenv("HOME"), ".config", "sand", "ssh_config")
	knownHostsPath := filepath.Join(os.Getenv("HOME"), ".config", "sand", "known_hosts")

	hostPattern, err := ssh_config.NewPattern("*.test")
	if err != nil {
		return err
	}
	cfg := &ssh_config.Config{
		Hosts: []*ssh_config.Host{
			{
				Patterns: []*ssh_config.Pattern{
					hostPattern,
				},
				Nodes: []ssh_config.Node{
					&ssh_config.KV{
						Key:   "IdentityFile",
						Value: identityPath,
					},

					&ssh_config.KV{
						Key:   "UserKnownHostsFile",
						Value: knownHostsPath,
					},
					&ssh_config.KV{
						Key:   "CanonicalizeHostname",
						Value: "yes",
					},
					&ssh_config.KV{
						Key:   "CanonicalDomains",
						Value: "test",
					},
				},
			},
		},
	}

	cfgBytes, err := cfg.MarshalText()
	if err != nil {
		return fmt.Errorf("couldn't marshal ssh_config: %w", err)
	}
	if err := fs.SafeWriteFile(sandSSHConfigPath, cfgBytes, 0o644); err != nil {
		return fmt.Errorf("couldn't safely write ssh_config: %w", err)
	}
	return nil
}

func modifySSHConfig(cfg *ssh_config.Config, sandSSHPathInclude string, fs FileSystem, defaultSSHPath string) error {
	cfgBytes, err := cfg.MarshalText()
	if err != nil {
		return fmt.Errorf("couldn't marshal ssh_config: %w", err)
	}

	// Add the include line to the beginning
	cfgBytes = append([]byte(sandSSHPathInclude+"\n"), cfgBytes...)

	// Safely write the updated config back to the file
	if err := fs.SafeWriteFile(defaultSSHPath, cfgBytes, 0o644); err != nil {
		return fmt.Errorf("couldn't safely write ssh_config: %w", err)
	}
	return nil
}

// encodePrivateKeyToPEM encodes an Ed25519 private key for storage
func encodePrivateKeyToPEM(privateKey ed25519.PrivateKey) []byte {
	// No need to create a signer first, we can directly marshal the key

	// Format and encode as a binary private key format
	pkBytes, err := ssh.MarshalPrivateKey(privateKey, "sand key")
	if err != nil {
		panic(fmt.Sprintf("failed to marshal private key: %v", err))
	}

	// Return PEM encoded bytes
	return pem.EncodeToMemory(pkBytes)
}

// FileSystem represents a filesystem interface for testability
type FileSystem interface {
	Stat(name string) (fs.FileInfo, error)
	Mkdir(name string, perm fs.FileMode) error
	MkdirAll(name string, perm fs.FileMode) error
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error)
	TempFile(dir, pattern string) (*os.File, error)
	Rename(oldpath, newpath string) error
	SafeWriteFile(name string, data []byte, perm fs.FileMode) error
}

func (fs *RealFileSystem) MkdirAll(name string, perm fs.FileMode) error {
	return os.MkdirAll(name, perm)
}

// RealFileSystem is the default implementation of FileSystem that uses the OS
type RealFileSystem struct{}

func (fs *RealFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(name)
}

func (fs *RealFileSystem) Mkdir(name string, perm fs.FileMode) error {
	return os.Mkdir(name, perm)
}

func (fs *RealFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (fs *RealFileSystem) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (fs *RealFileSystem) OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

func (fs *RealFileSystem) TempFile(dir, pattern string) (*os.File, error) {
	return os.CreateTemp(dir, pattern)
}

func (fs *RealFileSystem) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// SafeWriteFile writes data to a temporary file, syncs to disk, creates a backup of the existing file if it exists,
// and then renames the temporary file to the target file name.
func (fs *RealFileSystem) SafeWriteFile(name string, data []byte, perm fs.FileMode) error {
	// Get the directory from the target filename
	dir := filepath.Dir(name)

	// Create a temporary file in the same directory
	tmpFile, err := fs.TempFile(dir, filepath.Base(name)+".*.tmp")
	if err != nil {
		return fmt.Errorf("couldn't create temporary file: %w", err)
	}
	tmpFilename := tmpFile.Name()
	defer os.Remove(tmpFilename) // Clean up if we fail

	// Write data to the temporary file
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("couldn't write to temporary file: %w", err)
	}

	// Sync to disk to ensure data is written
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("couldn't sync temporary file: %w", err)
	}

	// Close the temporary file
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("couldn't close temporary file: %w", err)
	}

	// If the original file exists, create a backup
	if _, err := fs.Stat(name); err == nil {
		backupName := name + ".bak"
		// Remove any existing backup
		_ = os.Remove(backupName) // Ignore errors if the backup doesn't exist

		// Create the backup
		if err := fs.Rename(name, backupName); err != nil {
			return fmt.Errorf("couldn't create backup file: %w", err)
		}
	}

	// Rename the temporary file to the target file
	if err := fs.Rename(tmpFilename, name); err != nil {
		return fmt.Errorf("couldn't rename temporary file to target: %w", err)
	}

	// Set permissions on the new file
	if err := os.Chmod(name, perm); err != nil {
		return fmt.Errorf("couldn't set permissions on file: %w", err)
	}

	return nil
}

// CheckSSHReachability checks if the user's SSH config includes the Sand SSH config file and that
// ssh can resolve the container's hostname.
func CheckSSHReachability(ctx context.Context, cntrName string) (func() error, error) {
	if err := checkSSHHostResolve(ctx, cntrName); err != nil {
		slog.InfoContext(ctx, "CheckForIncludeWithFS")
		return CheckForIncludeWithFS(ctx, &RealFileSystem{})
	}
	return nil, nil
}

// KeyGenerator represents an interface for generating SSH keys for testability
type KeyGenerator interface {
	GenerateKeyPair() (ed25519.PrivateKey, ed25519.PublicKey, error)
	ConvertToSSHPublicKey(publicKey ed25519.PublicKey) (ssh.PublicKey, error)
}

// RealKeyGenerator is the default implementation of KeyGenerator
type RealKeyGenerator struct{}

func (kg *RealKeyGenerator) GenerateKeyPair() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	return privateKey, publicKey, err
}

func (kg *RealKeyGenerator) ConvertToSSHPublicKey(publicKey ed25519.PublicKey) (ssh.PublicKey, error) {
	return ssh.NewPublicKey(publicKey)
}
