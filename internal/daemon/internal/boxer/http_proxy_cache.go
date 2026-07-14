package boxer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	HTTPProxyCacheImage         = "ubuntu/squid:6.6-24.04_beta"
	HTTPProxyCachePort          = 3128
	httpProxyCacheVersion       = "1"
	httpProxyCacheDirName       = "http-proxy"
	httpProxyCacheServiceLabel  = "sand.service"
	httpProxyCacheVersionLabel  = "sand.service.version"
	httpProxyCacheServiceValue  = "http-proxy"
)

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
			Volume: []string{sandtypes.MountSpec{
				Source: cacheDir,
				Target: "/var/spool/squid",
			}.String()},
		},
	}, HTTPProxyCacheImage, nil)
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

func (s *HTTPProxyCacheService) waitReady(ctx context.Context) error {
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
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
		return fmt.Errorf("container %q already exists but is not managed by sand", HTTPProxyCacheContainerName)
	}
	if service != httpProxyCacheServiceValue {
		return fmt.Errorf("container %q has unexpected %s label %q", HTTPProxyCacheContainerName, httpProxyCacheServiceLabel, service)
	}
	version, ok := stringLabel(ctr.Configuration.Labels, httpProxyCacheVersionLabel)
	if !ok || version != httpProxyCacheVersion {
		return fmt.Errorf("container %q has unsupported service version; run `sand cache http-proxy clear` and start it again", HTTPProxyCacheContainerName)
	}
	if image := ctr.Configuration.Image.Reference; image != "" && image != HTTPProxyCacheImage {
		return fmt.Errorf("container %q uses image %q, want %q; run `sand cache http-proxy clear` and start it again", HTTPProxyCacheContainerName, image, HTTPProxyCacheImage)
	}
	return nil
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
