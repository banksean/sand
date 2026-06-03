package hostport

import (
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// startEcho starts a localhost TCP server on a free port that echoes everything
// it reads back to the client. Returns the chosen port and a cleanup func.
func startEcho(t *testing.T) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	done := make(chan struct{})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	return port, func() {
		close(done)
		_ = ln.Close()
	}
}

func TestForwarderProxiesTraffic(t *testing.T) {
	port, stop := startEcho(t)
	defer stop()

	f := &Forwarder{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: "127.0.0.1:" + strconv.Itoa(port),
	}
	// Start with a real bound address.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test: %v", err)
	}
	listenAddr := ln.Addr().String()
	_ = ln.Close()
	f.ListenAddr = listenAddr
	if err := f.Start(); err != nil {
		t.Fatalf("start forwarder: %v", err)
	}
	defer f.Close()

	c, err := net.DialTimeout("tcp", listenAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer c.Close()

	msg := "hello sandbox\n"
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if got := string(buf); got != msg {
		t.Fatalf("echo mismatch: got %q want %q", got, msg)
	}
}

func TestManagerLifecycle(t *testing.T) {
	port, stop := startEcho(t)
	defer stop()

	// Echo binds to 127.0.0.1; have the manager listen on a different loopback
	// alias (127.0.0.2) so it doesn't collide on the same port.
	listenIP := "127.0.0.2"
	if probe, err := net.Listen("tcp", listenIP+":0"); err != nil {
		t.Skipf("%s not available as loopback alias: %v", listenIP, err)
	} else {
		_ = probe.Close()
	}

	m := NewManager()
	if err := m.startForSandbox("sb1", listenIP, "127.0.0.1", []int{port}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Look up the listener we created.
	m.mu.Lock()
	fwds := m.fwd["sb1"]
	m.mu.Unlock()
	if len(fwds) != 1 {
		t.Fatalf("want 1 forwarder, got %d", len(fwds))
	}
	addr := fwds[0].listener.Addr().String()

	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial via manager: %v", err)
	}
	c.Close()

	m.StopForSandbox("sb1")
	m.mu.Lock()
	_, exists := m.fwd["sb1"]
	m.mu.Unlock()
	if exists {
		t.Fatalf("sb1 should be gone from manager")
	}

	// Dialing should now fail.
	if _, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		t.Fatalf("expected dial to fail after stop")
	}
}

func TestManagerRejectsBadIP(t *testing.T) {
	m := NewManager()
	err := m.StartForSandbox("sb", "203.0.113.255", []int{1}) // unbindable
	if err == nil {
		t.Fatalf("expected error binding to unreachable IP")
	}
	if !strings.Contains(err.Error(), "start forwarder") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForwarderRewritesHTTPHost(t *testing.T) {
	// Upstream HTTP server that echoes back the Host header it saw.
	up, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}
	defer up.Close()
	go func() {
		for {
			c, err := up.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				n, _ := c.Read(buf)
				got := string(buf[:n])
				host := ""
				for _, line := range strings.Split(got, "\r\n") {
					if strings.HasPrefix(strings.ToLower(line), "host:") {
						host = strings.TrimSpace(line[5:])
					}
				}
				body := "host=" + host
				resp := "HTTP/1.1 200 OK\r\nContent-Length: " + strconv.Itoa(len(body)) +
					"\r\nConnection: close\r\n\r\n" + body
				_, _ = c.Write([]byte(resp))
			}(c)
		}
	}()

	upAddr := up.Addr().String()
	f := &Forwarder{
		ListenAddr:      "127.0.0.1:0",
		TargetAddr:      upAddr,
		RewriteHTTPHost: upAddr,
	}
	// Bind a real port.
	probe, _ := net.Listen("tcp", "127.0.0.1:0")
	fwdAddr := probe.Addr().String()
	_ = probe.Close()
	f.ListenAddr = fwdAddr
	if err := f.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer f.Close()

	c, err := net.DialTimeout("tcp", fwdAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	// Send a request with Host: host.sand.
	req := "GET /mcp HTTP/1.1\r\nHost: host.sand:3845\r\nUser-Agent: t\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "host=" + upAddr
	if !strings.Contains(string(resp), want) {
		t.Fatalf("upstream saw wrong host. response:\n%s\nwant body containing %q", resp, want)
	}
}

func TestLooksLikeHTTPRequest(t *testing.T) {
	cases := map[string]bool{
		"GET / HTTP":  true,
		"POST /x":     true,
		"HELLO":       false,
		"\x00\x01\x02": false,
	}
	for in, want := range cases {
		if got := looksLikeHTTPRequest([]byte(in)); got != want {
			t.Errorf("looksLikeHTTPRequest(%q) = %v, want %v", in, got, want)
		}
	}
}
