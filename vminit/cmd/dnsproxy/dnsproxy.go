//go:build linux

package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/miekg/dns"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
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
		return os.MkdirAll(bpfTarget, 0755)
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

// attachTCEgress attaches prog to the egress hook of ifname using the clsact
// qdisc. Tries TCX (kernel 6.6+) first; falls back to classic netlink TC.
//
// Returns a cleanup func — call it if you want to detach (you probably don't,
// since the filter should outlive the exec into vminitd.real, but it's useful
// for error paths).
func attachTCEgress(ifname string, prog *ebpf.Program) (func(), error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, fmt.Errorf("interface %q: %w", ifname, err)
	}

	// Try TCX first (kernel >= 6.6, bpf_link based).
	l, err := link.AttachTCX(link.TCXOptions{
		Interface: iface.Index,
		Program:   prog,
		Attach:    ebpf.AttachTCXEgress,
	})

	if err == nil {
		//log("attachTCEgress: AttachTCX worked, pinning...")
		// Pin the link so it survives exec
		if err := l.Pin(bpfTarget + "/egress_link"); err != nil {
			log("pin tcx link failed, falling back to attachTCEgressNetlink: %v", err)
			return attachTCEgressNetlink(iface.Index, prog)
		}
		//log("attachTCEgress: pin worked")
		return func() { l.Close() }, nil
	}

	log("attachTCEgress: AttachTCX failed, falling back to attachTCEgressNetlink")
	// Fall back to classic netlink TC (works on older Kata kernels).
	return attachTCEgressNetlink(iface.Index, prog)
}

func attachTCEgressNetlink(ifindex int, prog *ebpf.Program) (func(), error) {
	// Add clsact qdisc — idempotent if already present.
	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: ifindex,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}
	if err := netlink.QdiscReplace(qdisc); err != nil {
		return nil, fmt.Errorf("qdisc replace: %w", err)
	}

	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: ifindex,
			Parent:    netlink.HANDLE_MIN_EGRESS,
			Handle:    1,
			Protocol:  unix.ETH_P_ALL,
			Priority:  1,
		},
		Fd:           prog.FD(),
		Name:         "egress_filter",
		DirectAction: true,
	}
	if err := netlink.FilterReplace(filter); err != nil {
		return nil, fmt.Errorf("filter replace: %w", err)
	}

	cleanup := func() {
		netlink.FilterDel(filter)
		netlink.QdiscDel(qdisc)
	}
	return cleanup, nil
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

	spec, err := ebpf.LoadCollectionSpec("/sbin/egress_filter.o")
	if err != nil {
		return nil, nil, fmt.Errorf("load bpf spec: %w", err)
	}

	var objs struct {
		EgressFilter *ebpf.Program `ebpf:"egress_filter"`
		AllowedIPs   *ebpf.Map     `ebpf:"allowed_ips"`
	}
	if err := spec.LoadAndAssign(&objs, &ebpf.CollectionOptions{
		Maps: ebpf.MapOptions{
			PinPath: "", // we pin manually
		},
		Programs: ebpf.ProgramOptions{
			LogLevel: ebpf.LogLevelInstruction, // helpful for verifier errors
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("load bpf objs: %w", err)
	}

	if err := objs.AllowedIPs.Pin(bpfTarget + "/allowed_ips"); err != nil {
		objs.AllowedIPs.Close()
		return nil, nil, fmt.Errorf("pin map: %w", err)
	}

	cleanup, err := attachTCEgress("eth0", objs.EgressFilter)
	if err != nil {
		objs.AllowedIPs.Close()
		return nil, nil, fmt.Errorf("attaching egress to eth0: %w", err)
	}
	objs.EgressFilter.Close()

	return objs.AllowedIPs, cleanup, nil
}

func main() {
	domainsFile := flag.String("domains-file", "/etc/sandbox/allowed-domains.txt", "")
	listen := flag.String("listen", "127.0.0.1:53", "")
	upstream := flag.String("upstream", "1.1.1.1:53", "")
	logTo := flag.String("log", "/dev/kmsg", "")
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

	// Pre-resolve domains from file and populate map
	domains := loadDomains(*domainsFile)
	preResolveDomains(domains, *upstream, m)

	// Intercept DNS and dynamically update map on each response
	dns.HandleFunc(".", makeHandler(domains, *upstream, m))

	server := &dns.Server{Addr: *listen, Net: "udp"}
	log("dnsproxy returned %v", server.ListenAndServe())
}
