// Package hostport provides a small TCP forwarder used to expose host-loopback
// services into sand sandboxes.
//
// Apple's container CLI puts each sandbox on a vmnet bridge with its own IP.
// Inside the sandbox, 127.0.0.1 is the sandbox itself; the Mac is the gateway
// IP on that bridge. Services bound to 127.0.0.1 on the Mac (e.g. Figma's MCP
// at 127.0.0.1:3845) are unreachable from the sandbox.
//
// A Forwarder listens on a bridge-facing host IP (the gateway IP) and forwards
// connections to a target on host loopback. Combined with an in-sandbox
// iptables DNAT rule that rewrites 127.0.0.1:<port> to <gateway>:<port>, this
// gives the agent the illusion of reaching the service on its own loopback.
package hostport

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// Forwarder is a single TCP listener that accepts connections on ListenAddr
// and proxies each one to TargetAddr.
//
// If RewriteHTTPHost is non-empty and the client speaks HTTP/1.x, the Host
// header on each request is rewritten to RewriteHTTPHost before being
// forwarded upstream. This lets a sandbox client say it's talking to
// host.sand:3845 while the upstream service (which only accepts
// 127.0.0.1:3845) sees the Host header it expects.
type Forwarder struct {
	ListenAddr      string
	TargetAddr      string
	RewriteHTTPHost string

	listener net.Listener
	wg       sync.WaitGroup
	closed   chan struct{}
	once     sync.Once
}

// Start binds the listener and begins accepting in a background goroutine.
// It returns an error if the bind fails.
func (f *Forwarder) Start() error {
	ln, err := net.Listen("tcp", f.ListenAddr)
	if err != nil {
		return fmt.Errorf("hostport: listen %s: %w", f.ListenAddr, err)
	}
	f.listener = ln
	f.closed = make(chan struct{})
	f.wg.Add(1)
	go f.acceptLoop()
	return nil
}

func (f *Forwarder) acceptLoop() {
	defer f.wg.Done()
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			select {
			case <-f.closed:
				return
			default:
			}
			slog.Warn("hostport: accept error", "listen", f.ListenAddr, "error", err)
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			f.handle(conn)
		}()
	}
}

func (f *Forwarder) handle(client net.Conn) {
	defer client.Close()
	upstream, err := net.Dial("tcp", f.TargetAddr)
	if err != nil {
		slog.Warn("hostport: dial upstream failed", "target", f.TargetAddr, "error", err)
		return
	}
	defer upstream.Close()

	// Peek the first few bytes from the client. If they look like an HTTP
	// request line and Host rewriting is enabled, run an HTTP-aware loop.
	// Otherwise fall through to a plain bidirectional pipe.
	br := bufio.NewReader(client)
	httpMode := false
	if f.RewriteHTTPHost != "" {
		peek, _ := br.Peek(8)
		httpMode = looksLikeHTTPRequest(peek)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if httpMode {
			if err := proxyHTTPRequests(br, upstream, f.RewriteHTTPHost); err != nil && err != io.EOF {
				slog.Debug("hostport: http proxy ended", "target", f.TargetAddr, "error", err)
			}
		} else {
			_, _ = io.Copy(upstream, br)
		}
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, upstream)
		if tc, ok := client.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	wg.Wait()
}

var httpMethods = [][]byte{
	[]byte("GET "), []byte("HEAD "), []byte("POST "), []byte("PUT "),
	[]byte("DELETE "), []byte("OPTIONS "), []byte("PATCH "), []byte("CONNECT "),
	[]byte("TRACE "),
}

func looksLikeHTTPRequest(b []byte) bool {
	for _, m := range httpMethods {
		if bytes.HasPrefix(b, m) {
			return true
		}
	}
	return false
}

// proxyHTTPRequests reads HTTP/1.x requests from br, rewrites the Host header
// to host, and writes them to w. It runs until the connection closes or an
// unrecoverable error occurs. WebSocket and other Upgrade requests are
// forwarded with Host rewritten and the remaining stream is copied raw.
func proxyHTTPRequests(br *bufio.Reader, w io.Writer, host string) error {
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return err
		}
		req.Host = host
		// http.Request.Write uses req.Host for the Host header and
		// req.URL.RequestURI() for the request line. RequestURI is
		// preserved unchanged.
		if err := req.Write(w); err != nil {
			return err
		}
		if isUpgrade(req) {
			// Drain remaining client bytes raw — the protocol has switched.
			_, err := io.Copy(w, br)
			return err
		}
	}
}

func isUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, v := range r.Header.Values("Connection") {
		if containsToken(v, "upgrade") {
			return true
		}
	}
	return false
}

func containsToken(headerValue, token string) bool {
	for _, part := range bytes.Split([]byte(headerValue), []byte{','}) {
		if string(bytes.ToLower(bytes.TrimSpace(part))) == token {
			return true
		}
	}
	return false
}

// Close stops accepting new connections and waits for in-flight connections
// to finish. Safe to call multiple times.
func (f *Forwarder) Close() error {
	var closeErr error
	f.once.Do(func() {
		if f.closed != nil {
			close(f.closed)
		}
		if f.listener != nil {
			closeErr = f.listener.Close()
		}
		f.wg.Wait()
	})
	return closeErr
}

// Manager tracks active Forwarders by sandbox ID so they can be torn down
// when a sandbox stops or is removed.
type Manager struct {
	mu  sync.Mutex
	fwd map[string][]*Forwarder
}

func NewManager() *Manager {
	return &Manager{fwd: map[string][]*Forwarder{}}
}

// StartForSandbox starts one forwarder per port. ListenIP is the bridge-facing
// host IP (gateway IP of the sandbox's network); ports are the host-loopback
// ports to expose. Already-running forwarders for sandboxID are stopped first.
//
// Each forwarder rewrites the HTTP Host header on incoming requests to
// 127.0.0.1:<port>, so clients pointed at host.sand:<port> (or another
// gateway-resolved name) reach upstreams that expect a loopback Host header.
func (m *Manager) StartForSandbox(sandboxID, listenIP string, ports []int) error {
	return m.startForSandbox(sandboxID, listenIP, "127.0.0.1", ports)
}

func (m *Manager) startForSandbox(sandboxID, listenIP, targetIP string, ports []int) error {
	m.StopForSandbox(sandboxID)
	if listenIP == "" || len(ports) == 0 {
		return nil
	}
	var started []*Forwarder
	for _, p := range ports {
		targetAddr := net.JoinHostPort(targetIP, strconv.Itoa(p))
		f := &Forwarder{
			ListenAddr:      net.JoinHostPort(listenIP, strconv.Itoa(p)),
			TargetAddr:      targetAddr,
			RewriteHTTPHost: targetAddr,
		}
		if err := f.Start(); err != nil {
			for _, x := range started {
				_ = x.Close()
			}
			return fmt.Errorf("hostport: start forwarder for port %d: %w", p, err)
		}
		slog.Info("hostport: forwarder started", "sandbox", sandboxID, "listen", f.ListenAddr, "target", f.TargetAddr)
		started = append(started, f)
	}
	m.mu.Lock()
	m.fwd[sandboxID] = started
	m.mu.Unlock()
	return nil
}

// StopForSandbox closes any forwarders for sandboxID. Safe if none exist.
func (m *Manager) StopForSandbox(sandboxID string) {
	m.mu.Lock()
	fwds := m.fwd[sandboxID]
	delete(m.fwd, sandboxID)
	m.mu.Unlock()
	for _, f := range fwds {
		if err := f.Close(); err != nil {
			slog.Warn("hostport: close forwarder", "sandbox", sandboxID, "listen", f.ListenAddr, "error", err)
		}
	}
}

// StopAll closes every forwarder the manager knows about. Used at daemon
// shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.fwd))
	for id := range m.fwd {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.StopForSandbox(id)
	}
}
