//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/miekg/dns"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const bpfTarget = "/sys/fs/bpf"

func allowIP(m *ebpf.Map, addr string) {
	ip := net.ParseIP(addr).To4()
	if ip == nil {
		return
	}
	key := binary.LittleEndian.Uint32(ip)
	val := uint8(1)
	if err := m.Put(key, val); err != nil {
		log.Printf("map put %s: %v", addr, err)
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
		log.Fatalf("open domains: %v", err)
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
			log.Printf("warn: resolve %s: %v", domain, err)
			continue
		}
		for _, ans := range resp.Answer {
			if a, ok := ans.(*dns.A); ok {
				allowIP(m, a.A.String())
				log.Printf("pre-resolved %s -> %s", domain, a.A)
			}
		}
	}
}

func waitForLoopback() {
	for {
		iface, err := net.InterfaceByName("lo")
		if err == nil {
			addrs, err := iface.Addrs()
			if err == nil && len(addrs) > 0 {
				// try actually binding to confirm it's up
				l, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err == nil {
					l.Close()
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForNetwork() {
	for {
		c, err := net.Dial("udp", "1.1.1.1:53")
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func maintainResolvConf() {
	// TODO: replace the "namesever" line only, and keep the rest of whatever else is in /etc/resolv.conf

	want := []byte("nameserver 127.0.0.1\ndomain test\n")
	for {
		current, _ := os.ReadFile("/etc/resolv.conf")
		if !bytes.Equal(current, want) {
			err := os.WriteFile("/etc/resolv.conf", want, 0644)
			if err == nil && false {
				log.Println("(re)wrote /etc/resolv.conf")
			} else if false {
				log.Printf("error trying to (re)write /etc/resolv.conf: %v", err)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func mountBPFFS() error {
	// First, wait until bpfTarget is writeable. This is very  brittle.
	// We try 100 times max, waiting 10ms between each attempt,
	// to write bpfs. We do this because this process starts
	// before the actual vminitd starts, and vminit is what mounts
	// bpfs and makes it writeable.
	// TODO: look into just forking vminitd rather than trying to
	// do all this stuff in a sidecar.
	tries := 100
	for {
		if err := os.MkdirAll(bpfTarget, 0755); err != nil {
			tries--
			if tries == 0 {
				return fmt.Errorf("mkdir bpffs: %w", err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		break
	}

	if err := syscall.Mount("bpffs", bpfTarget, "bpf", 0, ""); err != nil {
		if err == syscall.EBUSY {
			// already mounted, that's fine
			return nil
		}
		return fmt.Errorf("mount bpffs: %w", err)
	}
	return nil
}

// attachTCEgress attaches prog to the egress hook of ifname using the clsact
// qdisc. Tries TCX (kernel 6.6+) first; falls back to classic netlink TC.
//
// Returns a cleanup func — call it if you want to detach (you probably don't,
// since the filter should outlive the exec into vminitd.real, but it's useful
// for error paths).
func attachTCEgress(kmsg *os.File, ifname string, prog *ebpf.Program) (func(), error) {
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
		kmsg.WriteString("<6>attachTCEgress: AttachTCX worked, pinning...\n")
		// Pin the link so it survives exec
		if err := l.Pin(bpfTarget + "/egress_link"); err != nil {
			kmsg.WriteString(fmt.Sprintf("<6>pin tcx link failed, falling back to attachTCEgressNetlink: %v\n", err))
			return attachTCEgressNetlink(iface.Index, prog)
		}
		kmsg.WriteString("<6>attachTCEgress: pin worked\n")
		return func() { l.Close() }, nil
	}

	kmsg.WriteString("<6>attachTCEgress: AttachTCX failed, falling back to attachTCEgressNetlink\n")
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

func main() {
	domainsFile := flag.String("domains-file", "/etc/sandbox/allowed-domains.txt", "")
	listen := flag.String("listen", "127.0.0.1:53", "")
	upstream := flag.String("upstream", "1.1.1.1:53", "")

	// Write a message to kernel log
	kmsg, err := os.OpenFile("/dev/kmsg", os.O_WRONLY, 0)
	if err == nil {
		kmsg.WriteString("<6>sand-dns-proxy: === starting dns proxy ===\n")
		defer kmsg.Close()
	}

	kmsg.WriteString("<6>mounting bpffs...\n")
	if err := mountBPFFS(); err != nil {
		kmsg.WriteString(fmt.Sprintf("<6>mountBPFFS: %v\n", err))
		return
	}
	kmsg.WriteString("<6>bpffs mounted ok\n")

	// verify
	entries, err := os.ReadDir(bpfTarget)
	if err != nil {
		kmsg.WriteString(fmt.Sprintf("<6>readdir %s: %v\n", bpfTarget, err))
		return
	}
	kmsg.WriteString(fmt.Sprintf("<6>%s contents: %v\n", bpfTarget, entries))

	kmsg.WriteString("<6>loading bpf spec...\n")

	// Load and attach TC filter
	spec, err := ebpf.LoadCollectionSpec("/sbin/egress_filter.o")
	if err != nil {
		kmsg.WriteString(fmt.Sprintf("<6>load bpf spec: %v\n", err))
		return
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
		kmsg.WriteString(fmt.Sprintf("<6>load bpf objs: %v\n", err))
		return
	}

	kmsg.WriteString("<6>pinning allowed_ips...\n")

	if err := objs.AllowedIPs.Pin(bpfTarget + "/allowed_ips"); err != nil {
		kmsg.WriteString(fmt.Sprintf("<6>pin map: %v\n", err))
		return
	}

	kmsg.WriteString("<6>attaching egress filter to eth0...\n")

	_, err = attachTCEgress(kmsg, "eth0", objs.EgressFilter)
	if err != nil {
		kmsg.WriteString(fmt.Sprintf("<6>attaching egress to eth0: %v\n", err))
		return
	}

	m := objs.AllowedIPs

	log.Println("waiting for loopback...")
	waitForLoopback()
	log.Println("loopback ready")

	log.Println("waiting for network...")
	waitForNetwork()
	log.Println("network ready, pre-resolving domains...")

	// Pre-resolve domains from file and populate map
	domains := loadDomains(*domainsFile)
	preResolveDomains(domains, *upstream, m)

	// Intercept DNS and dynamically update map on each response
	dns.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		kmsg, err := os.OpenFile("/dev/kmsg", os.O_WRONLY, 0)
		if err == nil {
			defer kmsg.Close()
		}
		c := new(dns.Client)
		resp, _, err := c.Exchange(r, *upstream)
		if err != nil {
			if kmsg != nil {
				kmsg.WriteString(fmt.Sprintf("<6>dns.Client Exchange error: %v", err))
			}
			dns.HandleFailed(w, r)
			return
		}

		for _, ans := range resp.Answer {
			if a, ok := ans.(*dns.A); ok {
				if isAllowedDomain(domains, a.Hdr.Name) {
					if kmsg != nil {
						kmsg.WriteString(fmt.Sprintf("<6>dns.Client allowed domain: %s=%s", a.Hdr.Name, a.A.String()))
					}
					allowIP(m, a.A.String())
				} else {
					if kmsg != nil {
						kmsg.WriteString(fmt.Sprintf("<6>dns.Client NOT allowed domain: %s=%s", a.Hdr.Name, a.A.String()))
					}
				}
			}
		}
		w.WriteMsg(resp)
	})

	server := &dns.Server{Addr: *listen, Net: "udp"}
	go maintainResolvConf()
	log.Fatal(server.ListenAndServe())
}
