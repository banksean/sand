//go:build linux

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpf EgressFilter ../../egress_filter.c -- -D__TARGET_ARCH_arm64

package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/miekg/dns"
)

const bpfTarget = "/sys/fs/bpf"

func retry(attempts int, sleep time.Duration, f func() error) (err error) {
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(sleep)
			sleep *= 2 // Exponential backoff
		}
		err = f()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("after %d attempts, last error: %s", attempts, err)
}

func allowIP(m *ebpf.Map, addr string) {
	ip := net.ParseIP(addr).To4()
	if ip == nil {
		return
	}
	key := binary.LittleEndian.Uint32(ip)
	val := uint8(1)
	if err := m.Put(key, val); err != nil {
		log("map put %s: %v", addr, err)
	}
}

func isAllowedDomain(allowed []string, name string) bool {
	for _, d := range allowed {
		if dns.Fqdn(d) == name || dns.IsSubDomain(d, name) {
			return true
		}
	}
	return false
}

func loadDomains(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		log("open domains error: %v", err)
		return nil
	}
	defer f.Close()
	var domains []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if d := sc.Text(); d != "" && d[0] != '#' {
			domains = append(domains, d)
		}
	}
	return domains
}

func preResolveDomains(domains []string, upstream string, m *ebpf.Map) {
	c := new(dns.Client)
	for _, domain := range domains {
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(domain), dns.TypeA)
		resp, _, err := c.Exchange(msg, upstream)
		if err != nil {
			log("warn: resolve %s: %v", domain, err)
			continue
		}
		for _, ans := range resp.Answer {
			if a, ok := ans.(*dns.A); ok {
				allowIP(m, a.A.String())
				log("pre-resolved %s -> %s", domain, a.A)
			}
		}
	}
}

func waitForLoopback() error {
	return retry(100, time.Millisecond, func() error {
		iface, err := net.InterfaceByName("lo")
		if err == nil {
			addrs, err := iface.Addrs()

			if err == nil && len(addrs) > 0 { // try actually binding to confirm it's up
				l, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err == nil {
					l.Close()
					return nil
				}
				return err
			}
		}
		return err
	})
}

func waitForNetwork(upstream string) error {
	return retry(100, time.Millisecond, func() error {
		c := new(dns.Client)
		msg := new(dns.Msg).SetQuestion(".", dns.TypeNS)
		_, _, err := c.Exchange(msg, upstream)
		return err
	})
}

func mountBPFFS() error {
	// First, wait until bpfTarget is writeable. This is very  brittle.
	// We try 100 times max, waiting 10ms between each attempt,
	// to write bpfs. We do this because this process starts
	// before the actual vminitd starts, and vminit is what mounts
	// bpfs and makes it writeable.
	// TODO: look into just forking vminitd rather than trying to
	// do all this stuff in a sidecar.
	err := retry(100, time.Millisecond, func() error {
		return os.MkdirAll(bpfTarget, 0o755)
	})
	if err != nil {
		return err
	}

	return retry(100, time.Millisecond, func() error {
		if err := syscall.Mount("bpffs", bpfTarget, "bpf", 0, ""); err != nil {
			if err == syscall.EBUSY {
				// already mounted, that's fine
				return nil
			}
			return err
		}
		return nil
	})
}

// attachTCEgress attaches prog to the egress hook of ifname using TCX (kernel >= 6.6).
// Pins the bpf_link so it survives exec into vminitd.real.
func attachTCEgress(ifname string, prog *ebpf.Program) (func(), error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, fmt.Errorf("interface %q: %w", ifname, err)
	}

	l, err := link.AttachTCX(link.TCXOptions{
		Interface: iface.Index,
		Program:   prog,
		Attach:    ebpf.AttachTCXEgress,
	})
	if err != nil {
		return nil, fmt.Errorf("attach tcx: %w", err)
	}

	if err := l.Pin(bpfTarget + "/egress_link"); err != nil {
		l.Close()
		return nil, fmt.Errorf("pin tcx link: %w", err)
	}

	return func() { l.Close() }, nil
}

var (
	logWriterMu sync.Mutex
	logWriter   *os.File
)

func log(s string, args ...any) {
	if logWriter == nil {
		return
	}
	logWriterMu.Lock()
	defer logWriterMu.Unlock()
	logWriter.WriteString(fmt.Sprintf("<6>%s\n", fmt.Sprintf(s, args...)))
}

func makeHandler(domains []string, upstream string, m *ebpf.Map) func(dns.ResponseWriter, *dns.Msg) {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		// Filter out any domains that are not allowlisted, so the
		// upstream query doesn't even ask for them.
		var allowedQuestions []dns.Question
		for _, q := range r.Question {
			if isAllowedDomain(domains, q.Name) {
				allowedQuestions = append(allowedQuestions, q)
				log("dns.Client Question allowed domain: %s=%s", q.Name, q.String())
			} else {
				log("dns.Client Question blocked domain: %s=%s", q.Name, q.String())
			}
		}
		if len(allowedQuestions) == 0 && len(r.Question) > 0 {
			resp := new(dns.Msg).SetReply(r).SetRcode(r, dns.RcodeRefused)
			w.WriteMsg(resp)
			return
		}
		r.Question = allowedQuestions

		c := new(dns.Client)
		resp, _, err := c.Exchange(r, upstream)
		if err != nil {
			if logWriter != nil {
				log("dns.Client Exchange error: %v", err)
			}
			dns.HandleFailed(w, r)
			return
		}

		for _, ans := range resp.Answer {
			if a, ok := ans.(*dns.A); ok {
				allowIP(m, a.A.String())
			}
		}
		w.WriteMsg(resp)
	}
}

func setupBPF() (*ebpf.Map, func(), error) {
	if err := mountBPFFS(); err != nil {
		return nil, nil, fmt.Errorf("mountBPFFS: %w", err)
	}

	if _, err := os.ReadDir(bpfTarget); err != nil {
		return nil, nil, fmt.Errorf("readdir %s: %w", bpfTarget, err)
	}

	var objs EgressFilterObjects
	if err := LoadEgressFilterObjects(&objs, &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogLevel: ebpf.LogLevelInstruction,
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("load bpf objs: %w", err)
	}

	if err := objs.AllowedIps.Pin(bpfTarget + "/allowed_ips"); err != nil {
		objs.Close()
		return nil, nil, fmt.Errorf("pin map: %w", err)
	}

	cleanup, err := attachTCEgress("eth0", objs.EgressFilter)
	if err != nil {
		objs.Close()
		return nil, nil, fmt.Errorf("attaching egress to eth0: %w", err)
	}
	objs.EgressFilter.Close()

	return objs.AllowedIps, cleanup, nil
}

// defaultGateway returns the default gateway IP by reading /proc/net/route.
func defaultGateway() (string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // skip header line
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		if fields[1] != "00000000" { // destination 0.0.0.0 = default route
			continue
		}
		gw, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			continue
		}
		ip := make(net.IP, 4)
		binary.LittleEndian.PutUint32(ip, uint32(gw))
		return ip.String(), nil
	}
	return "", fmt.Errorf("no default gateway found in /proc/net/route")
}

// fetchDomainsFromSandd contacts the sandd HTTP server on the host to retrieve
// the allowed-domains list for this sandbox. Sandd identifies the sandbox by
// the source IP of the request, so no name parameter is needed.
func fetchDomainsFromSandd(gateway, port string) ([]string, error) {
	log("fetchDomainsFromSandd: gateway=%s", gateway)
	url := fmt.Sprintf("http://%s:%s/sandbox-config", gateway, port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result struct {
		Domains []string `json:"domains"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Domains, nil
}

func main() {
	domainsFile := flag.String("domains-file", "/etc/sandbox/allowed-domains.txt", "")
	listen := flag.String("listen", "127.0.0.1:53", "")
	upstream := flag.String("upstream", "1.1.1.1:53", "")
	logTo := flag.String("log", "/dev/kmsg", "")
	sanddPort := flag.String("sandd-port", "4242", "port that sandd listens on (for fetching allowed-domains at startup)")
	flag.Parse()
	logWriter = os.Stderr
	// Log to kernel log during start-up.
	if *logTo != "" {
		w, err := os.OpenFile(*logTo, os.O_WRONLY, 0)
		if err == nil {
			logWriter = w
			defer logWriter.Close()
		}
	}

	log("sand-dns-proxy: === starting dns proxy ===")

	m, cleanup, err := setupBPF()
	if err != nil {
		log("setupBPF: %v", err)
		return
	}
	defer cleanup()

	if err := waitForLoopback(); err != nil {
		log("waitForLoopback failed: %v", err)
		return
	} else {
		log("loopback ready")
	}

	if err := waitForNetwork(*upstream); err != nil {
		log("waitForNetwork failed: %v", err)
		return
	} else {
		log("network ready, pre-resolving domains...")
	}

	// Try to fetch per-sandbox allowed-domains from sandd on the host.
	// The eBPF filter allows all RFC1918 traffic, so the host gateway is
	// always reachable without pre-allowlisting.
	var domains []string
	gw, err := defaultGateway()
	if err != nil {
		log("warn: defaultGateway: %v; falling back to domains file", err)
	} else {
		fetched, err := fetchDomainsFromSandd(gw, *sanddPort)
		if err != nil {
			log("warn: fetchDomainsFromSandd: %v; falling back to domains file", err)
		} else if len(fetched) > 0 {
			log("fetched %d allowed domains from sandd", len(fetched))
			domains = fetched
		}
	}
	if domains == nil {
		domains = loadDomains(*domainsFile)
	}
	preResolveDomains(domains, *upstream, m)

	// Intercept DNS and dynamically update map on each response
	dns.HandleFunc(".", makeHandler(domains, *upstream, m))

	server := &dns.Server{Addr: *listen, Net: "udp"}
	log("dnsproxy returned %v", server.ListenAndServe())
}
