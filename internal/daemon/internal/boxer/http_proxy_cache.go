package boxer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/banksean/sand/internal/hostops"
	"github.com/banksean/sand/internal/runtimedeps"
	"github.com/banksean/sand/internal/sandtypes"
)

const (
	HTTPProxyCacheContainerName = "sand-http-cache"
	HTTPProxyCacheImage         = "docker.io/ubuntu/squid:6.6-24.04_beta"
	HTTPProxyCachePort          = 3128
	httpProxyCacheVersion       = "2"
	httpProxyCacheDirName       = "http-proxy"
	httpProxySquidDirName       = "squid"
	httpProxyCacheServiceLabel  = "sand.service"
	httpProxyCacheVersionLabel  = "sand.service.version"
	httpProxyCacheServiceValue  = "http-proxy"
)

const httpProxySquidConfig = `http_port 3128 ssl-bump generate-host-certificates=on dynamic_cert_mem_cache_size=4MB cert=/etc/squid/certs/squid.pem

sslcrtd_program /usr/lib/squid/security_file_certgen -s /var/lib/squid/ssl_db -M 4MB
sslcrtd_children 5

acl step1 at_step SslBump1
ssl_bump peek step1
ssl_bump bump all

cache_dir ufs /var/spool/squid 10000 16 256
maximum_object_size 1024 MB
cache_mem 256 MB

http_access allow all
`

const httpProxyCacheEntrypointScript = `set -eu
mkdir -p /var/lib/squid /etc/squid/certs
if ! command -v /usr/lib/squid/security_file_certgen >/dev/null 2>&1; then
	if command -v apt-get >/dev/null 2>&1; then
		apt-get update
		tmp="$(mktemp -d)"
		cd "$tmp"
		apt-get download squid-common squid-openssl
		mkdir root
		dpkg-deb -x squid-common_*.deb root
		dpkg-deb -x squid-openssl_*.deb root
		cp -a root/usr/sbin/. /usr/sbin/
		cp -a root/usr/lib/squid/. /usr/lib/squid/
		cp -a root/usr/share/squid/. /usr/share/squid/
		cd /
		rm -rf "$tmp"
		rm -rf /var/lib/apt/lists/*
	fi
fi
helper=""
for candidate in /usr/lib/squid/security_file_certgen /usr/libexec/squid/security_file_certgen /usr/local/squid/libexec/security_file_certgen /usr/local/squid/libexec/ssl_crtd; do
	if [ -x "$candidate" ]; then
		helper="$candidate"
		break
	fi
done
if [ -z "$helper" ]; then
	echo "sand-http-cache: missing Squid SSL certificate generator helper; install squid-openssl or provide security_file_certgen" >&2
	exit 1
fi
if [ "$helper" != /usr/lib/squid/security_file_certgen ]; then
	mkdir -p /usr/lib/squid
	ln -sf "$helper" /usr/lib/squid/security_file_certgen
fi
if [ ! -d /var/lib/squid/ssl_db ]; then
	/usr/lib/squid/security_file_certgen -c -s /var/lib/squid/ssl_db -M 4MB
fi
chown -R proxy:proxy /var/lib/squid /var/spool/squid 2>/dev/null || true
exec /usr/sbin/squid -f /etc/squid/sand-squid.conf -NYC
`

type HTTPProxyCacheStatus struct {
	Name     string
	Image    string
	State    string
	URL      string
	CacheDir string
	Running  bool
}

type HTTPProxyCacheService struct {
	boxer          *Boxer
	mu             sync.Mutex
	readinessCheck func(context.Context) error
}

func (sb *Boxer) HTTPProxyCacheService() *HTTPProxyCacheService {
	if sb.httpProxyService == nil {
		sb.httpProxyService = &HTTPProxyCacheService{
			boxer:          sb,
			readinessCheck: defaultHTTPProxyReadinessCheck,
		}
	}
	return sb.httpProxyService
}

func (s *HTTPProxyCacheService) Ensure(ctx context.Context, localDomain string, progress io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if progress == nil {
		progress = io.Discard
	}
	fmt.Fprintf(progress, "[sand] ensuring HTTP proxy cache service\n")
	if err := s.boxer.EnsureImage(ctx, HTTPProxyCacheImage, progress); err != nil {
		return fmt.Errorf("ensure HTTP proxy cache image: %w", err)
	}
	if err := s.ensureSquidFiles(); err != nil {
		return err
	}

	ctr, err := s.inspect(ctx)
	if err != nil {
		return err
	}
	if ctr == nil {
		fmt.Fprintf(progress, "[sand] creating HTTP proxy cache container %s\n", HTTPProxyCacheContainerName)
		if err := s.create(ctx, localDomain); err != nil {
			return err
		}
		ctr, err = s.inspect(ctx)
		if err != nil {
			return err
		}
	}
	if err := validateHTTPProxyCacheContainer(ctr); err != nil {
		return err
	}

	if ctr.Status.State != "running" {
		fmt.Fprintf(progress, "[sand] starting HTTP proxy cache container %s\n", HTTPProxyCacheContainerName)
		if _, err := s.boxer.ContainerService.Start(ctx, nil, HTTPProxyCacheContainerName); err != nil {
			return fmt.Errorf("start HTTP proxy cache container: %w", err)
		}
	}

	fmt.Fprintf(progress, "[sand] waiting for HTTP proxy cache readiness\n")
	if err := s.waitReady(ctx); err != nil {
		return err
	}
	return nil
}

func (s *HTTPProxyCacheService) Start(ctx context.Context, localDomain string, progress io.Writer) error {
	return s.Ensure(ctx, localDomain, progress)
}

func (s *HTTPProxyCacheService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctr, err := s.inspect(ctx)
	if err != nil {
		return err
	}
	if ctr == nil || ctr.Status.State != "running" {
		return nil
	}
	if err := validateHTTPProxyCacheContainer(ctr); err != nil {
		return err
	}
	_, err = s.boxer.ContainerService.Stop(ctx, &hostops.StopContainer{Time: 5}, HTTPProxyCacheContainerName)
	if err != nil {
		return fmt.Errorf("stop HTTP proxy cache container: %w", err)
	}
	return nil
}

func (s *HTTPProxyCacheService) Restart(ctx context.Context, localDomain string, progress io.Writer) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Ensure(ctx, localDomain, progress)
}

func (s *HTTPProxyCacheService) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctr, err := s.inspect(ctx)
	if err != nil {
		return err
	}
	if ctr != nil {
		if err := validateHTTPProxyCacheContainer(ctr); err != nil {
			return err
		}
		if _, err := s.boxer.ContainerService.Delete(ctx, &hostops.DeleteContainer{Force: true}, HTTPProxyCacheContainerName); err != nil {
			return fmt.Errorf("delete HTTP proxy cache container: %w", err)
		}
	}
	if err := s.boxer.FileOps.RemoveAll(s.cacheDir()); err != nil {
		return fmt.Errorf("clear HTTP proxy cache data: %w", err)
	}
	return nil
}

func (s *HTTPProxyCacheService) Status(ctx context.Context, localDomain string) (HTTPProxyCacheStatus, error) {
	ctr, err := s.inspect(ctx)
	if err != nil {
		return HTTPProxyCacheStatus{}, err
	}
	status := HTTPProxyCacheStatus{
		Name:     HTTPProxyCacheContainerName,
		Image:    HTTPProxyCacheImage,
		URL:      httpProxyCacheURL(localDomain),
		CacheDir: s.cacheDir(),
	}
	if ctr == nil {
		status.State = "missing"
		return status, nil
	}
	if err := validateHTTPProxyCacheContainer(ctr); err != nil {
		return status, err
	}
	status.State = ctr.Status.State
	status.Running = ctr.Status.State == "running"
	if ctr.Configuration.Image.Reference != "" {
		status.Image = ctr.Configuration.Image.Reference
	}
	return status, nil
}

func (s *HTTPProxyCacheService) create(ctx context.Context, localDomain string) error {
	if localDomain == "" {
		localDomain = runtimedeps.DefaultDNSDomain
	}
	cacheDir := s.cacheDir()
	if err := s.boxer.FileOps.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create HTTP proxy cache dir: %w", err)
	}
	_, err := s.boxer.ContainerService.Create(ctx, &hostops.CreateContainer{
		ResourceOptions: hostops.ResourceOptions{
			CPUs:   1,
			Memory: "512M",
		},
		ManagementOptions: hostops.ManagementOptions{
			Name:      HTTPProxyCacheContainerName,
			DNSDomain: strings.Trim(localDomain, "."),
			Publish:   fmt.Sprintf("127.0.0.1:%d:%d/tcp", HTTPProxyCachePort, HTTPProxyCachePort),
			Label: map[string]string{
				httpProxyCacheServiceLabel: httpProxyCacheServiceValue,
				httpProxyCacheVersionLabel: httpProxyCacheVersion,
			},
			Volume: []string{
				sandtypes.MountSpec{
					Source: cacheDir,
					Target: "/var/spool/squid",
				}.String(),
				httpProxySquidConfigPath(s.boxer.appRoot) + ":/etc/squid/sand-squid.conf:ro",
				httpProxySquidPEMPath(s.boxer.appRoot) + ":/etc/squid/certs/squid.pem:ro",
			},
			Entrypoint: "/bin/sh",
		},
	}, HTTPProxyCacheImage, []string{"-c", httpProxyCacheEntrypointScript})
	if err != nil {
		return fmt.Errorf("create HTTP proxy cache container: %w", err)
	}
	return nil
}

func (s *HTTPProxyCacheService) inspect(ctx context.Context) (*sandtypes.Container, error) {
	containers, err := s.boxer.ContainerService.Inspect(ctx, HTTPProxyCacheContainerName)
	if err != nil {
		if isMissingContainerError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect HTTP proxy cache container: %w", err)
	}
	if len(containers) == 0 {
		return nil, nil
	}
	return &containers[0], nil
}

func (s *HTTPProxyCacheService) cacheDir() string {
	return filepath.Join(s.boxer.appRoot, "caches", httpProxyCacheDirName)
}

func (s *HTTPProxyCacheService) ensureSquidFiles() error {
	squidDir := filepath.Join(s.boxer.appRoot, httpProxySquidDirName)
	if err := s.boxer.FileOps.MkdirAll(squidDir, 0o750); err != nil {
		return fmt.Errorf("create HTTP proxy squid dir: %w", err)
	}
	if err := s.ensureSquidCA(); err != nil {
		return err
	}
	if err := s.boxer.FileOps.WriteFile(httpProxySquidConfigPath(s.boxer.appRoot), []byte(httpProxySquidConfig), 0o644); err != nil {
		return fmt.Errorf("write HTTP proxy squid config: %w", err)
	}
	return nil
}

func (s *HTTPProxyCacheService) ensureSquidCA() error {
	keyPath := httpProxySquidKeyPath(s.boxer.appRoot)
	crtPath := httpProxyCACertPath(s.boxer.appRoot)
	pemPath := httpProxySquidPEMPath(s.boxer.appRoot)

	keyPEM, certPEM, ok := readValidSquidCA(keyPath, crtPath)
	if !ok {
		var err error
		keyPEM, certPEM, err = generateSquidCA()
		if err != nil {
			return err
		}
		if err := s.boxer.FileOps.WriteFile(keyPath, keyPEM, 0o600); err != nil {
			return fmt.Errorf("write HTTP proxy CA key: %w", err)
		}
		if err := s.boxer.FileOps.WriteFile(crtPath, certPEM, 0o644); err != nil {
			return fmt.Errorf("write HTTP proxy CA certificate: %w", err)
		}
	}
	combined := append(append([]byte{}, keyPEM...), certPEM...)
	if err := s.boxer.FileOps.WriteFile(pemPath, combined, 0o600); err != nil {
		return fmt.Errorf("write HTTP proxy squid PEM: %w", err)
	}
	return nil
}

func readValidSquidCA(keyPath, certPath string) ([]byte, []byte, bool) {
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, false
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, false
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		return nil, nil, false
	}
	if _, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err != nil {
		return nil, nil, false
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, nil, false
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, false
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) || !cert.IsCA {
		return nil, nil, false
	}
	return keyPEM, certPEM, true
}

func generateSquidCA() ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate HTTP proxy CA key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("generate HTTP proxy CA serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "Docker Caching Proxy CA",
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("generate HTTP proxy CA certificate: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return keyPEM, certPEM, nil
}

func httpProxySquidConfigPath(appRoot string) string {
	return filepath.Join(appRoot, httpProxySquidDirName, "squid.conf")
}

func httpProxySquidKeyPath(appRoot string) string {
	return filepath.Join(appRoot, httpProxySquidDirName, "squid.key")
}

func httpProxyCACertPath(appRoot string) string {
	return filepath.Join(appRoot, httpProxySquidDirName, "squid.crt")
}

func httpProxySquidPEMPath(appRoot string) string {
	return filepath.Join(appRoot, httpProxySquidDirName, "squid.pem")
}

func (s *HTTPProxyCacheService) waitReady(ctx context.Context) error {
	deadline, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		if err := s.readinessCheck(deadline); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("HTTP proxy cache did not become ready: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func defaultHTTPProxyReadinessCheck(ctx context.Context) error {
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", HTTPProxyCachePort))
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("proxy readiness returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func validateHTTPProxyCacheContainer(ctr *sandtypes.Container) error {
	if ctr == nil {
		return nil
	}
	service, ok := stringLabel(ctr.Configuration.Labels, httpProxyCacheServiceLabel)
	if !ok {
		if isExpectedHTTPProxyCacheImage(ctr.Configuration.Image.Reference) {
			return nil
		}
		return fmt.Errorf("container %q already exists but is not managed by sand", HTTPProxyCacheContainerName)
	}
	if service != httpProxyCacheServiceValue {
		return fmt.Errorf("container %q has unexpected %s label %q", HTTPProxyCacheContainerName, httpProxyCacheServiceLabel, service)
	}
	version, ok := stringLabel(ctr.Configuration.Labels, httpProxyCacheVersionLabel)
	if !ok || version != httpProxyCacheVersion {
		return fmt.Errorf("container %q has unsupported service version; run `sand cache http-proxy clear` and start it again", HTTPProxyCacheContainerName)
	}
	if image := ctr.Configuration.Image.Reference; image != "" && !isExpectedHTTPProxyCacheImage(image) {
		return fmt.Errorf("container %q uses image %q, want %q; run `sand cache http-proxy clear` and start it again", HTTPProxyCacheContainerName, image, HTTPProxyCacheImage)
	}
	return nil
}

func isExpectedHTTPProxyCacheImage(image string) bool {
	return image == HTTPProxyCacheImage || image == "docker.io/"+HTTPProxyCacheImage
}

func stringLabel(labels map[string]any, key string) (string, bool) {
	if labels == nil {
		return "", false
	}
	value, ok := labels[key]
	if !ok {
		return "", false
	}
	switch v := value.(type) {
	case string:
		return v, true
	case fmt.Stringer:
		return v.String(), true
	default:
		return fmt.Sprint(v), true
	}
}

func isMissingContainerError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such container") ||
		strings.Contains(msg, "does not exist")
}
